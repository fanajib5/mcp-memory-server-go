package main

import (
	"context"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-memory-server/internal/config"
	httpdelivery "mcp-memory-server/internal/delivery/http"
	mcpdelivery "mcp-memory-server/internal/delivery/mcp"
	"mcp-memory-server/internal/repository"
	"mcp-memory-server/internal/usecase"
)

func main() {
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

	// Wire dependencies: repository -> usecase -> delivery.
	memRepo := repository.NewMemoryRepository(pool)
	statsRepo := repository.NewStatsRepository(pool)
	memUC := usecase.NewMemoryUseCase(memRepo)
	statsUC := usecase.NewStatsUseCase(statsRepo)

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
	}

	router := httpdelivery.NewRouter(cfg, mcpHandler, oauth, ui)

	log.Printf("mcp-memory-server listening on :%s (%s)", cfg.Port, cfg.Describe())
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal(err)
	}
}
