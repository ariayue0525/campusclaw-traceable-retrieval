package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"campusclaw/backend/internal/model"
	"github.com/go-sql-driver/mysql"
)

var ErrConflict = errors.New("conflict")

const agentColumns = `a.id,a.class_id,a.name,a.prompt,a.enabled,a.created_at,a.updated_at,a.prompt_template_id`

func scanAgent(scan func(...any) error) (model.TutorAgent, error) {
	var a model.TutorAgent
	err := scan(&a.ID, &a.ClassID, &a.Name, &a.Prompt, &a.Enabled, &a.CreatedAt, &a.UpdatedAt, &a.PromptTemplateID)
	a.SkillIDs, a.MaterialIDs = []int64{}, []int64{}
	return a, err
}

func (s *Store) ListAgents(ctx context.Context, classID int64, enabledOnly bool) ([]model.TutorAgent, error) {
	query := `SELECT ` + agentColumns + ` FROM tutor_agents a WHERE a.class_id=?`
	if enabledOnly {
		query += ` AND a.enabled=TRUE`
	}
	query += ` ORDER BY a.id`
	rows, err := s.DB.QueryContext(ctx, query, classID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TutorAgent{}
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return nil, err
		}
		if err := s.loadAgentBindings(ctx, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AgentByID(ctx context.Context, id, classID int64, enabledOnly bool) (model.TutorAgent, error) {
	query := `SELECT ` + agentColumns + ` FROM tutor_agents a WHERE a.id=? AND a.class_id=?`
	if enabledOnly {
		query += ` AND a.enabled=TRUE`
	}
	a, err := scanAgent(s.DB.QueryRowContext(ctx, query, id, classID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if err := s.loadAgentBindings(ctx, &a); err != nil {
		return a, err
	}
	return a, nil
}

func (s *Store) loadAgentBindings(ctx context.Context, a *model.TutorAgent) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT skill_id FROM tutor_agent_skills WHERE agent_id=? ORDER BY skill_id`, a.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		a.SkillIDs = append(a.SkillIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT material_id FROM tutor_agent_materials WHERE agent_id=? ORDER BY material_id`, a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		a.MaterialIDs = append(a.MaterialIDs, id)
	}
	return rows.Err()
}

func (s *Store) BoundSkills(ctx context.Context,classID,agentID int64)([]model.Skill,error){
	rows,err:=s.DB.QueryContext(ctx,`SELECT `+skillColumns+` FROM skills WHERE class_id=? AND enabled=TRUE AND id IN (SELECT skill_id FROM tutor_agent_skills WHERE agent_id=?) ORDER BY id`,classID,agentID)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.Skill{}
	for rows.Next(){item,err:=scanSkill(rows.Scan);if err!=nil{return nil,err};out=append(out,item)}
	return out,rows.Err()
}

// AgentMaterialIDs resolves the authorization set before any retrieval path.
// Subject limits the set by material title; it is never applied after search.
func (s *Store) AgentMaterialIDs(ctx context.Context,classID,agentID int64,subject string)([]int64,error){
	rows,err:=s.DB.QueryContext(ctx,`SELECT m.id FROM tutor_agent_materials b JOIN tutor_agents a ON a.id=b.agent_id JOIN materials m ON m.id=b.material_id WHERE a.id=? AND a.class_id=? AND m.class_id=? AND (?='' OR INSTR(m.title,?)>0) ORDER BY m.id`,agentID,classID,classID,subject,subject)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]int64{}
	for rows.Next(){var id int64;if err:=rows.Scan(&id);err!=nil{return nil,err};out=append(out,id)}
	return out,rows.Err()
}

func duplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

// SaveAgent atomically validates and replaces both binding sets. SELECT ...
// FOR SHARE prevents a bound row from disappearing until commit; the agent row
// is locked FOR UPDATE during edits so concurrent updates serialize.
func (s *Store) SaveAgent(ctx context.Context, a *model.TutorAgent) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := validateIDsInClass(ctx, tx, `skills`, a.ClassID, a.SkillIDs); err != nil {
		return err
	}
	if err := validateIDsInClass(ctx, tx, `materials`, a.ClassID, a.MaterialIDs); err != nil {
		return err
	}
	if a.PromptTemplateID != nil {
		if err := validateIDsInClass(ctx, tx, `prompt_templates`, a.ClassID, []int64{*a.PromptTemplateID}); err != nil { return err }
	}
	if a.ID == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO tutor_agents(class_id,name,prompt,enabled,prompt_template_id) VALUES(?,?,?,?,?)`, a.ClassID, a.Name, a.Prompt, a.Enabled, a.PromptTemplateID)
		if duplicate(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		a.ID, err = res.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		var lockedID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM tutor_agents WHERE id=? AND class_id=? FOR UPDATE`, a.ID, a.ClassID).Scan(&lockedID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tutor_agents SET name=?,prompt=?,enabled=?,prompt_template_id=? WHERE id=? AND class_id=?`, a.Name, a.Prompt, a.Enabled, a.PromptTemplateID, a.ID, a.ClassID)
		if duplicate(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tutor_agent_skills WHERE agent_id=?`, a.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tutor_agent_materials WHERE agent_id=?`, a.ID); err != nil {
			return err
		}
	}
	for _, id := range a.SkillIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tutor_agent_skills(agent_id,skill_id) VALUES(?,?)`, a.ID, id); err != nil {
			return err
		}
	}
	for _, id := range a.MaterialIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tutor_agent_materials(agent_id,material_id) VALUES(?,?)`, a.ID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateIDsInClass(ctx context.Context, tx *sql.Tx, table string, classID int64, ids []int64) error {
	ordered := append([]int64(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, id := range ordered {
		var found int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE id=? AND class_id=? FOR SHARE`, id, classID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	return nil
}
