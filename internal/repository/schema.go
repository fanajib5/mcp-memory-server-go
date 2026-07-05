package repository

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaFS embed.FS

// EnsureSchema runs schema.sql on startup so the container is self-sufficient.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	sql, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema.sql: %w", err)
	}
	_, err = pool.Exec(ctx, string(sql))
	return err
}
