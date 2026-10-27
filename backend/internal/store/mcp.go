package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"

	"campusclaw/backend/internal/model"
)

const mcpServerColumns=`id,class_id,name,base_url,token_env_key,connected,created_at,updated_at`

func scanMCPServer(scan func(...any)error)(model.MCPServer,error){
	var item model.MCPServer
	err:=scan(&item.ID,&item.ClassID,&item.Name,&item.BaseURL,&item.TokenEnvKey,&item.Connected,&item.CreatedAt,&item.UpdatedAt)
	item.HasToken=os.Getenv(item.TokenEnvKey)!=""
	return item,err
}

func (s *Store) ListMCPServers(ctx context.Context,classID int64)([]model.MCPServer,error){
	rows,err:=s.DB.QueryContext(ctx,`SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? ORDER BY id`,classID)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.MCPServer{}
	for rows.Next(){item,err:=scanMCPServer(rows.Scan);if err!=nil{return nil,err};out=append(out,item)}
	return out,rows.Err()
}

func (s *Store) MCPServerByID(ctx context.Context,classID,id int64)(model.MCPServer,error){
	item,err:=scanMCPServer(s.DB.QueryRowContext(ctx,`SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND id=?`,classID,id).Scan)
	if errors.Is(err,sql.ErrNoRows){return item,ErrNotFound}
	return item,err
}

func (s *Store) SaveMCPServer(ctx context.Context,item *model.MCPServer)error{
	if item.ID==0{
		res,err:=s.DB.ExecContext(ctx,`INSERT INTO mcp_servers(class_id,name,base_url,token_env_key) VALUES(?,?,?,?)`,item.ClassID,item.Name,item.BaseURL,item.TokenEnvKey)
		if duplicate(err){return ErrConflict};if err!=nil{return err}
		item.ID,err=res.LastInsertId();return err
	}
	tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
	var found int64
	err=tx.QueryRowContext(ctx,`SELECT id FROM mcp_servers WHERE id=? AND class_id=? FOR UPDATE`,item.ID,item.ClassID).Scan(&found)
	if errors.Is(err,sql.ErrNoRows){return ErrNotFound};if err!=nil{return err}
	_,err=tx.ExecContext(ctx,`UPDATE mcp_servers SET name=?,base_url=?,token_env_key=?,connected=FALSE WHERE id=? AND class_id=?`,item.Name,item.BaseURL,item.TokenEnvKey,item.ID,item.ClassID)
	if duplicate(err){return ErrConflict};if err!=nil{return err}
	if _,err=tx.ExecContext(ctx,`DELETE FROM mcp_tools WHERE server_id=?`,item.ID);err!=nil{return err}
	return tx.Commit()
}

func (s *Store) ReplaceMCPTools(ctx context.Context,classID,serverID int64,items []model.MCPTool)error{
	tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
	var found int64
	err=tx.QueryRowContext(ctx,`SELECT id FROM mcp_servers WHERE id=? AND class_id=? FOR UPDATE`,serverID,classID).Scan(&found)
	if errors.Is(err,sql.ErrNoRows){return ErrNotFound};if err!=nil{return err}
	if _,err=tx.ExecContext(ctx,`DELETE FROM mcp_tools WHERE server_id=?`,serverID);err!=nil{return err}
	for _,item:=range items{
		if _,err=tx.ExecContext(ctx,`INSERT INTO mcp_tools(server_id,name,description,input_schema) VALUES(?,?,?,?)`,serverID,item.Name,item.Description,[]byte(item.InputSchema));err!=nil{return err}
	}
	if _,err=tx.ExecContext(ctx,`UPDATE mcp_servers SET connected=TRUE WHERE id=?`,serverID);err!=nil{return err}
	return tx.Commit()
}

func (s *Store) DisconnectMCPServer(ctx context.Context,classID,serverID int64)error{
	tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
	var found int64
	err=tx.QueryRowContext(ctx,`SELECT id FROM mcp_servers WHERE id=? AND class_id=? FOR UPDATE`,serverID,classID).Scan(&found)
	if errors.Is(err,sql.ErrNoRows){return ErrNotFound};if err!=nil{return err}
	if _,err=tx.ExecContext(ctx,`DELETE FROM mcp_tools WHERE server_id=?`,serverID);err!=nil{return err}
	if _,err=tx.ExecContext(ctx,`UPDATE mcp_servers SET connected=FALSE WHERE id=?`,serverID);err!=nil{return err}
	return tx.Commit()
}

func (s *Store) ListMCPTools(ctx context.Context,classID,serverID int64)([]model.MCPTool,error){
	if _,err:=s.MCPServerByID(ctx,classID,serverID);err!=nil{return nil,err}
	rows,err:=s.DB.QueryContext(ctx,`SELECT id,server_id,name,description,input_schema FROM mcp_tools WHERE server_id=? ORDER BY name`,serverID)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.MCPTool{}
	for rows.Next(){var item model.MCPTool;if err:=rows.Scan(&item.ID,&item.ServerID,&item.Name,&item.Description,&item.InputSchema);err!=nil{return nil,err};out=append(out,item)}
	return out,rows.Err()
}

func (s *Store) MCPToolByAlias(ctx context.Context,classID int64,alias string)(model.MCPTool,model.MCPServer,error){
	var tool model.MCPTool
	var server model.MCPServer
	name,serverName,ok:=strings.Cut(alias,"@");if !ok||name==""||serverName==""{return tool,server,ErrNotFound}
	err:=s.DB.QueryRowContext(ctx,`SELECT t.id,t.server_id,t.name,t.description,t.input_schema,s.id,s.class_id,s.name,s.base_url,s.token_env_key,s.connected,s.created_at,s.updated_at FROM mcp_tools t JOIN mcp_servers s ON s.id=t.server_id WHERE s.class_id=? AND s.connected=TRUE AND s.name=? AND t.name=?`,classID,serverName,name).Scan(&tool.ID,&tool.ServerID,&tool.Name,&tool.Description,&tool.InputSchema,&server.ID,&server.ClassID,&server.Name,&server.BaseURL,&server.TokenEnvKey,&server.Connected,&server.CreatedAt,&server.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows){return tool,server,ErrNotFound}
	server.HasToken=os.Getenv(server.TokenEnvKey)!=""
	return tool,server,err
}

func (s *Store) RecordMCPCall(ctx context.Context,item model.MCPCall)error{
	_,err:=s.DB.ExecContext(ctx,`INSERT INTO mcp_tool_calls(class_id,user_id,conversation_id,server_id,tool_name,status,detail) VALUES(?,?,?,?,?,?,?)`,item.ClassID,item.UserID,item.ConversationID,item.ServerID,item.ToolName,item.Status,item.Detail)
	return err
}

func (s *Store) ListMCPCalls(ctx context.Context,classID int64)([]model.MCPCall,error){
	rows,err:=s.DB.QueryContext(ctx,`SELECT id,class_id,user_id,conversation_id,server_id,tool_name,status,detail,created_at FROM mcp_tool_calls WHERE class_id=? ORDER BY id DESC LIMIT 200`,classID)
	if err!=nil{return nil,err};defer rows.Close()
	out:=[]model.MCPCall{}
	for rows.Next(){var item model.MCPCall;if err:=rows.Scan(&item.ID,&item.ClassID,&item.UserID,&item.ConversationID,&item.ServerID,&item.ToolName,&item.Status,&item.Detail,&item.CreatedAt);err!=nil{return nil,err};out=append(out,item)}
	return out,rows.Err()
}
