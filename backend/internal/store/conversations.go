package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"campusclaw/backend/internal/model"
)

const conversationColumns = `id,class_id,user_id,agent_id,title,created_at,updated_at`

func scanConversation(scan func(...any) error) (model.Conversation,error) {
	var c model.Conversation
	err:=scan(&c.ID,&c.ClassID,&c.UserID,&c.AgentID,&c.Title,&c.CreatedAt,&c.UpdatedAt)
	return c,err
}

func (s *Store) CreateConversation(ctx context.Context,classID,userID,agentID int64)(model.Conversation,error){
	var empty model.Conversation
	tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return empty,err};defer tx.Rollback()
	var found int64
	err=tx.QueryRowContext(ctx,`SELECT id FROM tutor_agents WHERE id=? AND class_id=? AND enabled=TRUE FOR SHARE`,agentID,classID).Scan(&found)
	if errors.Is(err,sql.ErrNoRows){return empty,ErrNotFound};if err!=nil{return empty,err}
	res,err:=tx.ExecContext(ctx,`INSERT INTO conversations(class_id,user_id,agent_id) VALUES(?,?,?)`,classID,userID,agentID)
	if err!=nil{return empty,err}
	id,err:=res.LastInsertId();if err!=nil{return empty,err}
	if err:=tx.Commit();err!=nil{return empty,err}
	return s.ConversationByID(ctx,classID,userID,id)
}

func (s *Store) ConversationByID(ctx context.Context,classID,userID,id int64)(model.Conversation,error){
	c,err:=scanConversation(s.DB.QueryRowContext(ctx,`SELECT `+conversationColumns+` FROM conversations WHERE id=? AND class_id=? AND user_id=?`,id,classID,userID).Scan)
	if errors.Is(err,sql.ErrNoRows){return c,ErrNotFound}
	return c,err
}

func (s *Store) ListConversations(ctx context.Context,classID,userID int64)([]model.Conversation,error){
	rows,err:=s.DB.QueryContext(ctx,`SELECT `+conversationColumns+` FROM conversations WHERE class_id=? AND user_id=? ORDER BY updated_at DESC,id DESC`,classID,userID)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.Conversation{}
	for rows.Next(){c,err:=scanConversation(rows.Scan);if err!=nil{return nil,err};out=append(out,c)}
	return out,rows.Err()
}

func scanMessage(scan func(...any)error)(model.Message,error){
	var m model.Message
	var skill sql.NullString
	err:=scan(&m.ID,&m.ConversationID,&m.Role,&m.Content,&m.Citations,&skill,&m.CreatedAt)
	if skill.Valid{m.SkillName=skill.String}
	return m,err
}

func (s *Store) ListMessages(ctx context.Context,classID,userID,conversationID int64)([]model.Message,error){
	if _,err:=s.ConversationByID(ctx,classID,userID,conversationID);err!=nil{return nil,err}
	rows,err:=s.DB.QueryContext(ctx,`SELECT id,conversation_id,role,content,citations_json,skill_name,created_at FROM messages WHERE conversation_id=? ORDER BY id`,conversationID)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.Message{}
	for rows.Next(){m,err:=scanMessage(rows.Scan);if err!=nil{return nil,err};out=append(out,m)}
	return out,rows.Err()
}

func firstRunes(value string,n int)string{
	if utf8.RuneCountInString(value)<=n{return value}
	return string([]rune(value)[:n])
}

// RecentMessages returns chronological context after reading at most six rows.
// Truncation applies only to model context; the stored transcript is complete.
func (s *Store) RecentMessages(ctx context.Context,classID,userID,conversationID int64)([]model.Message,error){
	return s.RecentMessagesThrough(ctx,classID,userID,conversationID,0)
}

func (s *Store) RecentMessagesThrough(ctx context.Context,classID,userID,conversationID,maxID int64)([]model.Message,error){
	if _,err:=s.ConversationByID(ctx,classID,userID,conversationID);err!=nil{return nil,err}
	query:=`SELECT id,conversation_id,role,content,citations_json,skill_name,created_at FROM messages WHERE conversation_id=?`
	args:=[]any{conversationID}
	if maxID>0{query+=` AND id<=?`;args=append(args,maxID)}
	query+=` ORDER BY id DESC LIMIT 6`
	rows,err:=s.DB.QueryContext(ctx,query,args...)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.Message{}
	for rows.Next(){m,err:=scanMessage(rows.Scan);if err!=nil{return nil,err};m.Content=firstRunes(m.Content,500);out=append(out,m)}
	if err:=rows.Err();err!=nil{return nil,err}
	for i,j:=0,len(out)-1;i<j;i,j=i+1,j-1{out[i],out[j]=out[j],out[i]}
	return out,nil
}

func (s *Store) SaveMessage(ctx context.Context,classID,userID,conversationID int64,role,content string,citations json.RawMessage,skillName string)(model.Message,error){
	var empty model.Message
	if role!="user"&&role!="assistant"{return empty,errors.New("invalid role")}
	if len(citations)==0{citations=json.RawMessage(`[]`)}
	if !json.Valid(citations){return empty,errors.New("invalid citations")}
	tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return empty,err};defer tx.Rollback()
	var existingCount int
	err=tx.QueryRowContext(ctx,`SELECT (SELECT COUNT(*) FROM messages WHERE conversation_id=c.id) FROM conversations c WHERE c.id=? AND c.class_id=? AND c.user_id=? FOR UPDATE`,conversationID,classID,userID).Scan(&existingCount)
	if errors.Is(err,sql.ErrNoRows){return empty,ErrNotFound};if err!=nil{return empty,err}
	res,err:=tx.ExecContext(ctx,`INSERT INTO messages(conversation_id,role,content,citations_json,skill_name) VALUES(?,?,?,?,?)`,conversationID,role,content,[]byte(citations),nullableSkill(skillName))
	if err!=nil{return empty,err}
	id,err:=res.LastInsertId();if err!=nil{return empty,err}
	if role=="user"&&existingCount==0{
		_,err=tx.ExecContext(ctx,`UPDATE conversations SET title=?,updated_at=NOW(6) WHERE id=?`,firstRunes(content,24),conversationID)
	}else{
		_,err=tx.ExecContext(ctx,`UPDATE conversations SET updated_at=NOW(6) WHERE id=?`,conversationID)
	}
	if err!=nil{return empty,err}
	if err:=tx.Commit();err!=nil{return empty,err}
	m,err:=scanMessage(s.DB.QueryRowContext(ctx,`SELECT id,conversation_id,role,content,citations_json,skill_name,created_at FROM messages WHERE id=?`,id).Scan)
	return m,err
}

func nullableSkill(value string) any {if value==""{return nil};return value}
