package server

import (
	"errors"
	"net/http"
	"strconv"

	"campusclaw/backend/internal/store"
)

func (s *Server) conversationRoutes() {
	s.mux.Handle("GET /api/conversations",s.requireAuth(http.HandlerFunc(s.listConversations)))
	s.mux.Handle("POST /api/conversations",s.requireAuth(http.HandlerFunc(s.createConversation)))
	s.mux.Handle("GET /api/conversations/{id}",s.requireAuth(http.HandlerFunc(s.getConversation)))
	s.mux.Handle("GET /api/conversations/{id}/messages",s.requireAuth(http.HandlerFunc(s.listConversationMessages)))
	s.mux.Handle("POST /api/conversations/{id}/messages/stream",s.requireAuth(http.HandlerFunc(s.streamConversationMessage)))
}

func conversationPathID(w http.ResponseWriter,r *http.Request)(int64,bool){
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil||id<1{writeError(w,http.StatusNotFound,"会话不存在");return 0,false}
	return id,true
}

func (s *Server) listConversations(w http.ResponseWriter,r *http.Request){
	u:=currentUser(r)
	items,err:=s.store.ListConversations(r.Context(),u.ClassID,u.ID)
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"conversations":items})
}

func (s *Server) createConversation(w http.ResponseWriter,r *http.Request){
	u:=currentUser(r)
	var in struct{AgentID int64 `json:"agent_id"`}
	if err:=decodeJSON(r,&in);err!=nil||in.AgentID<1{writeError(w,http.StatusBadRequest,"请选择智能体");return}
	item,err:=s.store.CreateConversation(r.Context(),u.ClassID,u.ID,in.AgentID)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"智能体不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusCreated,map[string]any{"conversation":item})
}

func (s *Server) getConversation(w http.ResponseWriter,r *http.Request){
	u:=currentUser(r);id,ok:=conversationPathID(w,r);if !ok{return}
	item,err:=s.store.ConversationByID(r.Context(),u.ClassID,u.ID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"会话不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"conversation":item})
}

func (s *Server) listConversationMessages(w http.ResponseWriter,r *http.Request){
	u:=currentUser(r);id,ok:=conversationPathID(w,r);if !ok{return}
	items,err:=s.store.ListMessages(r.Context(),u.ClassID,u.ID,id)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"会话不存在");return}
	if err!=nil{writeError(w,http.StatusServiceUnavailable,"服务暂时不可用");return}
	writeJSON(w,http.StatusOK,map[string]any{"messages":items})
}
