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
		`CREATE TABLE IF NOT EXISTS materials (id BIGINT PRIMARY KEY AUTO_INCREMENT, class_id BIGINT NOT NULL, uploader_id BIGINT NOT NULL, title VARCHAR(255) NOT NULL, original_name VARCHAR(255) NOT NULL, stored_name VARCHAR(255) NOT NULL UNIQUE, media_type VARCHAR(100) NOT NULL, size_bytes BIGINT NOT NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (class_id) REFERENCES classes(id), FOREIGN KEY (uploader_id) REFERENCES users(id), INDEX idx_materials_class_created (class_id, created_at)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS knowledge_entries (id BIGINT PRIMARY KEY AUTO_INCREMENT, material_id BIGINT NOT NULL UNIQUE, class_id BIGINT NOT NULL, content MEDIUMTEXT NOT NULL, created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE, FOREIGN KEY (class_id) REFERENCES classes(id), INDEX idx_knowledge_class (class_id)) ENGINE=InnoDB`,
		// 检索的最小单位。正文留在这里，向量库只有同主键的浮点向量。
		// ngram parser 让 MySQL 能对中文按 2 字 token 建立全文索引。
		`CREATE TABLE IF NOT EXISTS knowledge_chunks (id BIGINT PRIMARY KEY AUTO_INCREMENT, material_id BIGINT NOT NULL, knowledge_entry_id BIGINT NOT NULL, class_id BIGINT NOT NULL, chunk_index INT NOT NULL, chunk_text MEDIUMTEXT NOT NULL, start_offset INT NOT NULL, end_offset INT NOT NULL, index_status ENUM('pending','ready','failed') NOT NULL DEFAULT 'pending', chunk_strategy VARCHAR(16) NOT NULL DEFAULT 'auto', created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE, FOREIGN KEY (knowledge_entry_id) REFERENCES knowledge_entries(id) ON DELETE CASCADE, FOREIGN KEY (class_id) REFERENCES classes(id), INDEX idx_chunks_class_status (class_id, index_status), INDEX idx_chunks_entry (knowledge_entry_id), FULLTEXT KEY ft_chunk_text (chunk_text) WITH PARSER ngram) ENGINE=InnoDB`,
	}
	for _, statement := range statements { if _, err := s.DB.ExecContext(ctx, statement); err != nil { return err } }
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
	if len(ids) == 0 { return []model.Chunk{}, nil }
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, classID)
	for _, id := range ids { args = append(args, id) }
	rows, err := s.DB.QueryContext(ctx, `SELECT `+chunkColumns+` FROM knowledge_chunks WHERE class_id=? AND id IN (`+placeholders+`)`, args...)
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
	if limit < 1 { limit = 10 }
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+chunkColumns+`, MATCH(chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS score
		 FROM knowledge_chunks
		 WHERE class_id=? AND index_status='ready' AND MATCH(chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE)
		 ORDER BY score DESC, id ASC LIMIT ?`, query, classID, query, limit)
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
