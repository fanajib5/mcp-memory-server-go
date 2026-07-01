# mcp-memory-server (Go)

Self-hosted MCP knowledge-graph memory server yang ditulis dalam Go. Deploy sekali ke VPS/Coolify,
lalu sambungkan dari Claude Code, Kilo Code, atau MCP client mana pun, dari device mana pun — semua
membaca dan menulis ke database PostgreSQL yang sama. Berguna untuk membangun memori bersama antar
device dan antar session untuk agent AI.

Implementasi ini mengikuti konvensi MCP knowledge-graph (entity, observation, relation) dan
dibungkus sebagai binary Go statis: image container jauh lebih kecil, startup nyaris instan, dan
penggunaan RAM idle rendah — ideal ketika menjalankan banyak service kecil di satu VPS yang
resource-nya terbatas.

## Prasyarat & build

- Go **1.23** atau lebih baru.
- Instance PostgreSQL 12+ (bisa service Postgres yang sudah berjalan di Coolify, atau yang disediakan
  via `docker-compose.yml`).
- Clone repo, lalu:

```bash
go mod tidy      # resolve & lock dependency versions
go build ./...   # verifikasi kode compile
```

> Catatan: project ini memakai `github.com/modelcontextprotocol/go-sdk` v1.0.0. SDK tersebut masih
> aktif berkembang; jika di masa depan terdapat perubahan nama field antar minor versi, biasanya
> hanya butuh penyesuaian kecil di `main.go` / `db.go`.

## Isi project

```
main.go            -> server setup, tool registration, HTTP + auth
db.go              -> query layer ke PostgreSQL (pgx)
schema.sql         -> skema entities / observations / relations, di-embed ke binary (go:embed), auto-migrasi saat start
Dockerfile         -> multi-stage build -> distroless static image
docker-compose.yml -> untuk test lokal (Postgres + server jadi satu)
.env.example       -> template environment variables
```

## Tools yang di-expose ke agent

Enam tool, diekspos via MCP Streamable HTTP:

- `memory_create_entities` — bikin entity baru (project, person, decision, tool, dll) + observasi awal; reuse entity kalau nama sudah ada
- `memory_add_observations` — tambah fakta ke entity; bikin entity-nya kalau belum ada
- `memory_create_relations` — hubungkan entity, format `A --RELATION_TYPE--> B` (active voice, UPPER_SNAKE_CASE)
- `memory_delete_entities` — hapus entity (cascade ke observasi & relasi)
- `memory_search` — cari entity berdasarkan nama/isi observasi (pakai ini duluan, bukan read_graph)
- `memory_read_graph` — dump seluruh graph (untuk debugging, hindari dipakai rutin karena berat)

## 1. Test lokal

```bash
cd mcp-memory-server-go
cp .env.example .env      # isi MEMORY_API_TOKEN dengan token acak
docker compose up --build
```

Cek health check:
```bash
curl http://localhost:3000/health
# harus balas: {"status":"ok"}
```

Uji MCP endpoint dengan MCP Inspector:
```bash
npx @modelcontextprotocol/inspector
# connect ke http://localhost:3000/mcp dengan header Authorization: Bearer local-dev-token-change-me
```

> Catatan: `docker-compose.yml` memetakan Postgres ke port host **5433** (bukan 5432) dan server ke
> **3000**, supaya tidak bentrok dengan instance Postgres lokal lain.

## 2. Deploy ke Coolify

1. Push project ini ke repo Git (GitHub/GitLab).
2. Di Coolify: **New Resource → Application → Docker Compose** (atau gunakan Dockerfile saja jika
   memakai instance Postgres yang sudah ada, tanpa service `db` di compose).
3. Jika sudah ada instance Postgres di Coolify, cukup buat database baru di instance tersebut dan
   arahkan `DATABASE_URL` ke sana — tidak perlu Postgres terpisah kecuali ingin isolasi penuh.
4. Set environment variables:
   - `DATABASE_URL` — connection string Postgres (gunakan internal network Coolify, bukan public)
   - `MEMORY_API_TOKEN` — generate dengan `openssl rand -hex 32`
   - `PORT=3000`
5. Set domain, misal `memory.example.com` — Coolify otomatis mengurus SSL (Let's Encrypt).
6. Deploy, lalu pastikan `https://memory.example.com/health` membalas `{"status":"ok"}`.

### Keamanan tambahan (opsional tapi disarankan)
- Bearer token diverifikasi dengan `crypto/subtle.ConstantTimeCompare` (constant-time), tahan
  timing-attack — tetap pastikan token tidak terekspos di log atau tempat publik.
- Batasi akses via firewall / IP allowlist Coolify jika IP client statis.
- Atau tempatkan di belakang Tailscale/WireGuard untuk akses yang sepenuhnya privat.
- Jangan commit file `.env` ke Git.

## 3. Sambungkan dari Claude Code

Pada `.mcp.json` atau konfigurasi Claude Code:

```json
{
  "mcpServers": {
    "memory": {
      "type": "http",
      "url": "https://memory.example.com/mcp",
      "headers": {
        "Authorization": "Bearer <MEMORY_API_TOKEN>"
      }
    }
  }
}
```

## 4. Sambungkan dari Kilo Code

Pola yang sama, pada `kilo.jsonc`:

```json
{
  "mcp": {
    "memory": {
      "type": "remote",
      "url": "https://memory.example.com/mcp",
      "headers": {
        "Authorization": "Bearer <MEMORY_API_TOKEN>"
      }
    }
  }
}
```

Karena transportnya MCP Streamable HTTP, server ini kompatibel dengan MCP client mana pun. Konfigurasi
serupa dapat dipakai di setiap device — sumber data berada di server, sehingga seluruh device melihat
memori yang sama.

## 5. Integrasi dengan instruksi agent

Nama tool mengikuti konvensi MCP knowledge-graph (`memory_*`), sehingga instruksi memori yang sudah
ada pada `CLAUDE.md` / `AGENTS.md` umumnya dapat dipakai tanpa perubahan. Server ini fokus pada
knowledge graph atomik (entity/relation); jika memakai layer memori tambahan berbasis dokumen
markdown per-project, keduanya bisa berjalan berdampingan — knowledge graph di sini, dokumen lokal
di sana.

## Catatan pengembangan lanjutan

- Tambah ekstensi `pgvector` + kolom embedding pada `memory_observations` untuk semantic search
  (bukan hanya full-text match) — berguna jika volume observasi mencapai ribuan baris.
- Tambah tabel `memory_projects` untuk isolasi data per-project/per-client (multi-tenant), supaya
  konteks satu project tidak tercampur ke hasil search project lain.
- Transport saat ini `Stateless: true` per-request — cocok untuk beban ringan-menengah. Untuk trafik
  tinggi, dapat dioptimasi ke session-based transport dengan connection pooling yang lebih agresif.

## Kenapa Go (dan bukan Node / Rust)

| | Node/TypeScript | Go |
|---|---|---|
| Base image size | ~150-200 MB (node:22-slim) | ~15-25 MB (distroless static, `CGO_ENABLED=0`, `nonroot`) |
| RAM idle | ~80-150 MB | ~10-20 MB |
| Startup time | ~200-500ms | ~10-50ms |
| Concurrency model | Event loop (single-threaded JS) | Goroutines (native OS threads, jauh lebih murah untuk request paralel) |
| Runtime dependency di image | Butuh Node runtime | Nol — binary statis; distroless tanpa shell/package manager |

Untuk beban ringan-menengah, perbedaan ini tidak terlalu terasa. Beda nyatanya muncul ketika
menjalankan banyak service sekaligus di satu VPS kecil (mis. 2GB RAM) — versi Go menyisakan lebih
banyak ruang untuk service lain (Postgres, aplikasi web, dll).

Rust (via SDK `rmcp`) sedikit lebih hemat lagi dari Go karena tidak ada GC overhead, namun untuk use
case ini (CRUD sederhana ke Postgres dengan beberapa tool call per menit) perbedaannya tidak terasa,
sementara kurva belajar Rust lebih curam. Go menjadi pilihan default yang seimbang: ringan, cepat
dikompilasi, dan mudah dipelihara.
