package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-memory-server/internal/backup"
	"mcp-memory-server/internal/config"
	httpdelivery "mcp-memory-server/internal/delivery/http"
	mcpdelivery "mcp-memory-server/internal/delivery/mcp"
	"mcp-memory-server/internal/event"
	"mcp-memory-server/internal/gateway"
	"mcp-memory-server/internal/repository"
	"mcp-memory-server/internal/usecase"
)

func main() {
	backfill := flag.Bool("backfill-embeddings", false, "embed all observations with NULL embedding, then exit")
	flag.Parse()

	ctx := context.Background()
	cfg := config.Load()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

	if err := repository.EnsureSchema(ctx, pool); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}

	if *backfill {
		runBackfill(ctx, cfg, pool)
		return
	}

	// Wire dependencies: repository -> usecase -> delivery.
	memRepo := repository.NewMemoryRepository(pool)
	statsRepo := repository.NewStatsRepository(pool)

	// Embedder: Ollama local if configured, else noop (semantic off).
	var embedder gateway.Embedder
	if cfg.OllamaURL != "" {
		embedder = gateway.NewOllamaClient(cfg.OllamaURL, cfg.OllamaEmbedModel)
	}

	bus := event.NewBus()

	memUC := usecase.NewMemoryUseCase(memRepo, embedder, bus)
	statsUC := usecase.NewStatsUseCase(statsRepo)

	// LLM client: Kilo Gateway (or Ollama-compatible). Only wired if API key is set.
	var llmClient gateway.LLMClient
	llmClient = gateway.NewKiloGatewayClient(cfg.KiloGatewayBaseURL, cfg.KiloGatewayAPIKey, cfg.KiloGatewayModel)

	chatUC := usecase.NewChatUseCase(llmClient, memUC)

	handlers := mcpdelivery.NewHandlers(memUC)
	server := mcpdelivery.BuildServer(handlers)
	mcpHandler := mcpgo.NewStreamableHTTPHandler(
		func(*http.Request) *mcpgo.Server { return server },
		&mcpgo.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	oauth := httpdelivery.NewOAuthService(cfg)
	tmpl := httpdelivery.MustParseTemplates()
	ui := &httpdelivery.UI{
		Cfg:     cfg,
		UC:      memUC,
		SU:      statsUC,
		Tmpl:    tmpl,
		Session: httpdelivery.NewSession(cfg),
		Chat:    httpdelivery.NewChatHandler(chatUC),
	}

	sse := httpdelivery.NewSSEHandler(bus)

	router := httpdelivery.NewRouter(cfg, mcpHandler, oauth, ui, sse)

	// Graceful shutdown context.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Auto-backup scheduler (no-op if BACKUP_CRON empty).
	scheduler := backup.NewScheduler(memUC, cfg.BackupDir, cfg.BackupRetention, cfg.BackupCron)
	go scheduler.Start(ctx, cfg.BackupOnStart)

	httpServer := &http.Server{Addr: ":" + cfg.Port, Handler: router}

	go func() {
		log.Printf("mcp-memory-server listening on :%s (%s)", cfg.Port, cfg.Describe())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(shutdownCtx)
	log.Printf("stopped")
}

// runBackfill iterates all observations with NULL embedding, embeds them in
// batches, and UPDATEs the rows. Exits on completion. Requires OLLAMA_URL.
func runBackfill(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) {
	if cfg.OllamaURL == "" {
		log.Fatal("backfill requires OLLAMA_URL to be set")
	}
	embedder := gateway.NewOllamaClient(cfg.OllamaURL, cfg.OllamaEmbedModel)
	repo := repository.NewMemoryRepository(pool)

	const batchSize = 50
	total := 0
	for {
		rows, err := pool.Query(ctx, `
			SELECT id, content FROM memory_observations
			WHERE embedding IS NULL ORDER BY id LIMIT $1`, batchSize)
		if err != nil {
			log.Fatalf("backfill query: %v", err)
		}
		type pending struct {
			id      int
			content string
		}
		var batch []pending
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.content); err != nil {
				rows.Close()
				log.Fatalf("backfill scan: %v", err)
			}
			batch = append(batch, p)
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}

		texts := make([]string, len(batch))
		for i, p := range batch {
			texts[i] = p.content
		}
		vecs, err := embedder.Embed(ctx, texts)
		if err != nil {
			log.Fatalf("backfill embed: %v", err)
		}

		for i, p := range batch {
			if err := repo.UpdateObservation(ctx, "", p.id, p.content, nil, vecs[i]); err != nil {
				log.Printf("backfill: skip obs %d: %v", p.id, err)
			}
		}
		total += len(batch)
		log.Printf("backfill: embedded %d observations (%d total)", len(batch), total)
	}
	log.Printf("backfill complete: %d observations embedded", total)
}
