package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"campusclaw/backend/internal/auth"
	"campusclaw/backend/internal/chunk"
	"campusclaw/backend/internal/config"
	"campusclaw/backend/internal/embed"
	"campusclaw/backend/internal/model"
	"campusclaw/backend/internal/retrieval"
	"campusclaw/backend/internal/store"
	"campusclaw/backend/internal/vector"
	"golang.org/x/crypto/bcrypt"
)

// 凭证通过 Authorization: Bearer <JWT> 传输，浏览器不再持有 Cookie。
const bearerPrefix = "Bearer "

type userContextKey struct{}

type limiterEntry struct { failures int; resetAt time.Time }

type Server struct {
	cfg      config.Config
	store    *store.Store
	retrieval *retrieval.Service
	vectors  *vector.Store
	answerer retrieval.Answerer
	mux      *http.ServeMux
	limiterMu sync.Mutex
	limiter  map[string]limiterEntry
	dummyHash []byte
}

func New(cfg config.Config, st *store.Store, svc *retrieval.Service, vs *vector.Store, answerer retrieval.Answerer) *Server {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("campusclaw-dummy-password"), bcrypt.DefaultCost)
	s := &Server{cfg: cfg, store: st, retrieval: svc, vectors: vs, answerer: answerer, mux: http.NewServeMux(), limiter: map[string]limiterEntry{}, dummyHash: dummy}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.securityHeaders(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter,_ *http.Request){ writeJSON(w,http.StatusOK,map[string]string{"status":"ok"}) })
	s.mux.HandleFunc("POST /api/login",s.login)
	s.mux.HandleFunc("POST /api/token/refresh",s.refresh)
	s.mux.Handle("POST /api/logout",s.requireAuth(http.HandlerFunc(s.logout)))
	s.mux.Handle("GET /api/me",s.requireAuth(http.HandlerFunc(s.me)))
	s.mux.Handle("GET /api/materials",s.requireAuth(http.HandlerFunc(s.listMaterials)))
	s.mux.Handle("POST /api/materials",s.requireAuth(http.HandlerFunc(s.uploadMaterial)))
	s.mux.Handle("GET /api/materials/{id}",s.requireAuth(http.HandlerFunc(s.materialDetail)))
	s.mux.Handle("GET /api/materials/{id}/file",s.requireAuth(http.HandlerFunc(s.materialFile)))
	s.mux.Handle("GET /api/materials/{id}/chunks",s.requireAuth(http.HandlerFunc(s.materialChunks)))
	s.mux.Handle("GET /api/materials/{id}/vectors",s.requireAuth(http.HandlerFunc(s.materialVectors)))
	s.mux.Handle("POST /api/materials/{id}/reindex",s.requireAuth(http.HandlerFunc(s.reindexMaterial)))
	s.mux.Handle("POST /api/search",s.requireAuth(http.HandlerFunc(s.search)))
	s.mux.Handle("POST /api/ask",s.requireAuth(http.HandlerFunc(s.ask)))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
	w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("X-Frame-Options","DENY");w.Header().Set("Referrer-Policy","same-origin");w.Header().Set("Cache-Control","no-store")
	if r.Method!="GET"&&r.Method!="HEAD"&&r.Method!="OPTIONS" { if origin:=r.Header.Get("Origin");origin!=""&&!sameOrigin(origin,r.Host){writeError(w,http.StatusForbidden,"跨站请求被拒绝");return} }
	next.ServeHTTP(w,r)
})}

func sameOrigin(origin,host string)bool{return strings.HasSuffix(origin,"://"+host)}

// requireAuth 校验 Bearer JWT 后按 sub 回库读取实时身份：
// 授权（角色、班级）以数据库当前值为准，不信任令牌签发时刻的 payload。
func (s *Server) requireAuth(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
	claims,err:=s.bearerClaims(r);if err!=nil{writeError(w,http.StatusUnauthorized,"请先登录");return}
	u,err:=s.store.UserByID(r.Context(),claims.Sub);if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusUnauthorized,"登录已过期");return};if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	next.ServeHTTP(w,r.WithContext(context.WithValue(r.Context(),userContextKey{},u)))
})}

// bearerClaims 提取并校验 Authorization: Bearer 头中的 JWT。
func (s *Server) bearerClaims(r *http.Request) (auth.Claims, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) { return auth.Claims{}, auth.ErrInvalid }
	return auth.Verify(s.cfg.SessionSecret, strings.TrimPrefix(h, bearerPrefix))
}

func currentUser(r *http.Request) model.User { return r.Context().Value(userContextKey{}).(model.User) }

func (s *Server) login(w http.ResponseWriter,r *http.Request){
	var in struct{Username string `json:"username"`;Password string `json:"password"`}
	if err:=decodeJSON(r,&in);err!=nil||strings.TrimSpace(in.Username)==""||in.Password==""{writeError(w,http.StatusBadRequest,"请输入用户名和密码");return}
	key:=clientIP(r)+"|"+strings.ToLower(strings.TrimSpace(in.Username));if s.rateLimited(key){writeError(w,http.StatusTooManyRequests,"登录尝试过多，请稍后再试");return}
	u,err:=s.store.UserByUsername(r.Context(),strings.TrimSpace(in.Username));hash:=s.dummyHash;if err==nil{hash=[]byte(u.PasswordHash)}
	valid:=bcrypt.CompareHashAndPassword(hash,[]byte(in.Password))==nil&&err==nil
	if !valid{s.recordFailure(key);writeError(w,http.StatusUnauthorized,"用户名或密码错误");return};s.clearFailures(key)
	if err:=s.issueTokens(r.Context(),w,u);err!=nil{writeError(w,http.StatusServiceUnavailable,"登录失败");return}
}

// issueTokens 签发 access JWT（无状态、短时效）与 refresh token（有状态、可吊销），
// 并按课程示例的结构返回：Authorization 头使用 Bearer + access_token。
func (s *Server) issueTokens(ctx context.Context, w http.ResponseWriter, u model.User) error {
	now:=time.Now()
	access,err:=auth.Sign(s.cfg.SessionSecret,auth.Claims{
		Sub:u.ID,Username:u.Username,ClassID:u.ClassID,Role:u.Role,
		IssuedAt:now.Unix(),ExpiresAt:now.Add(s.cfg.TokenTTL).Unix(),
	})
	if err!=nil{return err}
	refresh,err:=randomToken();if err!=nil{return err}
	if err=s.store.CreateRefreshToken(ctx,refresh,u.ID,now.Add(s.cfg.RefreshTTL));err!=nil{return err}
	writeJSON(w,http.StatusOK,map[string]any{
		"token_type":"Bearer","access_token":access,
		"expires_in":int(s.cfg.TokenTTL.Seconds()),
		"refresh_token":refresh,"user":u,
	})
	return nil
}

// refresh 用 refresh_token 换取新 token 对；轮换在事务内完成，旧串立即作废。
func (s *Server) refresh(w http.ResponseWriter,r *http.Request){
	var in struct{RefreshToken string `json:"refresh_token"`}
	if err:=decodeLenient(r,&in);err!=nil||strings.TrimSpace(in.RefreshToken)==""{writeError(w,http.StatusBadRequest,"缺少 refresh_token");return}
	u,err:=s.store.UserByRefreshToken(r.Context(),in.RefreshToken);if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusUnauthorized,"登录已过期，请重新登录");return};if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	newRefresh,err:=randomToken();if err!=nil{writeError(w,http.StatusInternalServerError,"续期失败");return}
	if err=s.store.RotateRefreshToken(r.Context(),in.RefreshToken,newRefresh,u.ID,time.Now().Add(s.cfg.RefreshTTL));err!=nil{writeError(w,http.StatusServiceUnavailable,"续期失败");return}
	now:=time.Now()
	access,err:=auth.Sign(s.cfg.SessionSecret,auth.Claims{
		Sub:u.ID,Username:u.Username,ClassID:u.ClassID,Role:u.Role,
		IssuedAt:now.Unix(),ExpiresAt:now.Add(s.cfg.TokenTTL).Unix(),
	})
	if err!=nil{writeError(w,http.StatusInternalServerError,"续期失败");return}
	writeJSON(w,http.StatusOK,map[string]any{
		"token_type":"Bearer","access_token":access,
		"expires_in":int(s.cfg.TokenTTL.Seconds()),
		"refresh_token":newRefresh,"user":u,
	})
}

// logout 吊销 refresh token；access token 依赖短时效自然失效，前端同时清空本地凭证。
func (s *Server) logout(w http.ResponseWriter,r *http.Request){
	var in struct{RefreshToken string `json:"refresh_token"`}
	if err:=decodeLenient(r,&in);err==nil&&strings.TrimSpace(in.RefreshToken)!=""{
		_ = s.store.DeleteRefreshToken(r.Context(),strings.TrimSpace(in.RefreshToken))
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) me(w http.ResponseWriter,r *http.Request){writeJSON(w,http.StatusOK,map[string]any{"user":currentUser(r)})}

func (s *Server) listMaterials(w http.ResponseWriter,r *http.Request){u:=currentUser(r);items,err:=s.store.ListMaterials(r.Context(),u.ClassID,strings.TrimSpace(r.URL.Query().Get("q")));if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return};writeJSON(w,http.StatusOK,map[string]any{"materials":items})}

func (s *Server) loadOwnMaterial(w http.ResponseWriter,r *http.Request)(model.Material,bool){id,err:=strconv.ParseInt(r.PathValue("id"),10,64);if err!=nil||id<1{writeError(w,http.StatusNotFound,"材料不存在");return model.Material{},false};m,err:=s.store.MaterialByID(r.Context(),id);if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"材料不存在");return model.Material{},false};if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return model.Material{},false};if m.ClassID!=currentUser(r).ClassID{writeError(w,http.StatusNotFound,"材料不存在");return model.Material{},false};return m,true}
func (s *Server) materialDetail(w http.ResponseWriter,r *http.Request){if m,ok:=s.loadOwnMaterial(w,r);ok{writeJSON(w,http.StatusOK,map[string]any{"material":m})}}

func (s *Server) materialFile(w http.ResponseWriter,r *http.Request){m,ok:=s.loadOwnMaterial(w,r);if !ok{return};path:=filepath.Join(s.cfg.UploadDir,filepath.Base(m.StoredName));f,err:=os.Open(path);if err!=nil{writeError(w,http.StatusNotFound,"文件不存在");return};defer f.Close();w.Header().Set("Content-Type",m.MediaType);w.Header().Set("Content-Disposition",fmt.Sprintf("attachment; filename*=UTF-8''%s",urlEncodeFilename(m.OriginalName)));w.Header().Set("Content-Length",strconv.FormatInt(m.SizeBytes,10));_,_ = io.Copy(w,f)}

// ---- 检索 ----

// searchRequest 中的 ClassID 只会被解析后丢弃：班级只能来自登录会话。
type searchRequest struct {
	Q      string `json:"q"`
	Mode   string `json:"mode"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in searchRequest
	if err := decodeLenient(r, &in); err != nil { writeError(w, http.StatusBadRequest, "请求格式不正确"); return }
	mode, err := retrieval.ParseMode(in.Mode)
	if err != nil { writeError(w, http.StatusBadRequest, "检索模式只能是 keyword、vector 或 hybrid"); return }
	if strings.TrimSpace(in.Q) == "" { writeError(w, http.StatusBadRequest, "请输入检索内容"); return }
	result, err := s.retrieval.Search(r.Context(), u.ClassID, in.Q, mode, in.Limit, in.Offset)
	if err != nil { writeRetrievalError(w, err); return }
	writeJSON(w, http.StatusOK, result)
}

type askRequest struct {
	Q       string           `json:"q"`
	History []retrieval.Turn `json:"history"`
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in askRequest
	if err := decodeLenient(r, &in); err != nil { writeError(w, http.StatusBadRequest, "请求格式不正确"); return }
	if strings.TrimSpace(in.Q) == "" { writeError(w, http.StatusBadRequest, "请输入提问内容"); return }
	// 客户端自行构造的 system 消息在这里被丢弃，不会进入对话模块的输入。
	history := make([]retrieval.Turn, 0, len(in.History))
	for _, t := range in.History {
		if t.Role == "user" || t.Role == "assistant" { history = append(history, t) }
	}
	result, err := s.retrieval.Ask(r.Context(), u.ClassID, in.Q, history, s.cfg.AskTopK, s.answerer)
	if err != nil { writeRetrievalError(w, err); return }
	writeJSON(w, http.StatusOK, result)
}

// writeRetrievalError 把检索层的错误映射为状态码：
// 空查询与非法模式是 400；向量库或对话网关不可用是 503，不编造任何结果。
func writeRetrievalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, retrieval.ErrBadQuery):
		writeError(w, http.StatusBadRequest, "请输入检索内容")
	case errors.Is(err, retrieval.ErrBadMode):
		writeError(w, http.StatusBadRequest, "检索模式只能是 keyword、vector 或 hybrid")
	case errors.Is(err, retrieval.ErrVectorUnavailable), errors.Is(err, vector.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "向量库暂时不可用，请改用关键字检索")
	case errors.Is(err, retrieval.ErrAnswerUnavailable):
		writeError(w, http.StatusServiceUnavailable, "对话服务暂时不可用")
	default:
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用")
	}
}

// ---- 切片与向量对照、重建索引 ----

func (s *Server) materialChunks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	m, ok := s.loadOwnMaterial(w, r)
	if !ok { return }
	chunks, err := s.store.ListChunks(r.Context(), u.ClassID, m.ID)
	if err != nil { writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return }
	writeJSON(w, http.StatusOK, map[string]any{"material_id": m.ID, "chunks": chunks})
}

func (s *Server) materialVectors(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	m, ok := s.loadOwnMaterial(w, r)
	if !ok { return }
	chunks, err := s.store.ListChunks(r.Context(), u.ClassID, m.ID)
	if err != nil { writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return }
	ids := make([]int64, 0, len(chunks))
	for _, c := range chunks { ids = append(ids, c.ID) }
	// 向量库不可用时退化为只展示 MySQL 一侧，不伪造向量分量。
	points, err := s.vectors.Retrieve(r.Context(), ids)
	if err != nil { points = map[int64]vector.Retrieved{} }
	views := make([]retrieval.ChunkView, 0, len(chunks))
	for _, c := range chunks {
		view := retrieval.ChunkView{
			ChunkIndex: c.ChunkIndex, Status: c.IndexStatus, StartOffset: c.StartOffset, EndOffset: c.EndOffset,
			Excerpt: excerpt(c.ChunkText, 160), Text: c.ChunkText,
			MySQL: retrieval.MySQLRef{ChunkID: c.ID, EntryID: c.EntryID, MaterialID: c.MaterialID, ClassID: c.ClassID},
			Collection: s.vectors.Collection(),
		}
		if p, ok := points[c.ID]; ok {
			preview := p.Vector
			if len(preview) > s.cfg.VectorPreviewDims { preview = preview[:s.cfg.VectorPreviewDims] }
			view.Point = &retrieval.PointView{
				PointID: p.ID, ChunkID: p.ChunkID, EntryID: p.EntryID, MaterialID: p.MaterialID,
				ClassID: p.ClassID, ChunkIndex: p.Index,
				Dim: len(p.Vector), Norm: embed.Norm(p.Vector), Preview: preview,
			}
			// 主键一致才算两库对齐：knowledge_chunks.id = 向量主键 = payload.chunk_id。
			view.Linked = p.ChunkID == c.ID && p.ClassID == c.ClassID
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"material_id": m.ID, "class_id": u.ClassID, "collection": s.vectors.Collection(),
		"chunks": views,
	})
}

type reindexRequest struct {
	Strategy           string `json:"chunk_strategy"`
	Separator          string `json:"chunk_separator"`
	MaxChars           int    `json:"chunk_max_chars"`
	OverlapPercent     int    `json:"chunk_overlap_percent"`
	CollapseWhitespace int    `json:"chunk_collapse_ws"`
	StripLinks         int    `json:"chunk_strip_links"`
}

func (s *Server) reindexMaterial(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.Role != "teacher" { writeError(w, http.StatusForbidden, "只有教师可以重建索引"); return }
	m, ok := s.loadOwnMaterial(w, r)
	if !ok { return }
	var in reindexRequest
	if err := decodeLenient(r, &in); err != nil { writeError(w, http.StatusBadRequest, "请求格式不正确"); return }
	opt, err := parseChunkOptions(in)
	if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	ref, body, err := s.store.EntryForMaterial(r.Context(), m.ID, u.ClassID)
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "材料不存在"); return }
	if err != nil { writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return }
	outcome, err := s.retrieval.IndexMaterial(r.Context(), ref, body, opt)
	if err != nil {
		if errors.Is(err, vector.ErrUnavailable) { writeError(w, http.StatusServiceUnavailable, "向量库暂时不可用"); return }
		writeError(w, http.StatusServiceUnavailable, "重建索引失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": m.ID, "title": m.Title, "chunk_count": outcome.Chunks, "chunk_strategy": outcome.Strategy,
		"ready_count": outcome.Ready, "failed_count": outcome.Failed,
	})
}

func parseChunkOptions(in reindexRequest) (chunk.Options, error) {
	opt := chunk.Options{
		Strategy:           chunk.Strategy(strings.ToLower(strings.TrimSpace(in.Strategy))),
		Separator:          chunk.Separator(strings.ToLower(strings.TrimSpace(in.Separator))),
		MaxChars:           in.MaxChars,
		OverlapPercent:     in.OverlapPercent,
		CollapseWhitespace: in.CollapseWhitespace == 1,
		StripLinks:         in.StripLinks == 1,
	}
	opt, err := chunk.Normalize(opt)
	if err != nil { return opt, err }
	if opt.Strategy == chunk.Custom && opt.Separator == "" { opt.Separator = chunk.SepNewline }
	return opt, nil
}

// ---- 上传（成功后建索引）----

func (s *Server) uploadMaterial(w http.ResponseWriter,r *http.Request){
	u:=currentUser(r);if u.Role!="teacher"{writeError(w,http.StatusForbidden,"只有教师可以上传材料");return}
	r.Body=http.MaxBytesReader(w,r.Body,s.cfg.UploadMaxBytes+1024*128);if err:=r.ParseMultipartForm(s.cfg.UploadMaxBytes+1024*128);err!=nil{writeError(w,http.StatusRequestEntityTooLarge,"文件超过大小限制");return}
	file,header,err:=r.FormFile("file");if err!=nil{writeError(w,http.StatusBadRequest,"请选择文件");return};defer file.Close()
	ext:=strings.ToLower(filepath.Ext(header.Filename));if ext!=".txt"&&ext!=".md"{writeError(w,http.StatusBadRequest,"仅支持 .txt 和 .md 文件");return}
	data,err:=readLimited(file,s.cfg.UploadMaxBytes);if errors.Is(err,errTooLarge){writeError(w,http.StatusRequestEntityTooLarge,"文件超过大小限制");return};if err!=nil{writeError(w,http.StatusBadRequest,"无法读取文件");return};if !utf8.Valid(data){writeError(w,http.StatusBadRequest,"文件必须是 UTF-8 编码");return}
	title:=strings.TrimSpace(r.FormValue("title"));if title==""{title=strings.TrimSuffix(filepath.Base(header.Filename),filepath.Ext(header.Filename))};if len([]rune(title))>120{writeError(w,http.StatusBadRequest,"标题不能超过 120 个字符");return}
	opt,err:=parseChunkOptions(reindexRequest{
		Strategy: r.FormValue("chunk_strategy"), Separator: r.FormValue("chunk_separator"),
		MaxChars: formInt(r.FormValue("chunk_max_chars")), OverlapPercent: formInt(r.FormValue("chunk_overlap_percent")),
		CollapseWhitespace: formInt(r.FormValue("chunk_collapse_ws")), StripLinks: formInt(r.FormValue("chunk_strip_links")),
	})
	if err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	stored,err:=randomStoredName(ext);if err!=nil{writeError(w,http.StatusInternalServerError,"上传失败");return}
	tmp,err:=os.CreateTemp(s.cfg.UploadDir,".upload-*");if err!=nil{writeError(w,http.StatusInternalServerError,"上传失败");return};tmpName:=tmp.Name();finalName:=filepath.Join(s.cfg.UploadDir,stored);cleaned:=false;cleanup:=func(){if !cleaned{_ = os.Remove(tmpName);_ = os.Remove(finalName)}};defer cleanup()
	if _,err=tmp.Write(data);err!=nil{_ = tmp.Close();writeError(w,http.StatusInternalServerError,"上传失败");return}
	if err=tmp.Sync();err!=nil{_ = tmp.Close();writeError(w,http.StatusInternalServerError,"上传失败");return}
	if err=tmp.Close();err!=nil{writeError(w,http.StatusInternalServerError,"上传失败");return}
	m:=model.Material{ClassID:u.ClassID,UploaderID:u.ID,UploaderName:u.DisplayName,Title:title,OriginalName:filepath.Base(header.Filename),StoredName:stored,MediaType:mediaType(ext),SizeBytes:int64(len(data))}
	err=s.store.CreateMaterial(r.Context(),&m,string(data),func()error{return os.Rename(tmpName,finalName)})
	if err!=nil{writeError(w,http.StatusInternalServerError,"上传入库失败");return};cleaned=true
	// 原文已落库：这里失败只会让这批切片无法用于向量检索，材料本身仍然可查可下载。
	outcome := retrieval.IndexOutcome{Strategy: string(opt.Strategy)}
	if ref, body, e := s.store.EntryForMaterial(r.Context(), m.ID, u.ClassID); e == nil {
		if oc, e := s.retrieval.IndexMaterial(r.Context(), ref, body, opt); e == nil { outcome = oc }
	}
	m.ChunkCount, m.ChunkStrategy = outcome.Chunks, outcome.Strategy
	writeJSON(w,http.StatusCreated,map[string]any{"material":m,"id":m.ID,"title":m.Title,"chunk_count":outcome.Chunks,"chunk_strategy":outcome.Strategy,"ready_count":outcome.Ready,"failed_count":outcome.Failed})
}

func formInt(raw string) int { n, err := strconv.Atoi(strings.TrimSpace(raw)); if err != nil { return 0 }; return n }

var errTooLarge=errors.New("too large")
func readLimited(f multipart.File,max int64)([]byte,error){r:=io.LimitReader(f,max+1);b,err:=io.ReadAll(r);if err==nil&&int64(len(b))>max{return nil,errTooLarge};return b,err}
func mediaType(ext string)string{if ext==".md"{return "text/markdown; charset=utf-8"};return "text/plain; charset=utf-8"}
func randomToken()(string,error){b:=make([]byte,32);if _,err:=rand.Read(b);err!=nil{return "",err};return base64.RawURLEncoding.EncodeToString(b),nil}
func randomStoredName(ext string)(string,error){t,err:=randomToken();if err!=nil{return "",err};return t+ext,nil}
func clientIP(r *http.Request)string{host,_,err:=net.SplitHostPort(r.RemoteAddr);if err==nil{return host};return r.RemoteAddr}
func (s *Server) rateLimited(key string)bool{s.limiterMu.Lock();defer s.limiterMu.Unlock();e,ok:=s.limiter[key];if !ok||time.Now().After(e.resetAt){delete(s.limiter,key);return false};return e.failures>=s.cfg.LoginMaxFailures}
func (s *Server) recordFailure(key string){s.limiterMu.Lock();defer s.limiterMu.Unlock();e:=s.limiter[key];if time.Now().After(e.resetAt){e=limiterEntry{resetAt:time.Now().Add(s.cfg.LoginWindow)}};e.failures++;s.limiter[key]=e}
func (s *Server) clearFailures(key string){s.limiterMu.Lock();delete(s.limiter,key);s.limiterMu.Unlock()}
func decodeJSON(r *http.Request,out any)error{defer r.Body.Close();d:=json.NewDecoder(io.LimitReader(r.Body,64<<10));d.DisallowUnknownFields();return d.Decode(out)}
// decodeLenient 允许请求中携带本服务不读取的字段（例如客户端传来的 class_id），
// 这些字段解析后即被丢弃，不会进入任何过滤条件。
func decodeLenient(r *http.Request,out any)error{defer r.Body.Close();d:=json.NewDecoder(io.LimitReader(r.Body,64<<10));return d.Decode(out)}
func writeJSON(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
func writeError(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]string{"error":message})}
func urlEncodeFilename(name string)string{r:=strings.NewReplacer("%","%25"," ","%20","#","%23","?","%3F","\"","%22","'","%27");return r.Replace(filepath.Base(name))}
func excerpt(text string, max int) string { r := []rune(text); if len(r) <= max { return text }; return string(r[:max]) + "…" }
