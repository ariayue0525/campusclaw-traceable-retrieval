package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 保存进程运行所需的全部设置。检索相关配置项集中在下半部分。
// 网关密钥只存在于进程内存中，任何 HTTP 响应都不会包含它们。
type Config struct {
	Addr        string
	DatabaseDSN string
	// SessionSecret 复用为 JWT 签名密钥（HS256）。名称保留 SESSION_SECRET，
	// 避免部署环境里多一套要轮换的密钥。
	SessionSecret string
	// TokenTTL 是 access token（JWT）有效期；RefreshTTL 是 refresh token 有效期。
	TokenTTL   time.Duration
	RefreshTTL time.Duration
	UploadDir        string
	UploadMaxBytes   int64
	LoginMaxFailures int
	LoginWindow      time.Duration
	SeedTeacherPass  string
	SeedStudentAPass string
	SeedStudentBPass string

	// 向量库
	QdrantURL        string
	QdrantCollection string

	// 嵌入与对话网关
	EmbedProvider  string // gateway | local
	GatewayBaseURL string
	GatewayAPIKey  string
	EmbeddingModel string
	ChatModel      string
	EmbedDim       int
	GatewayTimeout time.Duration

	// 检索编排
	VectorMinScore    float64
	VectorCandidates  int
	RRFK              float64
	SearchLimit       int
	SearchMaxLimit    int
	AskTopK           int
	SnippetMaxChars   int
	VectorPreviewDims int
	IndexMaxChunks    int
}

func Load() (Config, error) {
	required := []string{"MYSQL_USER", "MYSQL_PASSWORD", "MYSQL_DATABASE", "SESSION_SECRET", "SEED_TEACHER_PASSWORD", "SEED_STUDENT_A_PASSWORD", "SEED_STUDENT_B_PASSWORD"}
	for _, key := range required {
		if os.Getenv(key) == "" {
			return Config{}, fmt.Errorf("required environment variable %s is missing", key)
		}
	}
	if len(os.Getenv("SESSION_SECRET")) < 32 { return Config{}, fmt.Errorf("SESSION_SECRET must contain at least 32 characters") }
	host := value("MYSQL_HOST", "db")
	port := value("MYSQL_PORT", "3306")
	tokenTTL, err := time.ParseDuration(value("TOKEN_TTL", "30m"))
	if err != nil || tokenTTL < time.Minute { return Config{}, fmt.Errorf("TOKEN_TTL must be a duration of at least 1m") }
	refreshTTL, err := time.ParseDuration(value("REFRESH_TTL", "168h"))
	if err != nil || refreshTTL < tokenTTL { return Config{}, fmt.Errorf("REFRESH_TTL must be a duration no shorter than TOKEN_TTL") }
	window, err := time.ParseDuration(value("LOGIN_WINDOW", "1m"))
	if err != nil { return Config{}, fmt.Errorf("LOGIN_WINDOW: %w", err) }
	maxBytes, err := strconv.ParseInt(value("UPLOAD_MAX_BYTES", "2097152"), 10, 64)
	if err != nil || maxBytes < 1 { return Config{}, fmt.Errorf("UPLOAD_MAX_BYTES must be positive") }
	maxFailures, err := strconv.Atoi(value("LOGIN_MAX_FAILURES", "5"))
	if err != nil || maxFailures < 1 { return Config{}, fmt.Errorf("LOGIN_MAX_FAILURES must be positive") }

	// 未显式指定时：配置了网关密钥走网关，否则回退到内置的确定性本地嵌入器，
	// 保证仓库在没有任何密钥的情况下也能一键启动。
	provider := strings.ToLower(strings.TrimSpace(value("EMBED_PROVIDER", "")))
	apiKey := os.Getenv("LLM_GATEWAY_API_KEY")
	if provider == "" {
		if strings.TrimSpace(apiKey) != "" { provider = "gateway" } else { provider = "local" }
	}
	if provider != "gateway" && provider != "local" {
		return Config{}, fmt.Errorf("EMBED_PROVIDER must be either 'gateway' or 'local'")
	}
	defaultDim := 256
	if provider == "gateway" { defaultDim = 1536 }
	embedDim, err := strconv.Atoi(value("EMBED_DIM", strconv.Itoa(defaultDim)))
	if err != nil || embedDim < 8 || embedDim > 4096 { return Config{}, fmt.Errorf("EMBED_DIM must be between 8 and 4096") }
	gatewayTimeout, err := time.ParseDuration(value("LLM_GATEWAY_TIMEOUT", "20s"))
	if err != nil { return Config{}, fmt.Errorf("LLM_GATEWAY_TIMEOUT: %w", err) }

	// 阈值必须匹配嵌入空间：网关的语义嵌入余弦值普遍落在 0.4 以上，
	// 而本地哈希嵌入衡量的是字面接近程度，短问句对长切片天然只有 0.2 上下。
	// 两者共用同一个 0.35 会让本地模式几乎召回不到东西，因此按实现给不同默认值。
	defaultMinScore := "0.35"
	if provider == "local" { defaultMinScore = "0.15" }
	minScore, err := strconv.ParseFloat(value("VECTOR_MIN_SCORE", defaultMinScore), 64)
	if err != nil || minScore < 0 || minScore > 1 { return Config{}, fmt.Errorf("VECTOR_MIN_SCORE must be between 0 and 1") }
	rrfK, err := strconv.ParseFloat(value("RRF_K", "60"), 64)
	if err != nil || rrfK <= 0 { return Config{}, fmt.Errorf("RRF_K must be positive") }
	candidates, err := strconv.Atoi(value("VECTOR_CANDIDATES", "50"))
	if err != nil || candidates < 1 { return Config{}, fmt.Errorf("VECTOR_CANDIDATES must be positive") }
	searchLimit, err := strconv.Atoi(value("SEARCH_LIMIT", "10"))
	if err != nil || searchLimit < 1 { return Config{}, fmt.Errorf("SEARCH_LIMIT must be positive") }
	searchMax, err := strconv.Atoi(value("SEARCH_MAX_LIMIT", "50"))
	if err != nil || searchMax < searchLimit { return Config{}, fmt.Errorf("SEARCH_MAX_LIMIT must be >= SEARCH_LIMIT") }
	askTopK, err := strconv.Atoi(value("ASK_TOP_K", "4"))
	if err != nil || askTopK < 1 { return Config{}, fmt.Errorf("ASK_TOP_K must be positive") }
	snippet, err := strconv.Atoi(value("SNIPPET_MAX_CHARS", "240"))
	if err != nil || snippet < 20 { return Config{}, fmt.Errorf("SNIPPET_MAX_CHARS must be >= 20") }
	preview, err := strconv.Atoi(value("VECTOR_PREVIEW_DIMS", "8"))
	if err != nil || preview < 1 || preview > embedDim { return Config{}, fmt.Errorf("VECTOR_PREVIEW_DIMS must be between 1 and EMBED_DIM") }
	indexMax, err := strconv.Atoi(value("INDEX_MAX_CHUNKS", "400"))
	if err != nil || indexMax < 1 { return Config{}, fmt.Errorf("INDEX_MAX_CHUNKS must be positive") }

	return Config{
		Addr: value("API_ADDR", ":8081"),
		DatabaseDSN: fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci", os.Getenv("MYSQL_USER"), os.Getenv("MYSQL_PASSWORD"), host, port, os.Getenv("MYSQL_DATABASE")),
		SessionSecret: os.Getenv("SESSION_SECRET"), TokenTTL: tokenTTL, RefreshTTL: refreshTTL,
		UploadDir: value("UPLOAD_DIR", "/data/uploads"), UploadMaxBytes: maxBytes,
		LoginMaxFailures: maxFailures, LoginWindow: window,
		SeedTeacherPass: os.Getenv("SEED_TEACHER_PASSWORD"), SeedStudentAPass: os.Getenv("SEED_STUDENT_A_PASSWORD"), SeedStudentBPass: os.Getenv("SEED_STUDENT_B_PASSWORD"),

		QdrantURL:        strings.TrimRight(value("QDRANT_URL", "http://qdrant:6333"), "/"),
		QdrantCollection: value("QDRANT_COLLECTION", "campusclaw_chunks"),
		EmbedProvider:    provider,
		GatewayBaseURL:   strings.TrimRight(value("LLM_GATEWAY_BASE_URL", "https://ai-gateway.devops.hello1023.com/v1"), "/"),
		GatewayAPIKey:    apiKey,
		EmbeddingModel:   value("EMBEDDING_MODEL", "text-embedding-3-small"),
		ChatModel:        value("CHAT_MODEL", "gpt-4o-mini"),
		EmbedDim:         embedDim,
		GatewayTimeout:   gatewayTimeout,

		VectorMinScore:    minScore,
		VectorCandidates:  candidates,
		RRFK:              rrfK,
		SearchLimit:       searchLimit,
		SearchMaxLimit:    searchMax,
		AskTopK:           askTopK,
		SnippetMaxChars:   snippet,
		VectorPreviewDims: preview,
		IndexMaxChunks:    indexMax,
	}, nil
}

func value(key, fallback string) string { if v := os.Getenv(key); v != "" { return v }; return fallback }
