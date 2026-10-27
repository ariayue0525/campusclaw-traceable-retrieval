package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/internal/model"
)

func TestConversationPersistenceIntegration(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION")!="1" { t.Skip("set RUN_DB_INTEGRATION=1 with test database credentials") }
	for _,key:=range []string{"MYSQL_USER","MYSQL_PASSWORD","MYSQL_DATABASE"}{if os.Getenv(key)==""{t.Fatalf("missing %s",key)}}
	host:=os.Getenv("MYSQL_HOST");if host==""{host="db"}
	dsn:=fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4",os.Getenv("MYSQL_USER"),os.Getenv("MYSQL_PASSWORD"),host,os.Getenv("MYSQL_DATABASE"))
	db,err:=sql.Open("mysql",dsn);if err!=nil{t.Fatal(err)};defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
	st:=&Store{DB:db}
	var classID,userID,otherID,agentID int64
	err=db.QueryRowContext(ctx,`SELECT u.class_id,u.id,(SELECT id FROM users WHERE username='teacher-a'),(SELECT id FROM tutor_agents WHERE class_id=u.class_id AND enabled=TRUE ORDER BY id LIMIT 1) FROM users u WHERE u.username='student-a1'`).Scan(&classID,&userID,&otherID,&agentID)
	if err!=nil{t.Fatal(err)}
	c,err:=st.CreateConversation(ctx,classID,userID,agentID);if err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_ = db.Exec(`DELETE FROM conversations WHERE id=? AND user_id=?`,c.ID,userID)})
	if _,err:=st.ConversationByID(ctx,classID,otherID,c.ID);err!=ErrNotFound{t.Fatalf("other user read: %v",err)}
	question:=strings.Repeat("题",30)
	for i:=0;i<7;i++{
		content:=fmt.Sprintf("%d-%s",i,question)
		if _,err:=st.SaveMessage(ctx,classID,userID,c.ID,"user",content,nil,"");err!=nil{t.Fatal(err)}
	}
	updated,err:=st.ConversationByID(ctx,classID,userID,c.ID);if err!=nil{t.Fatal(err)}
	if updated.Title!=firstRunes("0-"+question,24){t.Fatalf("title: %q",updated.Title)}
	full,err:=st.ListMessages(ctx,classID,userID,c.ID);if err!=nil||len(full)!=7{t.Fatalf("full messages %d: %v",len(full),err)}
	recent,err:=st.RecentMessages(ctx,classID,userID,c.ID);if err!=nil||len(recent)!=6{t.Fatalf("recent messages %d: %v",len(recent),err)}
	if !strings.HasPrefix(recent[0].Content,"1-")||!strings.HasPrefix(recent[5].Content,"6-"){t.Fatalf("wrong order: %+v",recent)}
	if _,err:=st.ListMessages(ctx,classID,otherID,c.ID);err!=ErrNotFound{t.Fatalf("other user messages: %v",err)}
}

func TestScopedRetrievalIntegration(t *testing.T){
	if os.Getenv("RUN_DB_INTEGRATION")!="1"{t.Skip("set RUN_DB_INTEGRATION=1")}
	host:=os.Getenv("MYSQL_HOST");if host==""{host="db"}
	dsn:=fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4",os.Getenv("MYSQL_USER"),os.Getenv("MYSQL_PASSWORD"),host,os.Getenv("MYSQL_DATABASE"))
	db,err:=sql.Open("mysql",dsn);if err!=nil{t.Fatal(err)};defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
	st:=&Store{DB:db}
	var classID,agentID,allowedMaterial,allowedChunk int64
	err=db.QueryRowContext(ctx,`SELECT a.class_id,a.id,m.id,c.id FROM tutor_agents a JOIN tutor_agent_materials b ON b.agent_id=a.id JOIN materials m ON m.id=b.material_id JOIN knowledge_chunks c ON c.material_id=m.id AND c.index_status='ready' WHERE a.name='启发式解题助手' AND a.class_id=(SELECT id FROM classes WHERE name='A班') ORDER BY c.id LIMIT 1`).Scan(&classID,&agentID,&allowedMaterial,&allowedChunk)
	if err!=nil{t.Fatal(err)}
	allowed,err:=st.AgentMaterialIDs(ctx,classID,agentID,"");if err!=nil{t.Fatal(err)}
	if !containsID(allowed,allowedMaterial){t.Fatalf("bound material %d absent: %v",allowedMaterial,allowed)}
	allowedChunks,err:=st.ChunksByIDsScoped(ctx,classID,[]int64{allowedMaterial},[]int64{allowedChunk})
	if err!=nil||len(allowedChunks)!=1{t.Fatalf("allowed chunk: %v %v",allowedChunks,err)}
	blocked,err:=st.ChunksByIDsScoped(ctx,classID,[]int64{},[]int64{allowedChunk})
	if err!=nil||len(blocked)!=0{t.Fatalf("empty allow-list leaked: %v %v",blocked,err)}
	var otherMaterial,otherChunk int64
	err=db.QueryRowContext(ctx,`SELECT m.id,c.id FROM materials m JOIN knowledge_chunks c ON c.material_id=m.id AND c.index_status='ready' WHERE m.class_id=? AND m.id<>? ORDER BY c.id LIMIT 1`,classID,allowedMaterial).Scan(&otherMaterial,&otherChunk)
	if err==nil{
		blocked,err=st.ChunksByIDsScoped(ctx,classID,[]int64{allowedMaterial},[]int64{otherChunk})
		if err!=nil||len(blocked)!=0{t.Fatalf("unbound material leaked: %v %v",blocked,err)}
	}
	var title string
	if err:=db.QueryRowContext(ctx,`SELECT title FROM materials WHERE id=?`,allowedMaterial).Scan(&title);err!=nil{t.Fatal(err)}
	byTitle,err:=st.AgentMaterialIDs(ctx,classID,agentID,title);if err!=nil||!containsID(byTitle,allowedMaterial){t.Fatalf("subject filter: %v %v",byTitle,err)}
	missing,err:=st.AgentMaterialIDs(ctx,classID,agentID,"不存在的科目xyz");if err!=nil||len(missing)!=0{t.Fatalf("subject restriction leaked: %v %v",missing,err)}
	keyword,err:=st.KeywordSearchScoped(ctx,classID,[]int64{allowedMaterial},"班级",10);if err!=nil{t.Fatal(err)}
	for _,row:=range keyword{if row.Chunk.MaterialID!=allowedMaterial{t.Fatalf("keyword leaked material %d",row.Chunk.MaterialID)}}
}

func containsID(ids []int64,want int64)bool{for _,id:=range ids{if id==want{return true}};return false}

func TestBoundSkillChangesIntegration(t *testing.T){
	if os.Getenv("RUN_DB_INTEGRATION")!="1"{t.Skip("set RUN_DB_INTEGRATION=1")}
	host:=os.Getenv("MYSQL_HOST");if host==""{host="db"}
	dsn:=fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4",os.Getenv("MYSQL_USER"),os.Getenv("MYSQL_PASSWORD"),host,os.Getenv("MYSQL_DATABASE"))
	db,err:=sql.Open("mysql",dsn);if err!=nil{t.Fatal(err)};defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
	st:=&Store{DB:db}
	var classID int64
	if err:=db.QueryRowContext(ctx,`SELECT id FROM classes WHERE name='A班'`).Scan(&classID);err!=nil{t.Fatal(err)}
	name:=fmt.Sprintf("路由测试-%d",time.Now().UnixNano())
	skill:=model.Skill{ClassID:classID,Name:name,TriggerText:"方程",Description:"只提问",Tools:[]string{"knowledge_search"},SourceText:"测试",Enabled:true}
	if err:=st.SaveSkill(ctx,&skill);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM skills WHERE id=? AND class_id=?`,skill.ID,classID)})
	agent:=model.TutorAgent{ClassID:classID,Name:name,Prompt:"引导",Enabled:true,SkillIDs:[]int64{skill.ID}}
	if err:=st.SaveAgent(ctx,&agent);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_,_=db.Exec(`DELETE FROM tutor_agents WHERE id=? AND class_id=?`,agent.ID,classID)})
	bound,err:=st.BoundSkills(ctx,classID,agent.ID);if err!=nil||len(bound)!=1||bound[0].ID!=skill.ID{t.Fatalf("bound: %+v %v",bound,err)}
	agent.SkillIDs=[]int64{}
	if err:=st.SaveAgent(ctx,&agent);err!=nil{t.Fatal(err)}
	bound,err=st.BoundSkills(ctx,classID,agent.ID);if err!=nil||len(bound)!=0{t.Fatalf("unbound skill routed: %+v %v",bound,err)}
	agent.SkillIDs=[]int64{skill.ID}
	if err:=st.SaveAgent(ctx,&agent);err!=nil{t.Fatal(err)}
	skill.Enabled=false
	if err:=st.SaveSkill(ctx,&skill);err!=nil{t.Fatal(err)}
	bound,err=st.BoundSkills(ctx,classID,agent.ID);if err!=nil||len(bound)!=0{t.Fatalf("disabled skill routed: %+v %v",bound,err)}
}
