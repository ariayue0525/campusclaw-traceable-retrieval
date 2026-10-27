package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"campusclaw/backend/internal/chunk"
	"campusclaw/backend/internal/config"
	"campusclaw/backend/internal/embed"
	"campusclaw/backend/internal/llm"
	"campusclaw/backend/internal/retrieval"
	"campusclaw/backend/internal/server"
	"campusclaw/backend/internal/store"
	"campusclaw/backend/internal/vector"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabaseDSN, 45)
	if err != nil {
		log.Fatal(err)
	}
	defer db.DB.Close()
	if err = db.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	if err = db.Seed(ctx, cfg.SeedTeacherPass, cfg.SeedStudentAPass, cfg.SeedStudentBPass); err != nil {
		log.Fatal(err)
	}
	if err = os.MkdirAll(cfg.UploadDir, 0750); err != nil {
		log.Fatal(err)
	}
	seedFiles := map[string]string{"seed-a.md": "# A班语文教研计划\n\n本材料仅供 A 班师生查看。", "seed-b.txt": "B班数学复习提纲\n本材料仅供 B 班师生查看。"}
	for name, content := range seedFiles {
		path := cfg.UploadDir + string(os.PathSeparator) + name
		if _, e := os.Stat(path); os.IsNotExist(e) {
			if e = os.WriteFile(path, []byte(content), 0640); e != nil {
				log.Fatal(e)
			}
		}
	}

	// 嵌入模块：配置了网关密钥走课程网关，否则用内置的确定性本地嵌入器。
	var embedder embed.Embedder
	if cfg.EmbedProvider == "gateway" {
		embedder = embed.NewGateway(cfg.GatewayBaseURL, cfg.GatewayAPIKey, cfg.EmbeddingModel, cfg.EmbedDim, cfg.GatewayTimeout)
	} else {
		embedder = embed.NewLocal(cfg.EmbedDim)
	}
	vectors := vector.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.GatewayTimeout)
	if err = waitForCollection(ctx, vectors, cfg.EmbedDim); err != nil {
		// 向量库不可用时仍然启动：关键字检索可以独立降级使用。
		log.Printf("Qdrant 不可用，向量与混合检索将返回 503：%v", err)
	} else {
		log.Printf("Qdrant 集合 %s 就绪；嵌入实现 %s，维度 %d", cfg.QdrantCollection, embedder.Kind(), cfg.EmbedDim)
	}
	svc := retrieval.New(db, embedder, vectors, cfg)
	// 没有网关密钥时不构造对话客户端：问答直接退回「按命中切片原文摘录」，
	// 而不是每次请求都去撞一次 401。
	var chatClient retrieval.Answerer
	if strings.TrimSpace(cfg.GatewayAPIKey) != "" {
		chatClient = llm.New(cfg.GatewayBaseURL, cfg.GatewayAPIKey, cfg.ChatModel, cfg.GatewayTimeout)
		log.Printf("对话网关已启用：%s，模型 %s", cfg.GatewayBaseURL, cfg.ChatModel)
	} else {
		log.Printf("未配置 LLM_GATEWAY_API_KEY：问答将退回按本班命中切片原文摘录回答（仍带 [1][2] 出处）")
	}

	backfillCtx, backfillCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	backfillIndex(backfillCtx, svc, db)
	backfillCancel()

	app := server.New(cfg, db, svc, vectors, chatClient)
	httpServer := &http.Server{Addr: cfg.Addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 40 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Printf("CampusClaw API listening on %s", cfg.Addr)
		if e := httpServer.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = httpServer.Shutdown(shutdown)
}

// waitForCollection 等待 Qdrant 就绪并幂等地建立集合；维度不一致时直接暴露为启动错误。
func waitForCollection(ctx context.Context, vectors *vector.Store, dim int) error {
	var last error
	deadline := time.Now().Add(60 * time.Second)
	for {
		last = vectors.EnsureCollection(ctx, dim)
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// backfillIndex 为还没有任何切片的材料补齐索引，一律采用自动窗口策略。
// 失败只记录日志：原文仍然可查可下载，只是暂时无法参与向量检索。
func backfillIndex(ctx context.Context, svc *retrieval.Service, db *store.Store) {
	refs, err := db.EntriesMissingChunks(ctx)
	if err != nil {
		log.Printf("扫描待建索引材料失败：%v", err)
		return
	}
	for _, ref := range refs {
		body, err := db.EntryBody(ctx, ref.EntryID)
		if err != nil {
			continue
		}
		outcome, err := svc.IndexMaterial(ctx, ref, body, chunk.Options{Strategy: chunk.Auto})
		if err != nil {
			log.Printf("材料 %d 建索引失败：%v", ref.MaterialID, err)
			continue
		}
		log.Printf("材料 %d「%s」已按自动窗口建索引：切片 %d，就绪 %d，失败 %d", ref.MaterialID, ref.Title, outcome.Chunks, outcome.Ready, outcome.Failed)
	}
}
