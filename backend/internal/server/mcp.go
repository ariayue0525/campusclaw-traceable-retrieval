package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"campusclaw/backend/internal/mcpbridge"
	"campusclaw/backend/internal/model"
	"campusclaw/backend/internal/store"
)

var mcpServerName=regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (s *Server) mcpRoutes(){
	s.mux.Handle("GET /api/mcp/servers",s.requireAuth(http.HandlerFunc(s.listMCPServers)))
	s.mux.Handle("POST /api/mcp/servers",s.requireAuth(http.HandlerFunc(s.createMCPServer)))
	s.mux.Handle("GET /api/mcp/servers/{id}",s.requireAuth(http.HandlerFunc(s.getMCPServer)))
	s.mux.Handle("PUT /api/mcp/servers/{id}",s.requireAuth(http.HandlerFunc(s.updateMCPServer)))
	s.mux.Handle("POST /api/mcp/servers/{id}/connect",s.requireAuth(http.HandlerFunc(s.connectMCPServer)))
	s.mux.Handle("POST /api/mcp/servers/{id}/disconnect",s.requireAuth(http.HandlerFunc(s.disconnectMCPServer)))
	s.mux.Handle("GET /api/mcp/servers/{id}/tools",s.requireAuth(http.HandlerFunc(s.listMCPTools)))
	s.mux.Handle("GET /api/mcp/audit",s.requireAuth(http.HandlerFunc(s.listMCPAudit)))
}

func mcpPathID(w http.ResponseWriter,r *http.Request)(int64,bool){
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil||id<1{writeError(w,http.StatusNotFound,"MCP 服务不存在");return 0,false}
	return id,true
}

func (s *Server) listMCPServers(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return}
	items,err:=s.store.ListMCPServers(r.Context(),u.ClassID)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"servers":items})
}

func (s *Server) getMCPServer(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return};id,ok:=mcpPathID(w,r);if !ok{return}
	item,err:=s.store.MCPServerByID(r.Context(),u.ClassID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"MCP 服务不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"server":item})
}

type mcpServerRequest struct{Name string `json:"name"`;BaseURL string `json:"base_url"`}

func (s *Server) saveMCPServer(w http.ResponseWriter,r *http.Request,id int64){
	u,ok:=teacherUser(w,r);if !ok{return}
	var in mcpServerRequest
	if err:=decodeJSON(r,&in);err!=nil{writeError(w,http.StatusBadRequest,"请求格式不正确");return}
	in.Name=strings.TrimSpace(in.Name);in.BaseURL=strings.TrimRight(strings.TrimSpace(in.BaseURL),"/")
	if !mcpServerName.MatchString(in.Name)||mcpbridge.ValidateBaseURL(in.BaseURL)!=nil{writeError(w,http.StatusBadRequest,"MCP 名称或地址无效");return}
	key:="MCP_TOKEN_"+strings.ToUpper(strings.ReplaceAll(in.Name,"-","_"))
	if in.BaseURL=="http://mcp-server:8091"{key="MCP_AUTH_TOKEN"}
	item:=model.MCPServer{ID:id,ClassID:u.ClassID,Name:in.Name,BaseURL:in.BaseURL,TokenEnvKey:key}
	err:=s.store.SaveMCPServer(r.Context(),&item)
	switch{
	case errors.Is(err,store.ErrNotFound):writeError(w,http.StatusNotFound,"MCP 服务不存在");return
	case errors.Is(err,store.ErrConflict):writeError(w,http.StatusBadRequest,"本班已有同名 MCP 服务");return
	case err!=nil:writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return
	}
	item,err=s.store.MCPServerByID(r.Context(),u.ClassID,item.ID)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	status:=http.StatusOK;if id==0{status=http.StatusCreated}
	writeJSON(w,status,map[string]any{"server":item})
}

func (s *Server) createMCPServer(w http.ResponseWriter,r *http.Request){s.saveMCPServer(w,r,0)}
func (s *Server) updateMCPServer(w http.ResponseWriter,r *http.Request){id,ok:=mcpPathID(w,r);if ok{s.saveMCPServer(w,r,id)}}

func (s *Server) connectMCPServer(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return};id,ok:=mcpPathID(w,r);if !ok{return}
	item,err:=s.store.MCPServerByID(r.Context(),u.ClassID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"MCP 服务不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	client,err:=mcpbridge.New(item)
	if err!=nil{writeError(w,http.StatusBadRequest,"MCP 令牌或地址未正确配置");return}
	tools,err:=client.Discover(r.Context())
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"MCP 服务连接或发现失败");return}
	if err:=s.store.ReplaceMCPTools(r.Context(),u.ClassID,id,tools);err!=nil{writeError(w,http.StatusServiceUnavailable,"MCP 工具保存失败");return}
	item,err=s.store.MCPServerByID(r.Context(),u.ClassID,id)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	storedTools,err:=s.store.ListMCPTools(r.Context(),u.ClassID,id)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"server":item,"tools":storedTools})
}

func (s *Server) disconnectMCPServer(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return};id,ok:=mcpPathID(w,r);if !ok{return}
	err:=s.store.DisconnectMCPServer(r.Context(),u.ClassID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"MCP 服务不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"connected":false})
}

func (s *Server) listMCPTools(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return};id,ok:=mcpPathID(w,r);if !ok{return}
	items,err:=s.store.ListMCPTools(r.Context(),u.ClassID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"MCP 服务不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"tools":items})
}

func (s *Server) listMCPAudit(w http.ResponseWriter,r *http.Request){
	u,ok:=teacherUser(w,r);if !ok{return}
	items,err:=s.store.ListMCPCalls(r.Context(),u.ClassID)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"calls":items})
}
