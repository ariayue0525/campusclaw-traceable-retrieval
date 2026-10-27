package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"campusclaw/backend/internal/model"
)

const skillColumns = `id,class_id,name,trigger_text,subject_text,description,tools_json,source_text,enabled,seed_key,created_at,updated_at`

func scanSkill(scan func(...any) error) (model.Skill, error) {
	var item model.Skill
	var raw []byte
	var seed sql.NullString
	err := scan(&item.ID, &item.ClassID, &item.Name, &item.TriggerText, &item.SubjectText, &item.Description, &raw, &item.SourceText, &item.Enabled, &seed, &item.CreatedAt, &item.UpdatedAt)
	if err != nil { return item, err }
	if err := json.Unmarshal(raw, &item.Tools); err != nil { return item, err }
	if item.Tools == nil { item.Tools = []string{} }
	if seed.Valid { item.SeedKey = seed.String }
	return item, nil
}

func (s *Store) ListSkills(ctx context.Context, classID int64, enabledOnly bool) ([]model.Skill, error) {
	query := `SELECT ` + skillColumns + ` FROM skills WHERE class_id=?`
	if enabledOnly { query += ` AND enabled=TRUE` }
	query += ` ORDER BY id`
	rows, err := s.DB.QueryContext(ctx, query, classID)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []model.Skill{}
	for rows.Next() {
		item, err := scanSkill(rows.Scan)
		if err != nil { return nil, err }
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SkillByID(ctx context.Context, classID, id int64) (model.Skill, error) {
	item, err := scanSkill(s.DB.QueryRowContext(ctx, `SELECT `+skillColumns+` FROM skills WHERE class_id=? AND id=?`, classID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) { return item, ErrNotFound }
	return item, err
}

// SaveSkill scopes every write to the authenticated class; a copied ID from
// another class has exactly the same outcome as an unknown ID.
func (s *Store) SaveSkill(ctx context.Context, item *model.Skill) error {
	tools, err := json.Marshal(item.Tools)
	if err != nil { return err }
	tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
	for _,alias:=range item.Tools{
		if alias=="knowledge_search"{continue}
		name,serverName,ok:=strings.Cut(alias,"@");if !ok||name==""||serverName==""{return ErrNotFound}
		var toolID int64
		err:=tx.QueryRowContext(ctx,`SELECT t.id FROM mcp_tools t JOIN mcp_servers m ON m.id=t.server_id WHERE m.class_id=? AND m.connected=TRUE AND m.name=? AND t.name=? FOR SHARE`,item.ClassID,serverName,name).Scan(&toolID)
		if errors.Is(err,sql.ErrNoRows){return ErrNotFound};if err!=nil{return err}
	}
	if item.ID == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO skills(class_id,name,trigger_text,subject_text,description,tools_json,source_text,enabled) VALUES(?,?,?,?,?,?,?,?)`, item.ClassID,item.Name,item.TriggerText,item.SubjectText,item.Description,tools,item.SourceText,item.Enabled)
		if duplicate(err) { return ErrConflict }
		if err != nil { return err }
		item.ID, err = res.LastInsertId()
		if err!=nil{return err}
		return tx.Commit()
	}
	res, err := tx.ExecContext(ctx, `UPDATE skills SET name=?,trigger_text=?,subject_text=?,description=?,tools_json=?,source_text=?,enabled=? WHERE id=? AND class_id=?`, item.Name,item.TriggerText,item.SubjectText,item.Description,tools,item.SourceText,item.Enabled,item.ID,item.ClassID)
	if duplicate(err) { return ErrConflict }
	if err != nil { return err }
	n, err := res.RowsAffected()
	if err != nil { return err }
	if n == 0 {
		// MySQL reports changed rows, not matched rows. Unchanged edits still
		// succeed, but an absent/cross-class ID must remain indistinguishable.
		var found int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM skills WHERE id=? AND class_id=?`, item.ID,item.ClassID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) { return ErrNotFound }
		if err!=nil{return err}
	}
	return tx.Commit()
}
