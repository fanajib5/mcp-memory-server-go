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

> Catatan: project ini memakai `github.com/modelcontextprotocol/go-sdk` v1.0.0 (build terverifikasi
> bersih dengan `go build ./...` + `go vet`). SDK tersebut masih aktif berkembang; jika di masa depan
> terdapat perubahan nama field antar minor versi, biasanya hanya butuh penyesuaian kecil di
> `main.go` / `db.go`.

## Isi project

```
main.go            -> server setup, tool registration, HTTP + auth
db.go              -> query layer ke PostgreSQL (pgx)
schema.sql         -> skema entities / observations / relations + lookup type, di-embed ke binary (go:embed), auto-migrasi saat start
Dockerfile         -> multi-stage build -> distroless static image
docker-compose.yml -> untuk test lokal (Postgres + server jadi satu)
.env.example       -> template environment variables
```

## Tools yang di-expose ke agent

Delapan tool, diekspos via MCP Streamable HTTP. Tool create/add/delete/search/read menerima parameter
opsional `project` (default `"default"`) untuk mengisolasi graph per-project — lihat
[Project isolation & validasi type](#project-isolation--validasi-type).

- `memory_create_entities` — bikin entity baru + observasi awal; reuse entity kalau `(project, name)` sudah ada
- `memory_add_observations` — tambah fakta ke entity; bikin entity-nya kalau belum ada
- `memory_create_relations` — hubungkan entity, format `A --RELATION_TYPE--> B` (active voice, UPPER_SNAKE_CASE)
- `memory_delete_entities` — hapus entity (cascade ke observasi & relasi)
- `memory_search` — cari entity berdasarkan nama/isi observasi (pakai ini duluan, bukan read_graph)
- `memory_read_graph` — dump graph satu project (atau semua project kalau `project` kosong); untuk debugging
- `memory_export` — export graph (entities + relations) sebagai JSON terstruktur untuk backup/migrasi
- `memory_import` — import JSON ke sebuah project; idempoten (skip yang sudah ada)

## Project isolation & validasi type

**Project isolation.** Setiap entity dimiliki satu `project_id` (default `"default"`), dan identitas
entity adalah komposit `(project_id, name)` — nama yang sama boleh ada di project berbeda tanpa
bentrok. Semua tool menerima `project` opsional; search/delete/read hanya menyentuh entity di project
itu. Ini menjaga konteks antar-project tetap terpisah (konteks project A tidak akan nyangkut di hasil
search project B). Client lama yang tidak mengirim `project` otomatis jatuh ke `"default"`, jadi
tetap backward-compatible.

**Validasi type.** Menjaga graph rapi supaya bisa diandalkan untuk query terstruktur:
- `entity_type` harus merupakan type terdaftar di tabel lookup `memory_entity_types` (di-seed:
  `project`, `person`, `decision`, `tool`, `concept`, `place`). Nilai tak terdaftar ditolak oleh
  foreign key di Postgres. Menambah type baru = `INSERT` satu baris ke tabel lookup.
- `relation_type` dinormalisasi otomatis ke `UPPER_SNAKE_CASE` (`"deployed via"` → `DEPLOYED_VIA`),
  jadi variasi penulisan tidak menumpuk jadi type berbeda.

**Backup & portabilitas.** `memory_export` + `memory_import` memakai format JSON terstruktur yang
sama (round-trip lossless) — berguna untuk backup sebelum eksperimen skema, migrasi antar host, atau
memindahkan data dari server memory lama.

## 1. Test lokal

```bash
cd mcp-memory-server-go
cp .env.example .env      # isi DATABASE_URL, OAUTH_CLIENT_ID, OAUTH_CLIENT_SECRET, dan JWT_SECRET
docker compose up --build
```

Cek health check:
```bash
curl http://localhost:3000/health
# harus balas: {"status":"ok"}
```

Cek OAuth metadata:
```bash
curl https://localhost:3000/.well-known/oauth-authorization-server
```

Ambil access token:
```bash
curl -X POST https://localhost:3000/oauth/token \
  -d "client_id=myclient&client_secret=mysecret"
```

Uji MCP endpoint dengan MCP Inspector:
```bash
npx @modelcontextprotocol/inspector
# connect ke https://localhost:3000/mcp dengan header Authorization: Bearer <token dari /oauth/token>
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
- `MEMORY_API_TOKEN` — generate dengan `openssl rand -hex 32` (fallback auth jika OAuth tidak diisi)
- `JWT_SECRET` — secret untuk signing JWT opsional, default `MEMORY_API_TOKEN`
- `OAUTH_CLIENT_ID` — client ID untuk OAuth 2.0 (untuk Claude.ai custom connector)
- `OAUTH_CLIENT_SECRET` — client secret untuk OAuth 2.0 (untuk Claude.ai custom connector)
- `PORT=3000`
- `PUBLIC_URL` — URL publik server, misal `https://memory.example.com` (untuk OAuth metadata issuer)
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

## 5. Sambungkan dari Claude.ai (Custom Connector)

Server ini mendukung OAuth 2.0 client_credentials untuk Claude.ai custom connector. Isi form
di Claude.ai dengan:

- **Name**: `memory` (atau nama lain yang kamu suka)
- **Remote mcp server url**: `https://memory.example.com/mcp`
- **OAuth Client ID**: isi dengan value env `OAUTH_CLIENT_ID`
- **OAuth Client Secret**: isi dengan value env `OAUTH_CLIENT_SECRET`

Endpoint OAuth yang disediakan:

- `/.well-known/oauth-authorization-server` — metadata server OAuth
- `/oauth/token` — menukar client credentials menjadi JWT access token
- `/oauth/register` — dynamic client registration (echo client_id/secret)

Contoh env vars untuk Claude.ai:

```env
DATABASE_URL=postgres://user:pass@postgres:5432/memory
OAUTH_CLIENT_ID=claude-ai-client
OAUTH_CLIENT_SECRET=<openssl rand -hex 32>
JWT_SECRET=<openssl rand -hex 32>
PUBLIC_URL=https://memory.example.com
PORT=3000
```

Catatan: `client_id` dan `client_secret` yang kamu daftarkan di Claude.ai harus sama persis dengan
nilai `OAUTH_CLIENT_ID` dan `OAUTH_CLIENT_SECRET` di server.

## 5. Integrasi dengan instruksi agent

Nama tool mengikuti konvensi MCP knowledge-graph (`memory_*`), sehingga instruksi memori yang sudah
ada pada `CLAUDE.md` / `AGENTS.md` umumnya dapat dipakai tanpa perubahan. Server ini fokus pada
knowledge graph atomik (entity/relation); jika memakai layer memori tambahan berbasis dokumen
markdown per-project, keduanya bisa berjalan berdampingan — knowledge graph di sini, dokumen lokal
di sana.

## Catatan pengembangan lanjutan

- Semantic search: tambahkan ekstensi `pgvector` + kolom embedding pada `memory_observations`. Search
  saat ini hanya full-text match; semantic menangkap makna (mis. "pindah dari Redis" bisa menemukan
  observasi yang menyebut "predis"). Butuh generate embedding tiap observasi baru.
- Graph traversal multi-hop: tool `memory_find_path(from, to)` untuk menjelajah hubungan tidak
  langsung begitu graph membesar.
- Audit trail: kolom `source` / `session_id` di `memory_observations` untuk melacak agent atau sesi
  mana yang menulis suatu fakta.
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
