package model

import (
	"encoding/json"
	"time"
)

type User struct {
	ID int64 `json:"id"`
	Username string `json:"username"`
	DisplayName string `json:"displayName"`
	Role string `json:"role"`
	ClassID int64 `json:"classId"`
	ClassName string `json:"className"`
	// ClassCode 是班级代号（A / B），供界面显示「A 班知识检索」一类的文案。
	ClassCode string `json:"classCode"`
	PasswordHash string `json:"-"`
}

type Material struct {
	ID int64 `json:"id"`
	ClassID int64 `json:"classId"`
	// ClassCode 是班级代号，随列表与详情一起返回，避免前端再猜班级名称。
	ClassCode string `json:"classCode"`
	UploaderID int64 `json:"uploaderId"`
	UploaderName string `json:"uploaderName"`
	Title string `json:"title"`
	OriginalName string `json:"originalName"`
	StoredName string `json:"-"`
	MediaType string `json:"mediaType"`
	SizeBytes int64 `json:"sizeBytes"`
	Content string `json:"content,omitempty"`
	// BodyText 与 Content 同源：knowledge_entries 中的上传原文。
	// 切分与预处理都不会改写它。
	BodyText string `json:"bodyText,omitempty"`
	ChunkCount int `json:"chunkCount"`
	ChunkStrategy string `json:"chunkStrategy"`
	CreatedAt time.Time `json:"createdAt"`
}

// Chunk 是检索的最小单位。正文保存在 MySQL，向量库只保留同主键的浮点向量；
// 偏移量是相对「预处理之后」的文本计算的，不是原文件中的下标。
type Chunk struct {
	ID int64 `json:"id"`
	MaterialID int64 `json:"materialId"`
	EntryID int64 `json:"knowledgeEntryId"`
	ClassID int64 `json:"classId"`
	ChunkIndex int `json:"chunkIndex"`
	ChunkText string `json:"chunkText"`
	StartOffset int `json:"startOffset"`
	EndOffset int `json:"endOffset"`
	IndexStatus string `json:"indexStatus"`
	ChunkStrategy string `json:"chunkStrategy"`
	CreatedAt time.Time `json:"createdAt"`
}

type TutorAgent struct {
	ID          int64     `json:"id"`
	ClassID     int64     `json:"class_id"`
	Name        string    `json:"name"`
	Prompt      string    `json:"prompt"`
	Enabled     bool      `json:"enabled"`
	SkillIDs    []int64   `json:"skill_ids"`
	MaterialIDs []int64   `json:"material_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Skill struct {
	ID          int64     `json:"id"`
	ClassID     int64     `json:"class_id"`
	Name        string    `json:"name"`
	TriggerText string    `json:"trigger_text"`
	SubjectText string    `json:"subject_text"`
	Description string    `json:"description"`
	Tools       []string  `json:"tools"`
	SourceText  string    `json:"source_text"`
	Enabled     bool      `json:"enabled"`
	SeedKey     string    `json:"seed_key,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Conversation struct {
	ID        int64     `json:"id"`
	ClassID   int64     `json:"class_id"`
	UserID    int64     `json:"user_id"`
	AgentID   int64     `json:"agent_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             int64           `json:"id"`
	ConversationID int64           `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Citations      json.RawMessage `json:"citations"`
	SkillName      string          `json:"skill_name,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}
