# Design: Refactor ke Clean Architecture (khannedy-style)

Tanggal: 2026-07-05
Status: DRAFT — menunggu validasi

## Tujuan

Pecah monolith flat (semua di `package main`) menjadi layered architecture mengikuti
pola `khannedy/golang-clean-architecture`, dengan full separation:
`entity → repository → usecase → delivery`. Tetap pakai `pgx` + raw SQL (bukan GORM),
karena schema SQL sudah mature dan idiomatic untuk knowledge-graph queries.

## Masalah arsitektur saat ini

1. **Business logic & data access tercampur.** `db.go`/`crud.go` mengandung baik SQL
   murni MAUPUN domain rules (`defaultProject`, `normalizeEntityType`,
   `normalizeRelationType`, collision check, idempotent create-or-get, default limit).
2. **Global state.** `pool *pgxpool.Pool`, `jwtSecret`, `uiPassword`, `cookieInsecure`,
   `authCodes` adalah package-level globals → coupling tinggi, sulit di-test secara
   terisolasi, dependency tersembunyi.
3. **File gemuk.** `main.go` 704 baris mencampur: entry point + config loading + OAuth
   handlers + MCP handlers + 2 middleware + route wiring.
4. **Tidak ada seam untuk fitur baru.** Nambah decay-score / vector-search / versioning
   bakal menumpuk di file yang sudah gemuk.

## Aturan boundary usecase ↔ repository (KEPUTUSAN KUNCI)

Setiap fungsi db.go/crud.go saat ini = **satu method repository coarse-grained** yang
manage transaction-nya sendiri (atomicity tetap terjaga). Domain rules dipisah:

| Domain rule | Tempat | Alasan |
|---|---|---|
| `defaultProject()` (blank→default) | **usecase** | pure, no DB |
| `normalizeEntityType()` | **usecase** | pure, no DB |
| `normalizeRelationType()` | **usecase** | pure, no DB |
| default limit (20) | **usecase** | pure |
| "content/name required" validation | **usecase** | pure |
| collision check (perlu DB read dalam tx) | **repository** (dalam method) | butuh tx |
| idempotent create-or-get | **repository** (dalam method) | butuh tx |
| raw SQL + row scanning | **repository** | data access |

UseCase adalah **seam** untuk fitur masa depan: decay scoring, confidence score,
versioning, vector ranking — semua hook di sini tanpa menyentuh SQL.

## Struktur folder target

```
cmd/
  server/main.go                 ← entry point murni: config → deps → routes → listen
internal/
  config/config.go               ← struct Config + load/validate env (fail-fast)
  entity/memory.go               ← domain models: Entity, Observation, Relation,
  │                                EntityDetail, FullGraph, Metrics, SearchResult...
  model/
    memory.go                    ← *Input structs (JSON schema untuk MCP tools)
    converter.go                 ← entity ↔ model conversion
  repository/
    memory.go                    ← interface MemoryRepository
    postgres_memory.go           ← pgx impl (dari db.go + crud.go)
    stats.go                     ← interface + impl StatsRepository (dari stats.go)
    schema.go                    ← EnsureSchema (embed schema.sql)
  usecase/
    memory.go                    ← MemoryUseCase (domain rules + orchestration)
    stats.go                     ← StatsUseCase
  delivery/
    mcp/
      handlers.go                ← MCP tool handlers (dari main.go)
      server.go                  ← buildServer (registrasi 13 tools)
    http/
      oauth.go                   ← OAuth handlers + Claims/AuthCode (dari main.go)
      middleware.go              ← cors, authMiddleware, sessionAuth, csrf (gabungan)
      assets.go                  ← embed static/ + templates/ (dari assets.go)
      ui/handlers.go             ← UI handlers (dari web.go)
      routes.go                  ← wiring mux
  gateway/                       ← kosong sekarang; pgvector/S3 nanti
db/migrations/schema.sql         ← dipindah dari root
```

## Dependency Injection (membunuh globals)

- `MemoryRepository` (struct bawa-pgxpool) → di-pass ke `MemoryUseCase{repo}` →
  di-pass ke `delivery/mcp` handlers & `delivery/http/ui` handlers.
- `Config` struct menggantikan global `jwtSecret`/`uiPassword`/`cookieInsecure`/`token`.
- Middleware jadi closure/struct-method yang capture `Config`, bukan baca global.
- `authCodes` (OAuth) jadi field di struct `OAuthService`.

## Urutan migrasi (incremental strangler — test hijau tiap langkah)

1. **entity + model** — pindahkan structs (zero behavior change), perbaiki import.
2. **config** — extract Config struct + validate; main() pakai Config.
3. **repository** — extract MemoryRepository interface + postgres impl; pindahkan
   SQL functions. Ubah pemanggil untuk pakai repo method.
4. **usecase** — extract domain rules; repo methods terima nilai sudah-ternormalisasi.
5. **delivery/mcp** — pindahkan MCP handlers + buildServer.
6. **delivery/http** — pindahkan OAuth, middleware, UI handlers, assets, routes.
7. **cmd/server/main.go** — entry point tipis: compose semua, listen.
8. Pindah `schema.sql` → `db/migrations/`.

Setiap langkah: `go build ./...` + `go test ./...` harus lulus sebelum lanjut.

## Yang TIDAK berubah

- Behavior (semua test existing tetap relevan, hanya package path berubah).
- Schema SQL (hanya pindah lokasi).
- Dependencies (pgx, mcp-sdk, jwt, godotenv) — tidak tambah ORM.
- Endpoint & MCP tool names.

## Keputusan final (validated)

1. Boundary usecase/repository — **disetujui** (lihat tabel di atas).
2. **stats.go → StatsRepository + StatsUseCase terpisah** dari memory CRUD.
3. **delivery/http/ui dipecah per-resource**: `entity.go`, `observation.go`,
   `relation.go`, `backup.go`, `auth.go`, `dashboard.go`, `handlers.go` (shared helpers).
