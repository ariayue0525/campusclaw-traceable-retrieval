package store

import (
	"context"
	"database/sql"
	"errors"

	"campusclaw/backend/internal/model"
)

const templateColumns = `id,class_id,name,content,enabled,created_at,updated_at`

func scanTemplate(scan func(...any) error) (model.PromptTemplate, error) {
	var item model.PromptTemplate
	err := scan(&item.ID,&item.ClassID,&item.Name,&item.Content,&item.Enabled,&item.CreatedAt,&item.UpdatedAt)
	return item, err
}

func (s *Store) ListTemplates(ctx context.Context, classID int64) ([]model.PromptTemplate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+templateColumns+` FROM prompt_templates WHERE class_id=? ORDER BY id`, classID)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []model.PromptTemplate{}
	for rows.Next() {
		item, err := scanTemplate(rows.Scan)
		if err != nil { return nil, err }
		out = append(out,item)
	}
	return out, rows.Err()
}

func (s *Store) TemplateByID(ctx context.Context, classID,id int64) (model.PromptTemplate,error) {
	item,err := scanTemplate(s.DB.QueryRowContext(ctx,`SELECT `+templateColumns+` FROM prompt_templates WHERE class_id=? AND id=?`,classID,id).Scan)
	if errors.Is(err,sql.ErrNoRows) { return item,ErrNotFound }
	return item,err
}

func (s *Store) SaveTemplate(ctx context.Context,item *model.PromptTemplate) error {
	if item.ID==0 {
		res,err:=s.DB.ExecContext(ctx,`INSERT INTO prompt_templates(class_id,name,content,enabled) VALUES(?,?,?,?)`,item.ClassID,item.Name,item.Content,item.Enabled)
		if duplicate(err) { return ErrConflict }
		if err!=nil { return err }
		item.ID,err=res.LastInsertId(); return err
	}
	res,err:=s.DB.ExecContext(ctx,`UPDATE prompt_templates SET name=?,content=?,enabled=? WHERE id=? AND class_id=?`,item.Name,item.Content,item.Enabled,item.ID,item.ClassID)
	if duplicate(err) { return ErrConflict }
	if err!=nil { return err }
	n,err:=res.RowsAffected(); if err!=nil { return err }
	if n==0 { _,err=s.TemplateByID(ctx,item.ClassID,item.ID); return err }
	return nil
}
