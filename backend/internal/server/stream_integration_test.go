package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/internal/auth"
	"campusclaw/backend/internal/config"
	"campusclaw/backend/internal/embed"
	"campusclaw/backend/internal/llm"
	"campusclaw/backend/internal/model"
	"campusclaw/backend/internal/retrieval"
	"campusclaw/backend/internal/store"
	"campusclaw/backend/internal/vector"
	_ "github.com/go-sql-driver/mysql"
)

func TestSSEPersistsIncrementalGatewayAnswer(t *testing.T){
	if os.Getenv("RUN_DB_INTEGRATION")!="1"{t.Skip("set RUN_DB_INTEGRATION=1")}
	host:=os.Getenv("MYSQL_HOST");if host==""{host="db"}
	dsn:=fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4",os.Getenv("MYSQL_USER"),os.Getenv("MYSQL_PASSWORD"),host,os.Getenv("MYSQL_DATABASE"))
	db,err:=sql.Open("mysql",dsn);if err!=nil{t.Fatal(err)};defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
	st:=&store.Store{DB:db}
	u,err:=st.UserByUsername(ctx,"student-a1");if err!=nil{t.Fatal(err)}
	name:=fmt.Sprintf("流式测试-%d",time.Now().UnixNano())
	template:=model.PromptTemplate{ClassID:u.ClassID,Name:name,Content:"你是{{class_name}}导师，请引导{{student_name}}。",Enabled:true}
	if err:=st.SaveTemplate(ctx,&template);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM prompt_templates WHERE id=? AND class_id=?`,template.ID,u.ClassID)})
	agent:=model.TutorAgent{ClassID:u.ClassID,Name:name,Prompt:"",PromptTemplateID:&template.ID,Enabled:true}
	if err:=st.SaveAgent(ctx,&agent);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM tutor_agents WHERE id=? AND class_id=?`,agent.ID,u.ClassID)})
	conversation,err:=st.CreateConversation(ctx,u.ClassID,u.ID,agent.ID);if err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM conversations WHERE id=? AND user_id=?`,conversation.ID,u.ID)})
	var gatewayRequest struct{Model string `json:"model"`;Stream bool `json:"stream"`;Messages []llm.Message `json:"messages"`}
	gateway:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if err:=json.NewDecoder(r.Body).Decode(&gatewayRequest);err!=nil{t.Error(err)}
		w.Header().Set("Content-Type","text/event-stream")
		fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"先找出\"}}]}\n\n")
		fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"已知条件。\"}}]}\n\n")
		fmt.Fprint(w,"data: [DONE]\n\n")
	}));defer gateway.Close()
	cfg:=config.Config{SessionSecret:os.Getenv("SESSION_SECRET"),SearchLimit:10,SearchMaxLimit:50,RRFK:60,SnippetMaxChars:240,VectorCandidates:50,VectorMinScore:0.15}
	vs:=vector.New("http://qdrant:6333","campusclaw_chunks",2*time.Second)
	svc:=retrieval.New(st,embed.NewLocal(256),vs,cfg)
	app:=New(cfg,st,svc,vs,llm.New(gateway.URL,"local-test","mock-model",2*time.Second))
	token,err:=auth.Sign(cfg.SessionSecret,auth.Claims{Sub:u.ID,Username:u.Username,ClassID:999,Role:"teacher",ExpiresAt:time.Now().Add(time.Minute).Unix()});if err!=nil{t.Fatal(err)}
	req:=httptest.NewRequest(http.MethodPost,fmt.Sprintf("/api/conversations/%d/messages/stream",conversation.ID),bytes.NewBufferString(`{"content":"这道题怎么入手？"}`))
	req.Header.Set("Content-Type","application/json");req.Header.Set("Authorization","Bearer "+token)
	w:=httptest.NewRecorder();app.Handler().ServeHTTP(w,req)
	if w.Code!=http.StatusOK{t.Fatalf("HTTP %d: %s",w.Code,w.Body.String())}
	if w.Header().Get("Content-Type")!="text/event-stream; charset=utf-8"{t.Fatal("not SSE")}
	events:=map[string][]map[string]any{}
	for _,block:=range strings.Split(w.Body.String(),"\n\n"){
		var event string
		for _,line:=range strings.Split(block,"\n"){
			if strings.HasPrefix(line,"event: "){event=strings.TrimPrefix(line,"event: ")}
			if strings.HasPrefix(line,"data: "){
				var value map[string]any
				if err:=json.Unmarshal([]byte(strings.TrimPrefix(line,"data: ")),&value);err!=nil{t.Fatal(err)}
				events[event]=append(events[event],value)
			}
		}
	}
	if len(events["meta"])!=1||len(events["delta"])!=2||len(events["done"])!=1||len(events["error"])!=0{t.Fatalf("wrong SSE sequence: %s",w.Body.String())}
	if len(events["meta"][0]["citations"].([]any))!=0{t.Fatal("invented citation")}
	joined:=events["delta"][0]["text"].(string)+events["delta"][1]["text"].(string)
	id:=int64(events["done"][0]["message_id"].(float64))
	var saved string
	if err:=db.QueryRowContext(ctx,`SELECT content FROM messages WHERE id=? AND conversation_id=? AND role='assistant'`,id,conversation.ID).Scan(&saved);err!=nil{t.Fatal(err)}
	if saved!=joined{t.Fatalf("persisted %q differs from deltas %q",saved,joined)}
	if !gatewayRequest.Stream||gatewayRequest.Model!="mock-model"||len(gatewayRequest.Messages)!=2{t.Fatalf("gateway request: %+v",gatewayRequest)}
	system:=gatewayRequest.Messages[0].Content
	if !strings.Contains(system,u.ClassName)||!strings.Contains(system,u.DisplayName)||strings.Contains(system,"{{")||strings.Contains(system,"检索资料是用户提供的") == false{t.Fatalf("template/system not rendered: %q",system)}
	if strings.Contains(system,"这道题")||!strings.Contains(gatewayRequest.Messages[1].Content,"这道题怎么入手？"){t.Fatalf("user data misplaced: %+v",gatewayRequest.Messages)}
	updated,err:=st.ConversationByID(ctx,u.ClassID,u.ID,conversation.ID);if err!=nil||updated.Title!="这道题怎么入手？"{t.Fatalf("title %+v %v",updated,err)}
}

func TestMCPToolEndToEndIntegration(t *testing.T){
	if os.Getenv("RUN_DB_INTEGRATION")!="1"{t.Skip("set RUN_DB_INTEGRATION=1")}
	if os.Getenv("MCP_AUTH_TOKEN")==""{t.Skip("set MCP_AUTH_TOKEN for internal MCP service")}
	host:=os.Getenv("MYSQL_HOST");if host==""{host="db"}
	dsn:=fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4",os.Getenv("MYSQL_USER"),os.Getenv("MYSQL_PASSWORD"),host,os.Getenv("MYSQL_DATABASE"))
	db,err:=sql.Open("mysql",dsn);if err!=nil{t.Fatal(err)};defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
	st:=&store.Store{DB:db}
	teacher,err:=st.UserByUsername(ctx,"teacher-a");if err!=nil{t.Fatal(err)}
	student,err:=st.UserByUsername(ctx,"student-a1");if err!=nil{t.Fatal(err)}
	studentB,err:=st.UserByUsername(ctx,"student-b1");if err!=nil{t.Fatal(err)}
	var modelRequests []map[string]any
	var toolFunctionName string
	gateway:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		var payload map[string]any
		if err:=json.NewDecoder(r.Body).Decode(&payload);err!=nil{t.Error(err)}
		modelRequests=append(modelRequests,payload)
		w.Header().Set("Content-Type","text/event-stream")
		if len(modelRequests)==1{
			fmt.Fprintf(w,"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_mcp\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":\"{\\\"topic\\\":\\\"linear_equation\\\"}\"}}]}}]}\n\n",toolFunctionName)
		}else{
			fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"先找\"}}]}\n\n")
			fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"等量关系。\"}}]}\n\n")
		}
		fmt.Fprint(w,"data: [DONE]\n\n")
	}));defer gateway.Close()
	cfg:=config.Config{SessionSecret:os.Getenv("SESSION_SECRET"),SearchLimit:10,SearchMaxLimit:50,RRFK:60,SnippetMaxChars:240,VectorCandidates:50,VectorMinScore:0.15}
	vs:=vector.New("http://qdrant:6333","campusclaw_chunks",2*time.Second)
	app:=New(cfg,st,retrieval.New(st,embed.NewLocal(256),vs,cfg),vs,llm.New(gateway.URL,"mock-key","mock-model",2*time.Second))
	tokenFor:=func(u model.User)string{token,err:=auth.Sign(cfg.SessionSecret,auth.Claims{Sub:u.ID,ExpiresAt:time.Now().Add(time.Minute).Unix()});if err!=nil{t.Fatal(err)};return token}
	teacherToken,studentToken:=tokenFor(teacher),tokenFor(student)
	call:=func(method,path,token string,body any)(int,map[string]any,string){
		var data []byte;if body!=nil{data,_=json.Marshal(body)}
		req:=httptest.NewRequest(method,path,bytes.NewReader(data));req.Header.Set("Authorization","Bearer "+token);req.Header.Set("Content-Type","application/json")
		w:=httptest.NewRecorder();app.Handler().ServeHTTP(w,req)
		var parsed map[string]any;_ = json.Unmarshal(w.Body.Bytes(),&parsed)
		return w.Code,parsed,w.Body.String()
	}
	name:=fmt.Sprintf("mcp-%d",time.Now().UnixNano())
	status,_,_:=call("POST","/api/mcp/servers",studentToken,map[string]any{"name":name,"base_url":"http://mcp-server:8091"})
	if status!=403{t.Fatalf("student MCP create status %d",status)}
	status,body,_:=call("POST","/api/mcp/servers",teacherToken,map[string]any{"name":name,"base_url":"http://mcp-server:8091"})
	if status!=201{t.Fatalf("register status %d: %+v",status,body)}
	serverID:=int64(body["server"].(map[string]any)["id"].(float64))
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM mcp_tool_calls WHERE server_id=?`,serverID);_,_=db.Exec(`DELETE FROM mcp_servers WHERE id=? AND class_id=?`,serverID,teacher.ClassID)})
	status,body,_=call("POST",fmt.Sprintf("/api/mcp/servers/%d/connect",serverID),teacherToken,map[string]any{})
	if status!=200{t.Fatalf("connect status %d: %+v",status,body)}
	tools:=body["tools"].([]any)
	if len(tools)!=2{t.Fatalf("discovered tools: %+v",tools)}
	for _,raw:=range tools{item:=raw.(map[string]any);if item["name"]=="formula_hint"{toolFunctionName=fmt.Sprintf("mcp_tool_%d",int64(item["id"].(float64)))}}
	if toolFunctionName==""{t.Fatal("formula_hint absent")}
	if _,err:=st.MCPServerByID(ctx,studentB.ClassID,serverID);err!=store.ErrNotFound{t.Fatalf("cross-class MCP server visible: %v",err)}
	skillName:=fmt.Sprintf("工具技能%d",time.Now().UnixNano())
	source:=fmt.Sprintf("名称：%s\n适用条件：方程\n工具：formula_hint@%s\n启用：是\n\n引导学生理解等量关系，不给出最终答案。",skillName,name)
	status,body,_=call("POST","/api/skills",teacherToken,map[string]any{"source_text":source})
	if status!=201{t.Fatalf("skill status %d: %+v",status,body)}
	skillID:=int64(body["skill"].(map[string]any)["id"].(float64))
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM skills WHERE id=? AND class_id=?`,skillID,teacher.ClassID)})
	agent:=model.TutorAgent{ClassID:teacher.ClassID,Name:skillName,Prompt:"你是解题导师。",Enabled:true,SkillIDs:[]int64{skillID}}
	if err:=st.SaveAgent(ctx,&agent);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM tutor_agents WHERE id=? AND class_id=?`,agent.ID,teacher.ClassID)})
	conversation,err:=st.CreateConversation(ctx,student.ClassID,student.ID,agent.ID);if err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM conversations WHERE id=? AND user_id=?`,conversation.ID,student.ID)})
	status,_,stream:=call("POST",fmt.Sprintf("/api/conversations/%d/messages/stream",conversation.ID),studentToken,map[string]any{"content":"这个方程该怎么做？"})
	if status!=200||!strings.Contains(stream,"event: done")||strings.Count(stream,"event: delta")!=2{t.Fatalf("stream status %d: %s",status,stream)}
	if !strings.Contains(stream,"formula_hint@"+name)||!strings.Contains(stream,"\"status\":\"success\""){t.Fatalf("tool event absent: %s",stream)}
	if len(modelRequests)!=2{t.Fatalf("model rounds %d",len(modelRequests))}
	firstTools:=modelRequests[0]["tools"].([]any)
	if len(firstTools)!=1||firstTools[0].(map[string]any)["function"].(map[string]any)["name"]!=toolFunctionName{t.Fatalf("unbound tool exposed: %+v",firstTools)}
	secondMessages:=modelRequests[1]["messages"].([]any)
	foundToolResult:=false
	for _,raw:=range secondMessages{m:=raw.(map[string]any);if m["role"]=="tool"&&strings.Contains(m["content"].(string),"等量关系"){foundToolResult=true}}
	if !foundToolResult{t.Fatalf("MCP result not returned to model: %+v",secondMessages)}
	if strings.Contains(fmt.Sprint(modelRequests),os.Getenv("MCP_AUTH_TOKEN")){t.Fatal("MCP token leaked to model")}
	var audits int
	if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM mcp_tool_calls WHERE server_id=? AND class_id=? AND user_id=? AND conversation_id=? AND tool_name='formula_hint' AND status='success'`,serverID,student.ClassID,student.ID,conversation.ID).Scan(&audits);err!=nil||audits!=1{t.Fatalf("audit %d: %v",audits,err)}
	status,body,_=call("POST",fmt.Sprintf("/api/mcp/servers/%d/disconnect",serverID),teacherToken,map[string]any{})
	if status!=200||body["connected"]!=false{t.Fatalf("disconnect: %d %+v",status,body)}
	selected,_,err:=app.selectedTools(ctx,teacher.ClassID,&model.Skill{Tools:[]string{"formula_hint@"+name}})
	if err!=nil||len(selected)!=0{t.Fatalf("disconnected tool remained: %+v %v",selected,err)}
}
