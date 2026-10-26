package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"campusclaw/backend/internal/model"
	"campusclaw/backend/internal/store"
)

func (s *Server) agentRoutes() {
	s.mux.Handle("GET /api/agents", s.requireAuth(http.HandlerFunc(s.listAgents)))
	s.mux.Handle("POST /api/agents", s.requireAuth(http.HandlerFunc(s.createAgent)))
	s.mux.Handle("GET /api/agents/{id}", s.requireAuth(http.HandlerFunc(s.getAgent)))
	s.mux.Handle("PUT /api/agents/{id}", s.requireAuth(http.HandlerFunc(s.updateAgent)))
}

func teacherUser(w http.ResponseWriter, r *http.Request) (model.User, bool) {
	u := currentUser(r)
	if u.Role != "teacher" {
		writeError(w, http.StatusForbidden, "只有教师可以管理智能体")
		return u, false
	}
	return u, true
}

func agentPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "智能体不存在")
		return 0, false
	}
	return id, true
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	items, err := s.store.ListAgents(r.Context(), u.ClassID, u.Role != "teacher")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": items})
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, ok := agentPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.AgentByID(r.Context(), id, u.ClassID, u.Role != "teacher")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "智能体不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent": item})
}

type agentRequest struct {
	Name        string  `json:"name"`
	Prompt      string  `json:"prompt"`
	Enabled     bool    `json:"enabled"`
	SkillIDs    []int64 `json:"skill_ids"`
	MaterialIDs []int64 `json:"material_ids"`
}

func validateAgentRequest(in agentRequest) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("智能体名称不能为空")
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Name)) > 64 {
		return errors.New("智能体名称不能超过 64 字")
	}
	if utf8.RuneCountInString(in.Prompt) > 2000 {
		return errors.New("角色提示词不能超过 2000 字")
	}
	if duplicateID(in.SkillIDs) || duplicateID(in.MaterialIDs) {
		return errors.New("绑定编号不能重复")
	}
	return nil
}

func duplicateID(ids []int64) bool {
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

func (s *Server) saveAgent(w http.ResponseWriter, r *http.Request, id int64) {
	u, ok := teacherUser(w, r)
	if !ok {
		return
	}
	var in agentRequest
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if err := validateAgentRequest(in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item := model.TutorAgent{ID: id, ClassID: u.ClassID, Name: strings.TrimSpace(in.Name), Prompt: strings.TrimSpace(in.Prompt), Enabled: in.Enabled, SkillIDs: in.SkillIDs, MaterialIDs: in.MaterialIDs}
	err := s.store.SaveAgent(r.Context(), &item)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "智能体或绑定资源不存在")
		return
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusBadRequest, "本班已有同名智能体")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用")
		return
	}
	item, err = s.store.AgentByID(r.Context(), item.ID, u.ClassID, false)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用")
		return
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"agent": item})
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) { s.saveAgent(w, r, 0) }

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := agentPathID(w, r)
	if ok {
		s.saveAgent(w, r, id)
	}
}
