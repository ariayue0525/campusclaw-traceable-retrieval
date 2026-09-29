package config

import "testing"

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MYSQL_USER", "app")
	t.Setenv("MYSQL_PASSWORD", "db-pass")
	t.Setenv("MYSQL_DATABASE", "campusclaw")
	t.Setenv("SESSION_SECRET", "01234567890123456789012345678901")
	t.Setenv("SEED_TEACHER_PASSWORD", "teacher-pass")
	t.Setenv("SEED_STUDENT_A_PASSWORD", "student-a-pass")
	t.Setenv("SEED_STUDENT_B_PASSWORD", "student-b-pass")
}

func TestLoadFailsClosedWithoutSecret(t *testing.T) {
	validEnv(t)
	t.Setenv("SESSION_SECRET", "")
	if _, err := Load(); err == nil { t.Fatal("expected missing SESSION_SECRET to fail") }
}

func TestLoadRejectsShortSecret(t *testing.T) {
	validEnv(t)
	t.Setenv("SESSION_SECRET", "too-short")
	if _, err := Load(); err == nil { t.Fatal("expected short SESSION_SECRET to fail") }
}

// 阈值要匹配嵌入空间：本地哈希嵌入的余弦整体偏低，沿用网关的 0.35 会召回不到东西。
func TestVectorMinScoreFollowsEmbedder(t *testing.T) {
	validEnv(t)
	t.Setenv("EMBED_PROVIDER", "local")
	local, err := Load()
	if err != nil { t.Fatal(err) }
	if local.VectorMinScore > 0.2 { t.Fatalf("本地嵌入的默认阈值应更低，得到 %v", local.VectorMinScore) }

	t.Setenv("EMBED_PROVIDER", "gateway")
	gateway, err := Load()
	if err != nil { t.Fatal(err) }
	if gateway.VectorMinScore != 0.35 { t.Fatalf("网关嵌入默认阈值应为 0.35，得到 %v", gateway.VectorMinScore) }

	// 显式指定时以环境变量为准。
	t.Setenv("VECTOR_MIN_SCORE", "0.05")
	overridden, err := Load()
	if err != nil { t.Fatal(err) }
	if overridden.VectorMinScore != 0.05 { t.Fatalf("显式阈值应被采用，得到 %v", overridden.VectorMinScore) }
}

// 没有密钥时回退到本地嵌入，保证仓库可以一键启动。
func TestEmbedProviderDefaultsToLocalWithoutKey(t *testing.T) {
	validEnv(t)
	t.Setenv("EMBED_PROVIDER", "")
	t.Setenv("LLM_GATEWAY_API_KEY", "")
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if cfg.EmbedProvider != "local" { t.Fatalf("无密钥时应回退 local，得到 %q", cfg.EmbedProvider) }
	if cfg.EmbedDim != 256 { t.Fatalf("本地嵌入默认维度应为 256，得到 %d", cfg.EmbedDim) }
}

func TestLoadBuildsDatabaseDSN(t *testing.T) {
	validEnv(t)
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if cfg.DatabaseDSN != "app:db-pass@tcp(db:3306)/campusclaw?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci" { t.Fatalf("unexpected DSN %q", cfg.DatabaseDSN) }
}

