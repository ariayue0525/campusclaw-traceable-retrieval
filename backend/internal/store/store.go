package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"campusclaw/backend/internal/model"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("not found")

type Store struct { DB *sql.DB }

func Open(ctx context.Context, dsn string, attempts int) (*Store, error) {
	var last error
	for i := 0; i < attempts; i++ {
		db, err := sql.Open("mysql", dsn)
		if err == nil { err = db.PingContext(ctx) }
		if err == nil { db.SetMaxOpenConns(15); db.SetConnMaxLifetime(3*time.Minute); return &Store{DB: db}, nil }
		if db != nil { _ = db.Close() }
		last = err
		select { case <-ctx.Done(): return nil, ctx.Err(); case <-time.After(2 * time.Second): }
	}
	return nil, fmt.Errorf("database unavailable after retries: %w", last)
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS classes (id BIGINT PRIMARY KEY AUTO_INCREMENT, name VARCHAR(80) NOT NULL UNIQUE) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS users (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, username VARCHAR(80) NOT NULL UNIQUE, display_name VARCHAR(80) NOT NULL, role ENUM('teacher','student') NOT NULL, password_hash VARCHAR(255) NOT NULL, FOREIGN KEY (class_id) REFERENCES classes(id), INDEX idx_users_class (class_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS sessions (token_hash BINARY(32) PRIMARY KEY, user_id BIGINT NOT NULL, expires_at DATETIME(6) NOT NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE, INDEX idx_sessions_expiry (expires_at)) ENGINE=InnoDB`,
		// JWT Bearer 方案的 refresh token：库里只存随机串的 SHA-256 摘要。
		// 轮换在同一事务里删旧写新，重放旧串直接失配。
		`CREATE TABLE IF NOT EXISTS refresh_tokens (id BIGINT PRIMARY KEY AUTO_INCREMENT, token_hash BINARY(32) NOT NULL UNIQUE, user_id BIGINT NOT NULL, expires_at DATETIME(6) NOT NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE, INDEX idx_refresh_expiry (expires_at)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS materials (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, uploader_id BIGINT NOT NULL, title VARCHAR(255) NOT NULL, original_name VARCHAR(255) NOT NULL, stored_name VARCHAR(255) NOT NULL UNIQUE, media_type VARCHAR(100) NOT NULL, size_bytes BIGINT NOT NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (class_id) REFERENCES classes(id), FOREIGN KEY (uploader_id) REFERENCES users(id), INDEX idx_materials_class_created (class_id, created_at)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS knowledge_entries (id BIGINT PRIMARY KEY AUTO_INCREMENT, material_id BIGINT NOT NULL UNIQUE, class_id BIGINT NOT NULL, content MEDIUMTEXT NOT NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE, FOREIGN KEY (class_id) REFERENCES classes(id), INDEX idx_knowledge_class (class_id)) ENGINE=InnoDB`,
		// 检索的最小单位。正文留在这里，向量库只有同主键的浮点向量。
		// ngram parser 让 MySQL 能对中文按 2 字 token 建立全文索引。
		`CREATE TABLE IF NOT EXISTS knowledge_chunks (id BIGINT PRIMARY KEY AUTO_INCREMENT, material_id BIGINT NOT NULL, knowledge_entry_id BIGINT NOT NULL, class_id BIGINT NOT NULL, chunk_index INT NOT NULL, chunk_text MEDIUMTEXT NOT NULL, start_offset INT NOT NULL, end_offset INT NOT NULL, index_status ENUM('pending','ready','failed') NOT NULL DEFAULT 'pending', chunk_strategy VARCHAR(16) NOT NULL DEFAULT 'auto', created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE, FOREIGN KEY (knowledge_entry_id) REFERENCES knowledge_entries(id) ON DELETE CASCADE, FOREIGN KEY (class_id) REFERENCES classes(id), INDEX idx_chunks_class_status (class_id, index_status), INDEX idx_chunks_entry (knowledge_entry_id), FULLTEXT KEY ft_chunk_text (chunk_text) WITH PARSER ngram) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS tutor_agents (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, name VARCHAR(64) NOT NULL, prompt TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6), FOREIGN KEY (class_id) REFERENCES classes(id), UNIQUE KEY uq_agent_class_name (class_id,name), INDEX idx_agents_class_enabled (class_id,enabled)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS skills (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, name VARCHAR(32) NOT NULL, trigger_text VARCHAR(120) NOT NULL, subject_text VARCHAR(32) NOT NULL DEFAULT '', description TEXT NOT NULL, tools_json JSON NOT NULL, source_text TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, seed_key VARCHAR(64) NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6), FOREIGN KEY (class_id) REFERENCES classes(id), UNIQUE KEY uq_skill_class_name (class_id,name), UNIQUE KEY uq_skill_seed (seed_key), INDEX idx_skills_class_enabled (class_id,enabled)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS tutor_agent_skills (agent_id BIGINT NOT NULL, skill_id BIGINT NOT NULL, PRIMARY KEY(agent_id,skill_id), FOREIGN KEY(agent_id) REFERENCES tutor_agents(id) ON DELETE CASCADE, FOREIGN KEY(skill_id) REFERENCES skills(id) ON DELETE CASCADE) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS tutor_agent_materials (agent_id BIGINT NOT NULL, material_id BIGINT NOT NULL, PRIMARY KEY(agent_id,material_id), FOREIGN KEY(agent_id) REFERENCES tutor_agents(id) ON DELETE CASCADE, FOREIGN KEY(material_id) REFERENCES materials(id) ON DELETE CASCADE) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS conversations (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, user_id BIGINT NOT NULL, agent_id BIGINT NOT NULL, title VARCHAR(100) NOT NULL DEFAULT '新的提问', created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY(class_id) REFERENCES classes(id), FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE, FOREIGN KEY(agent_id) REFERENCES tutor_agents(id), INDEX idx_conversations_owner_updated (user_id,class_id,updated_at)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS messages (id BIGINT PRIMARY KEY AUTO_INCREMENT, conversation_id BIGINT NOT NULL, role ENUM('user','assistant') NOT NULL, content MEDIUMTEXT NOT NULL, citations_json JSON NOT NULL, skill_name VARCHAR(32) NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY(conversation_id) REFERENCES conversations(id) ON DELETE CASCADE, INDEX idx_messages_conversation (conversation_id,id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS prompt_templates (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, name VARCHAR(64) NOT NULL, content TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6), CONSTRAINT fk_template_class FOREIGN KEY (class_id) REFERENCES classes(id), UNIQUE KEY uq_template_class_name (class_id,name), UNIQUE KEY uq_template_id_class (id,class_id), INDEX idx_templates_class_enabled (class_id,enabled)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS mcp_servers (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, name VARCHAR(64) NOT NULL, base_url VARCHAR(512) NOT NULL, token_env_key VARCHAR(128) NOT NULL, connected BOOLEAN NOT NULL DEFAULT FALSE, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6), CONSTRAINT fk_mcp_server_class FOREIGN KEY (class_id) REFERENCES classes(id), UNIQUE KEY uq_mcp_server_class_name (class_id,name), INDEX idx_mcp_servers_class_connected (class_id,connected)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS mcp_tools (id BIGINT PRIMARY KEY AUTO_INCREMENT, server_id BIGINT NOT NULL, name VARCHAR(128) NOT NULL, description TEXT NOT NULL, input_schema JSON NOT NULL, discovered_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), CONSTRAINT fk_mcp_tool_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE, UNIQUE KEY uq_mcp_tool_server_name (server_id,name)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS mcp_tool_calls (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, user_id BIGINT NOT NULL, conversation_id BIGINT NULL, server_id BIGINT NOT NULL, tool_name VARCHAR(128) NOT NULL, status ENUM('success','refused','error') NOT NULL, detail VARCHAR(500) NOT NULL DEFAULT '', created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), CONSTRAINT fk_mcp_call_class FOREIGN KEY (class_id) REFERENCES classes(id), CONSTRAINT fk_mcp_call_user FOREIGN KEY (user_id) REFERENCES users(id), CONSTRAINT fk_mcp_call_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL, CONSTRAINT fk_mcp_call_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id), INDEX idx_mcp_calls_class_created (class_id,created_at)) ENGINE=InnoDB`,
	}
	for _, statement := range statements { if _, err := s.DB.ExecContext(ctx, statement); err != nil { return err } }
	// MySQL 8.4 does not support ADD COLUMN IF NOT EXISTS. Inspect the catalogue
	// before altering older installations; never rewrite material/chunk tables.
	var templateColumnCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tutor_agents' AND column_name='prompt_template_id'`).Scan(&templateColumnCount); err != nil { return err }
	if templateColumnCount == 0 {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE tutor_agents ADD COLUMN prompt_template_id BIGINT NULL, ADD CONSTRAINT fk_agent_template_class FOREIGN KEY (prompt_template_id,class_id) REFERENCES prompt_templates(id,class_id)`); err != nil { return err }
	}
	return nil
}

func (s *Store) Seed(ctx context.Context, teacherPass, studentAPass, studentBPass string) error {
	tx, err := s.DB.BeginTx(ctx, nil); if err != nil { return err }
	defer tx.Rollback()
	for _, name := range []string{"A班", "B班"} { if _, err = tx.ExecContext(ctx, `INSERT INTO classes(name) VALUES(?) ON DUPLICATE KEY UPDATE name=VALUES(name)`, name); err != nil { return err } }
	var aID, bID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM classes WHERE name='A班'`).Scan(&aID); err != nil { return err }
	if err = tx.QueryRowContext(ctx, `SELECT id FROM classes WHERE name='B班'`).Scan(&bID); err != nil { return err }
	users := []struct{ classID int64; username, display, role, password string }{{aID,"teacher-a","王老师","teacher",teacherPass},{aID,"student-a1","李同学","student",studentAPass},{bID,"student-b1","张同学","student",studentBPass}}
	for _, u := range users {
		hash, e := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost); if e != nil { return e }
		_, err = tx.ExecContext(ctx, `INSERT INTO users(class_id,username,display_name,role,password_hash) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), role=VALUES(role)`, u.classID,u.username,u.display,u.role,string(hash)); if err != nil { return err }
	}
	var teacherID, studentBID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username='teacher-a'`).Scan(&teacherID); err != nil { return err }
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username='student-b1'`).Scan(&studentBID); err != nil { return err }
	seeds := []struct{ classID, userID int64; title, name, stored, content string }{{aID,teacherID,"A班班级公约与科学角手册","a-class-handbook.md","seed-a.md",SeedAClass},{bID,studentBID,"B班数学复习提纲","b-class-review.txt","seed-b.txt",SeedBClass}}
	for _, m := range seeds {
		res, e := tx.ExecContext(ctx, `INSERT IGNORE INTO materials(class_id,uploader_id,title,original_name,stored_name,media_type,size_bytes) VALUES(?,?,?,?,?,?,?)`,m.classID,m.userID,m.title,m.name,m.stored,mediaType(m.name),len([]byte(m.content))); if e != nil { return e }
		id, _ := res.LastInsertId(); if id == 0 { if e = tx.QueryRowContext(ctx, `SELECT id FROM materials WHERE stored_name=?`,m.stored).Scan(&id); e != nil { return e } }
		if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO knowledge_entries(material_id,class_id,content) VALUES(?,?,?)`,id,m.classID,m.content); e != nil { return e }
	}
	lessonSkill := "名称：数学解题引导\n适用条件：方程,去括号,分数,通分,面积,应用题,未知数,行程\n资料：数学\n工具：knowledge_search\n启用：是\n\n先看懂学生卡在哪一步，只指出下一步该检查什么。不要给出最终答案、选项、得数或完整解题过程；用问题引导学生自己完成。"
	for _, classID := range []int64{aID, bID} {
		seedKey := fmt.Sprintf("lesson5-guidance-%d", classID)
		if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO prompt_templates(class_id,name,content,enabled) VALUES(?,'启发式导师模板','你是{{class_name}}的导师。请用问题引导{{student_name}}独立思考，不直接给出最终答案。',TRUE)`, classID); err != nil { return err }
		if _, err = tx.ExecContext(ctx, `INSERT INTO skills(class_id,name,trigger_text,subject_text,description,tools_json,source_text,enabled,seed_key) VALUES(?,'数学解题引导','方程,去括号,分数,通分,面积,应用题,未知数,行程','数学','先看懂学生卡在哪一步，只指出下一步该检查什么。不要给出最终答案、选项、得数或完整解题过程；用问题引导学生自己完成。',JSON_ARRAY('knowledge_search'),?,TRUE,?) ON DUPLICATE KEY UPDATE seed_key=VALUES(seed_key)`, classID, lessonSkill, seedKey); err != nil { return err }
		if _, err = tx.ExecContext(ctx, `INSERT INTO tutor_agents(class_id,name,prompt,enabled) VALUES(?,'启发式解题助手','你是一名耐心的教学助手，优先用提问帮助学生自己发现思路。',TRUE) ON DUPLICATE KEY UPDATE name=VALUES(name)`, classID); err != nil { return err }
		if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO tutor_agent_skills(agent_id,skill_id) SELECT a.id,s.id FROM tutor_agents a JOIN skills s ON s.class_id=a.class_id WHERE a.class_id=? AND a.name='启发式解题助手' AND s.seed_key=?`, classID, seedKey); err != nil { return err }
		if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO tutor_agent_materials(agent_id,material_id) SELECT a.id,m.id FROM tutor_agents a JOIN materials m ON m.class_id=a.class_id WHERE a.class_id=? AND a.name='启发式解题助手'`, classID); err != nil { return err }
		// 第二种内置应用强调概念解释，和解题引导有不同角色设定。
		// 仅在首次创建时绑定现有材料；后续重启不覆盖教师对它的启停、提示词或材料绑定。
		created, e := tx.ExecContext(ctx, `INSERT IGNORE INTO tutor_agents(class_id,name,prompt,enabled) VALUES(?,'概念讲解助手','你是清晰、耐心的概念讲解助手。先解释关键词与核心原理，再用本班资料中的例子帮助学生理解；资料没有依据时不要编造引用。',TRUE)`, classID)
		if e != nil { return e }
		if n, e := created.RowsAffected(); e != nil { return e } else if n > 0 {
			if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO tutor_agent_materials(agent_id,material_id) SELECT a.id,m.id FROM tutor_agents a JOIN materials m ON m.class_id=a.class_id WHERE a.class_id=? AND a.name='概念讲解助手'`, classID); e != nil { return e }
		}
	}
	return tx.Commit()
}

func (s *Store) UserByUsername(ctx context.Context, username string) (model.User, error) {
	var u model.User
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.username,u.display_name,u.role,u.class_id,c.name,TRIM(TRAILING '班' FROM c.name),u.password_hash FROM users u JOIN classes c ON c.id=u.class_id WHERE u.username=?`, username).Scan(&u.ID,&u.Username,&u.DisplayName,&u.Role,&u.ClassID,&u.ClassName,&u.ClassCode,&u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) { return u, ErrNotFound }; return u, err
}

func tokenDigest(token string) [32]byte { return sha256.Sum256([]byte(token)) }

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiry time.Time) error { d:=tokenDigest(token); _,err:=s.DB.ExecContext(ctx,`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`,d[:],userID,expiry); return err }
func (s *Store) DeleteSession(ctx context.Context, token string) error { d:=tokenDigest(token); _,err:=s.DB.ExecContext(ctx,`DELETE FROM sessions WHERE token_hash=?`,d[:]); return err }
func (s *Store) UserBySession(ctx context.Context, token string) (model.User,error) { var u model.User; d:=tokenDigest(token); err:=s.DB.QueryRowContext(ctx,`SELECT u.id,u.username,u.display_name,u.role,u.class_id,c.name,TRIM(TRAILING '班' FROM c.name) FROM sessions s JOIN users u ON u.id=s.user_id JOIN classes c ON c.id=u.class_id WHERE s.token_hash=? AND s.expires_at>NOW(6)`,d[:]).Scan(&u.ID,&u.Username,&u.DisplayName,&u.Role,&u.ClassID,&u.ClassName,&u.ClassCode); if errors.Is(err,sql.ErrNoRows){return u,ErrNotFound}; return u,err }

// ---- JWT Bearer：refresh token 与按 ID 取实时身份 ----

// CreateRefreshToken 落库随机串的 SHA-256 摘要。明文只出现在登录响应里。
func (s *Store) CreateRefreshToken(ctx context.Context, token string, userID int64, expiry time.Time) error {
	d := tokenDigest(token)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO refresh_tokens(token_hash,user_id,expires_at) VALUES(?,?,?)`, d[:], userID, expiry)
	return err
}

// RotateRefreshToken 在同一事务里删除旧摘要、写入新摘要（轮换）。
// 旧串重放时查不到记录，直接 401。
func (s *Store) RotateRefreshToken(ctx context.Context, oldToken, newToken string, userID int64, expiry time.Time) error {
	old, new_ := tokenDigest(oldToken), tokenDigest(newToken)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token_hash=? AND user_id=? AND expires_at>NOW(6)`, old[:], userID)
	if err != nil { return err }
	if n, err := res.RowsAffected(); err != nil { return err } else if n != 1 { return ErrNotFound }
	if _, err := tx.ExecContext(ctx, `INSERT INTO refresh_tokens(token_hash,user_id,expires_at) VALUES(?,?,?)`, new_[:], userID, expiry); err != nil { return err }
	return tx.Commit()
}

// DeleteRefreshToken 吊销 refresh token（登出）。
func (s *Store) DeleteRefreshToken(ctx context.Context, token string) error {
	d := tokenDigest(token)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token_hash=?`, d[:])
	return err
}

// UserByRefreshToken 用摘要换取实时用户身份；过期或不存在返回 ErrNotFound。
func (s *Store) UserByRefreshToken(ctx context.Context, token string) (model.User, error) {
	var u model.User
	d := tokenDigest(token)
	err := s.DB.QueryRowContext(ctx,
		`SELECT u.id,u.username,u.display_name,u.role,u.class_id,c.name,TRIM(TRAILING '班' FROM c.name)
		 FROM refresh_tokens r JOIN users u ON u.id=r.user_id JOIN classes c ON c.id=u.class_id
		 WHERE r.token_hash=? AND r.expires_at>NOW(6)`, d[:]).
		Scan(&u.ID,&u.Username,&u.DisplayName,&u.Role,&u.ClassID,&u.ClassName,&u.ClassCode)
	if errors.Is(err, sql.ErrNoRows) { return u, ErrNotFound }
	return u, err
}

// UserByID 按主键读取实时身份。JWT 校验通过后调用：授权以数据库当前值为准，
// 不信任令牌签发时刻写入 payload 的角色与班级。
func (s *Store) UserByID(ctx context.Context, id int64) (model.User, error) {
	var u model.User
	err := s.DB.QueryRowContext(ctx,
		`SELECT u.id,u.username,u.display_name,u.role,u.class_id,c.name,TRIM(TRAILING '班' FROM c.name)
		 FROM users u JOIN classes c ON c.id=u.class_id WHERE u.id=?`, id).
		Scan(&u.ID,&u.Username,&u.DisplayName,&u.Role,&u.ClassID,&u.ClassName,&u.ClassCode)
	if errors.Is(err, sql.ErrNoRows) { return u, ErrNotFound }
	return u, err
}

const materialColumns = `m.id,m.class_id,TRIM(TRAILING '班' FROM c.name),m.uploader_id,u.display_name,m.title,m.original_name,m.stored_name,m.media_type,m.size_bytes,k.content,m.created_at,` +
	`(SELECT COUNT(*) FROM knowledge_chunks x WHERE x.material_id=m.id),` +
	`(SELECT COALESCE(MIN(x.chunk_strategy),'') FROM knowledge_chunks x WHERE x.material_id=m.id)`
const materialJoin = ` FROM materials m JOIN users u ON u.id=m.uploader_id JOIN classes c ON c.id=m.class_id JOIN knowledge_entries k ON k.material_id=m.id`

func scanMaterial(scan func(dest ...any) error) (model.Material, error) {
	var m model.Material
	err := scan(&m.ID,&m.ClassID,&m.ClassCode,&m.UploaderID,&m.UploaderName,&m.Title,&m.OriginalName,&m.StoredName,&m.MediaType,&m.SizeBytes,&m.Content,&m.CreatedAt,&m.ChunkCount,&m.ChunkStrategy)
	if err != nil { return m, err }
	m.BodyText = m.Content
	return m, nil
}

func (s *Store) ListMaterials(ctx context.Context,classID int64,q string)([]model.Material,error){
	rows,err:=s.DB.QueryContext(ctx,`SELECT `+materialColumns+materialJoin+` WHERE m.class_id=? AND (?='' OR m.title LIKE CONCAT('%',?,'%') OR m.original_name LIKE CONCAT('%',?,'%')) ORDER BY m.created_at DESC,m.id DESC`,classID,q,q,q)
	if err!=nil{return nil,err}; defer rows.Close(); result:=[]model.Material{}
	for rows.Next(){ m, err := scanMaterial(rows.Scan); if err!=nil{return nil,err}; result=append(result,m) }
	return result,rows.Err()
}

func (s *Store) MaterialByID(ctx context.Context,id int64)(model.Material,error){
	m,err:=scanMaterial(s.DB.QueryRowContext(ctx,`SELECT `+materialColumns+materialJoin+` WHERE m.id=?`,id).Scan)
	if errors.Is(err,sql.ErrNoRows){return model.Material{},ErrNotFound}; return m,err
}

func (s *Store) CreateMaterial(ctx context.Context,m *model.Material,content string,beforeCommit func() error) error { tx,err:=s.DB.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback();res,err:=tx.ExecContext(ctx,`INSERT INTO materials(class_id,uploader_id,title,original_name,stored_name,media_type,size_bytes) VALUES(?,?,?,?,?,?,?)`,m.ClassID,m.UploaderID,m.Title,m.OriginalName,m.StoredName,m.MediaType,m.SizeBytes);if err!=nil{return err};m.ID,err=res.LastInsertId();if err!=nil{return err};if _,err=tx.ExecContext(ctx,`INSERT INTO knowledge_entries(material_id,class_id,content) VALUES(?,?,?)`,m.ID,m.ClassID,content);err!=nil{return err};if beforeCommit!=nil{if err=beforeCommit();err!=nil{return err}};if err=tx.Commit();err!=nil{return err};return s.DB.QueryRowContext(ctx,`SELECT created_at FROM materials WHERE id=?`,m.ID).Scan(&m.CreatedAt)}

func mediaType(name string)string{if strings.HasSuffix(strings.ToLower(name),".md"){return "text/markdown; charset=utf-8"};return "text/plain; charset=utf-8"}

// ---- 切片（knowledge_chunks）----

// EntryRef 定位一份材料的正文条目。
type EntryRef struct {
	MaterialID int64
	EntryID    int64
	ClassID    int64
	Title      string
}

// EntryForMaterial 读取材料的正文条目；跨班调用返回 ErrNotFound。
func (s *Store) EntryForMaterial(ctx context.Context, materialID, classID int64) (EntryRef, string, error) {
	var ref EntryRef
	var body string
	err := s.DB.QueryRowContext(ctx,
		`SELECT k.id, m.id, m.class_id, m.title, k.content FROM materials m JOIN knowledge_entries k ON k.material_id=m.id WHERE m.id=? AND m.class_id=?`,
		materialID, classID).Scan(&ref.EntryID, &ref.MaterialID, &ref.ClassID, &ref.Title, &body)
	if errors.Is(err, sql.ErrNoRows) { return ref, "", ErrNotFound }
	return ref, body, err
}

// EntriesMissingChunks 返回还没有任何切片的正文章，用于启动时按自动窗口补齐索引。
func (s *Store) EntriesMissingChunks(ctx context.Context) ([]EntryRef, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT k.id, m.id, m.class_id, m.title FROM knowledge_entries k JOIN materials m ON m.id=k.material_id WHERE NOT EXISTS (SELECT 1 FROM knowledge_chunks c WHERE c.knowledge_entry_id=k.id) ORDER BY k.id`)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []EntryRef{}
	for rows.Next() {
		var ref EntryRef
		if err := rows.Scan(&ref.EntryID, &ref.MaterialID, &ref.ClassID, &ref.Title); err != nil { return nil, err }
		out = append(out, ref)
	}
	return out, rows.Err()
}

// EntryBody 读取正文原文。
func (s *Store) EntryBody(ctx context.Context, entryID int64) (string, error) {
	var body string
	err := s.DB.QueryRowContext(ctx, `SELECT content FROM knowledge_entries WHERE id=?`, entryID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) { return "", ErrNotFound }
	return body, err
}

// NewChunk 是待写入的切片。
type NewChunk struct {
	Text  string
	Start int
	End   int
}

// ReplaceChunks 先删除该条目的旧切片，再写入新切片（状态为 pending）。
// 返回被删除的旧主键（供调用方同步删除向量）与新写入的主键。
func (s *Store) ReplaceChunks(ctx context.Context, ref EntryRef, strategy string, chunks []NewChunk) (oldIDs, newIDs []int64, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return nil, nil, err }
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM knowledge_chunks WHERE knowledge_entry_id=?`, ref.EntryID)
	if err != nil { return nil, nil, err }
	oldIDs = []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil { rows.Close(); return nil, nil, err }
		oldIDs = append(oldIDs, id)
	}
	if err := rows.Err(); err != nil { return nil, nil, err }
	rows.Close()
	if len(oldIDs) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunks WHERE knowledge_entry_id=?`, ref.EntryID); err != nil { return nil, nil, err }
	}
	newIDs = []int64{}
	for i, c := range chunks {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO knowledge_chunks(material_id,knowledge_entry_id,class_id,chunk_index,chunk_text,start_offset,end_offset,index_status,chunk_strategy) VALUES(?,?,?,?,?,?,?,'pending',?)`,
			ref.MaterialID, ref.EntryID, ref.ClassID, i, c.Text, c.Start, c.End, strategy)
		if err != nil { return nil, nil, err }
		id, err := res.LastInsertId()
		if err != nil { return nil, nil, err }
		newIDs = append(newIDs, id)
	}
	if err := tx.Commit(); err != nil { return nil, nil, err }
	return oldIDs, newIDs, nil
}

// SetChunkStatus 批量更新切片状态：pending → ready / failed。
func (s *Store) SetChunkStatus(ctx context.Context, ids []int64, status string) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, status)
	for _, id := range ids { args = append(args, id) }
	_, err := s.DB.ExecContext(ctx, `UPDATE knowledge_chunks SET index_status=? WHERE id IN (`+placeholders+`)`, args...)
	return err
}

// MarkEntryFailed 把某条目下的全部切片标记为 failed（嵌入整体失败时使用）。
func (s *Store) MarkEntryFailed(ctx context.Context, entryID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE knowledge_chunks SET index_status='failed' WHERE knowledge_entry_id=? AND index_status<>'ready'`, entryID)
	return err
}

func scanChunk(scan func(dest ...any) error) (model.Chunk, error) {
	var c model.Chunk
	err := scan(&c.ID, &c.MaterialID, &c.EntryID, &c.ClassID, &c.ChunkIndex, &c.ChunkText, &c.StartOffset, &c.EndOffset, &c.IndexStatus, &c.ChunkStrategy, &c.CreatedAt)
	return c, err
}

const chunkColumns = `id,material_id,knowledge_entry_id,class_id,chunk_index,chunk_text,start_offset,end_offset,index_status,chunk_strategy,created_at`

func chunkSelect(rows *sql.Rows) ([]model.Chunk, error) {
	defer rows.Close()
	out := []model.Chunk{}
	for rows.Next() {
		c, err := scanChunk(rows.Scan)
		if err != nil { return nil, err }
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChunksByIDs 按主键回表取回切片正文；班级条件在这里再次核对，避免只靠向量库过滤。
func (s *Store) ChunksByIDs(ctx context.Context, classID int64, ids []int64) ([]model.Chunk, error) {
	return s.ChunksByIDsScoped(ctx,classID,nil,ids)
}

// ChunksByIDsScoped is the final authorization check for agent retrieval. A
// non-nil material set also requires ready chunks; an empty set returns none.
func (s *Store) ChunksByIDsScoped(ctx context.Context,classID int64,materialIDs,ids []int64)([]model.Chunk,error){
	if len(ids) == 0 { return []model.Chunk{}, nil }
	if materialIDs!=nil&&len(materialIDs)==0{return []model.Chunk{},nil}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, classID)
	for _, id := range ids { args = append(args, id) }
	query:=`SELECT `+chunkColumns+` FROM knowledge_chunks WHERE class_id=? AND id IN (`+placeholders+`)`
	if materialIDs!=nil{
		query+=` AND index_status='ready' AND material_id IN (`+strings.TrimRight(strings.Repeat("?,",len(materialIDs)),",")+`)`
		for _,id:=range materialIDs{args=append(args,id)}
	}
	rows, err := s.DB.QueryContext(ctx,query,args...)
	if err != nil { return nil, err }
	return chunkSelect(rows)
}

// ListChunks 列出某材料的全部切片（含非 ready，供页面对照两库）。
func (s *Store) ListChunks(ctx context.Context, classID, materialID int64) ([]model.Chunk, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+chunkColumns+` FROM knowledge_chunks WHERE class_id=? AND material_id=? ORDER BY chunk_index,id`, classID, materialID)
	if err != nil { return nil, err }
	return chunkSelect(rows)
}

// KeywordRow 是关键字路径的一条候选，Score 为 MySQL 全文相关度。
type KeywordRow struct {
	Chunk model.Chunk
	Score float64
}

// KeywordSearch 只查 MySQL 全文索引：限定本班且仅检索已就绪切片。
// 它不调用嵌入服务，也不访问 Qdrant。
func (s *Store) KeywordSearch(ctx context.Context, classID int64, query string, limit int) ([]KeywordRow, error) {
	return s.KeywordSearchScoped(ctx,classID,nil,query,limit)
}

func (s *Store) KeywordSearchScoped(ctx context.Context,classID int64,materialIDs []int64,query string,limit int)([]KeywordRow,error){
	if limit < 1 { limit = 10 }
	if materialIDs!=nil&&len(materialIDs)==0{return []KeywordRow{},nil}
	statement:=`SELECT `+chunkColumns+`, MATCH(chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS score FROM knowledge_chunks WHERE class_id=? AND index_status='ready' AND MATCH(chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE)`
	args:=[]any{query,classID,query}
	if materialIDs!=nil{
		statement+=` AND material_id IN (`+strings.TrimRight(strings.Repeat("?,",len(materialIDs)),",")+`)`
		for _,id:=range materialIDs{args=append(args,id)}
	}
	statement+=` ORDER BY score DESC,id ASC LIMIT ?`
	args=append(args,limit)
	rows, err := s.DB.QueryContext(ctx,statement,args...)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []KeywordRow{}
	for rows.Next() {
		var score float64
		c, err := scanChunk(func(dest ...any) error {
			dest = append(dest, &score)
			return rows.Scan(dest...)
		})
		if err != nil { return nil, err }
		out = append(out, KeywordRow{Chunk: c, Score: score})
	}
	return out, rows.Err()
}

// TitlesByMaterialIDs 批量取回本班材料标题，用于命中结果显示「出自哪一份材料」。
func (s *Store) TitlesByMaterialIDs(ctx context.Context, classID int64, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 { return out, nil }
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, classID)
	for _, id := range ids { args = append(args, id) }
	rows, err := s.DB.QueryContext(ctx, `SELECT id,title FROM materials WHERE class_id=? AND id IN (`+placeholders+`)`, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil { return nil, err }
		out[id] = title
	}
	return out, rows.Err()
}

// CountReadyChunks 统计本班已就绪切片数。
func (s *Store) CountReadyChunks(ctx context.Context, classID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_chunks WHERE class_id=? AND index_status='ready'`, classID).Scan(&n)
	return n, err
}
