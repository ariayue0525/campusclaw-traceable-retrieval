package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"campusclaw/backend/internal/skillfile"
	"campusclaw/backend/internal/store"
)

func (s *Server) skillRoutes() {
	s.mux.Handle("GET /api/skills", s.requireAuth(http.HandlerFunc(s.listSkills)))
	s.mux.Handle("POST /api/skills", s.requireAuth(http.HandlerFunc(s.createSkill)))
	s.mux.Handle("GET /api/skills/{id}", s.requireAuth(http.HandlerFunc(s.getSkill)))
	s.mux.Handle("PUT /api/skills/{id}", s.requireAuth(http.HandlerFunc(s.updateSkill)))
}

func skillPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 { writeError(w, http.StatusNotFound, "技能不存在"); return 0, false }
	return id, true
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	u, ok := teacherUser(w, r); if !ok { return }
	items, err := s.store.ListSkills(r.Context(), u.ClassID, false)
	if err != nil { writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return }
	writeJSON(w, http.StatusOK, map[string]any{"skills": items})
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	u, ok := teacherUser(w, r); if !ok { return }
	id, ok := skillPathID(w, r); if !ok { return }
	item, err := s.store.SkillByID(r.Context(), u.ClassID, id)
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "技能不存在"); return }
	if err != nil { writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return }
	writeJSON(w, http.StatusOK, map[string]any{"skill": item})
}

func readSkillSource(w http.ResponseWriter, r *http.Request) (string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "application/json":
		var in struct { SourceText string `json:"source_text"` }
		if err := decodeJSON(r, &in); err != nil { return "", err }
		return in.SourceText, nil
	case "multipart/form-data":
		if err := r.ParseMultipartForm(32 << 10); err != nil { return "", err }
		if file, _, err := r.FormFile("file"); err == nil {
			defer file.Close()
			raw, err := io.ReadAll(io.LimitReader(file, 32<<10))
			return string(raw), err
		}
		return r.FormValue("source_text"), nil
	case "text/plain":
		defer r.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
		return string(raw), err
	default:
		return "", skillfile.ErrInvalid
	}
}

func (s *Server) saveSkill(w http.ResponseWriter, r *http.Request, id int64) {
	u, ok := teacherUser(w, r); if !ok { return }
	source, err := readSkillSource(w, r)
	if err != nil || strings.TrimSpace(source) == "" { writeError(w, http.StatusBadRequest, "技能文件格式不正确"); return }
	item, err := skillfile.Parse(source)
	if err != nil { writeError(w, http.StatusBadRequest, "技能文件格式不正确"); return }
	item.ID, item.ClassID = id, u.ClassID
	err = s.store.SaveSkill(r.Context(), &item)
	switch {
	case errors.Is(err, store.ErrNotFound): writeError(w, http.StatusNotFound, "技能不存在"); return
	case errors.Is(err, store.ErrConflict): writeError(w, http.StatusBadRequest, "本班已有同名技能"); return
	case err != nil: writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return
	}
	item, err = s.store.SkillByID(r.Context(), u.ClassID, item.ID)
	if err != nil { writeError(w, http.StatusServiceUnavailable, "服务暂时不可用"); return }
	status := http.StatusOK; if id == 0 { status = http.StatusCreated }
	writeJSON(w, status, map[string]any{"skill": item})
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) { s.saveSkill(w, r, 0) }
func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := skillPathID(w, r); if ok { s.saveSkill(w, r, id) }
}
