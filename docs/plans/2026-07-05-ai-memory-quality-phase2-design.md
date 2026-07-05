# Design: AI Memory Quality — Phase 2 (pgvector Semantic Search)

Tanggal: 2026-07-05
Status: VALIDATED
Phase: 2 of 2 (Phase 1 = confidence + decay, sudah selesai)

## Tujuan

Tambahkan semantic search via pgvector: query konseptual cocok berdasarkan
makna (cosine similarity), bukan hanya exact keyword. Hybrid fusion dengan
lexical (ts_rank) yang sudah ada.

## Keputusan design (validated)

1. **Embedding source**: Ollama local (container sibling), model nomic-embed-text (768-dim, multilingual). Self-hosted, private, no API cost.
2. **Granularity**: per-observation. Setiap fakta punya vector sendiri. Search group ke entity.
3. **Strategy**: hybrid. `final = (lexical*0.3 + semantic*0.7) × avg_conf × recency`.
4. **Indexing**: synchronous embed-on-write (tool personal, volume rendah). Fail-safe: embed error → NULL embedding, lexical tetap jalan.
5. **No new MCP tool**: fusion transparan, memory_search tetap satu tool.
6. **Graceful fallback**: Ollama mati → lexical-only (Phase 1 behavior).

## Infra changes

**docker-compose.yml**:
- Postgres image: `postgres:17-alpine` → `pgvector/pgvector:pg17` (memuat vector extension)
- Service baru: `ollama` (image ollama/ollama, volume ollama_data, model pulled at startup)
- mcp-memory env: `OLLAMA_URL=http://ollama:11434`, `OLLAMA_EMBED_MODEL=nomic-embed-text`

**Config baru** (`internal/config/config.go`):
- `OllamaURL` (default `http://ollama:11434`)
- `OllamaEmbedModel` (default `nomic-embed-text`)

## Schema changes (idempotent)

```sql
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS embedding vector(768);
CREATE INDEX IF NOT EXISTS idx_observations_embedding
    ON memory_observations USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
```

Existing observations → embedding NULL (semantic off untuk baris itu, lexical tetap).

## Layer changes

### Gateway (`internal/gateway/embedding.go`) — BARU
```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}
```
- `OllamaClient` impl: POST http://ollama:11434/api/embeddings.
- Batch embedding (Ollama support list input).
- Timeout via ctx (caller set 5s).

### Repository (`internal/repository/memory.go`)
- `Search()` signature: + `queryVec []float32` param (opsional).
  - non-nil → hybrid path: semantic candidates via ivfflat ANN (top-K, cosine >= 0.2 threshold) UNION lexical candidates, recompute final_score.
  - nil → lexical-only (Phase 1 behavior).
- `AddObservations()`/`CreateEntities()`/`Import()`: + `embeddings [][]float32` param parallel.
- `UpdateObservationByContent()`: + `newEmbedding []float32` param.

### UseCase (`internal/usecase/memory.go`)
- Field `embedder gateway.Embedder` (nil = semantic off).
- `Search()`: embed query best-effort (5s timeout) → pass vector ke repo. Gagal → pass nil.
- `AddObservations()`/`CreateEntities()`/`Import()`/`UpdateObservationByContent()`: embed new content sync → pass vectors. Gagal → nil, log warning.

### Delivery
- MCP: no new tool. SearchResult.Score = hybrid final_score.
- HTTP UI: per-observation embedding indicator (• = embedded).
- Backfill: `go run ./cmd/server -backfill-embeddings` (flag) iterates NULL embeddings.

## Scoring formula (hybrid)

```
lexical_score   = ts_rank(tsvector, query)
semantic_score  = max(cosine_similarity(query_vec, embedding)) per entity, >= 0.2 threshold
hybrid_score    = lexical_score * 0.3 + semantic_score * 0.7
final_score     = hybrid_score * avg_confidence * recency_factor
```

## Testing
- Unit: gateway mock (Embedder interface), hybrid fusion math.
- Integration: semantic match (query konsep cocok observation beda kata),
  hybrid fusion order, Ollama-down fallback (lexical only), embed-on-write stored,
  backfill populates NULL.
- Mock Ollama via httptest server in test harness.

## Yang TIDAK berubah
- Behavior existing saat Ollama unavailable (graceful lexical-only fallback).
- MCP tool names & input shapes.
- Phase 1 confidence/decay scoring tetap active dalam final_score.
