package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"campusclaw/backend/internal/model"
	"campusclaw/backend/internal/store"
)

var templateVariable = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)

func validateTemplate(name, content string) error {
	if strings.TrimSpace(name)=="" || utf8.RuneCountInString(strings.TrimSpace(name))>64 { return errors.New("模板名称不能为空且不能超过 64 字") }
	if strings.TrimSpace(content)=="" || utf8.RuneCountInString(content)>2000 { return errors.New("模板内容不能为空且不能超过 2000 字") }
	for _, match := range templateVariable.FindAllStringSubmatch(content,-1) {
		if match[1]!="student_name" && match[1]!="class_name" { return errors.New("模板包含不支持的变量") }
	}
	remaining:=templateVariable.ReplaceAllString(content,"")
	if strings.Contains(remaining,"{{") || strings.Contains(remaining,"}}") { return errors.New("模板变量格式不正确") }
	return nil
}

func (s *Server) templateRoutes() {
	s.mux.Handle("GET /api/prompt-templates",s.requireAuth(http.HandlerFunc(s.listTemplates)))
	s.mux.Handle("POST /api/prompt-templates",s.requireAuth(http.HandlerFunc(s.createTemplate)))
	s.mux.Handle("GET /api/prompt-templates/{id}",s.requireAuth(http.HandlerFunc(s.getTemplate)))
	s.mux.Handle("PUT /api/prompt-templates/{id}",s.requireAuth(http.HandlerFunc(s.updateTemplate)))
}

func templatePathID(w http.ResponseWriter,r *http.Request)(int64,bool){
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil||id<1 { writeError(w,http.StatusNotFound,"模板不存在");return 0,false }
	return id,true
}

func (s *Server) listTemplates(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return}
	items,err:=s.store.ListTemplates(r.Context(),u.ClassID)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"templates":items})
}

func (s *Server) getTemplate(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return}
	id,ok:=templatePathID(w,r);if !ok{return}
	item,err:=s.store.TemplateByID(r.Context(),u.ClassID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"模板不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"template":item})
}

type templateRequest struct { Name string `json:"name"`; Content string `json:"content"`; Enabled bool `json:"enabled"` }

func (s *Server) saveTemplate(w http.ResponseWriter,r *http.Request,id int64){
	u,ok:=teacherUser(w,r);if !ok{return}
	var in templateRequest
	if err:=decodeJSON(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"请求格式不正确");return}
	if err:=validateTemplate(in.Name,in.Content);err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	item:=model.PromptTemplate{ID:id,ClassID:u.ClassID,Name:strings.TrimSpace(in.Name),Content:in.Content,Enabled:in.Enabled}
	err:=s.store.SaveTemplate(r.Context(),&item)
	switch {
	case errors.Is(err,store.ErrNotFound): writeError(w,http.StatusNotFound,"模板不存在");return
	case errors.Is(err,store.ErrConflict): writeError(w,http.StatusBadRequest,"本班已有同名模板");return
	case err!=nil: writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return
	}
	item,err=s.store.TemplateByID(r.Context(),u.ClassID,item.ID)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	status:=http.StatusOK;if id==0{status=http.StatusCreated}
	writeJSON(w,status,map[string]any{"template":item})
}

func (s *Server) createTemplate(w http.ResponseWriter,r *http.Request){s.saveTemplate(w,r,0)}
func (s *Server) updateTemplate(w http.ResponseWriter,r *http.Request){id,ok:=templatePathID(w,r);if ok{s.saveTemplate(w,r,id)}}
