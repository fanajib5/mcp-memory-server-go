# Design: Persistent OAuth Connection (Refresh Token Stateless JWT)

Tanggal: 2026-10-23
Status: IMPLEMENTED
Oleh: Kilo @ laptop kantor (diskusi arsitektur + approval Faiq)

## Tujuan

Koneksi OAuth remote MCP (Claude Web, Grok Web) harus persisten antar sesi
chat — tanpa reconnect ulang. Benchmark: Google Drive MCP yang tetap
terhubung karena OAuth server-nya (Google) mengeluarkan refresh token.

## Problem & root cause

Gejala: tiap sesi chat baru di Claude Web/Grok Web, connector minta
reconnect ulang (gejala khas "Connection has expired. You can reconnect to
re-authenticate").

Root cause terkonfirmasi di kode:

1. **Tidak ada refresh_token sama sekali.** `issueToken` hanya mengeluarkan
   `access_token` TTL 1 jam. `HandleToken` cuma mengenali
   `authorization_code`; `grant_type=refresh_token` jatuh diam-diam ke
   `tokenFromClientCredentials` → `invalid_client` untuk DCR client.
   Dokumen resmi Claude (connectors/authentication): _"Claude refreshes
   tokens reactively on a 401, and proactively up to five minutes before the
   stored expiry"_ — begitu refresh gagal, koneksi dianggap expired.
2. **Registry DCR client + auth code in-memory** (map di `OAuthService`) —
   restart/redeploy Coolify menghapus semua `client_id` terdaftar → token
   request berikutnya `invalid_client` → wajib DCR + re-auth.
3. **Metadata** `grant_types_supported` tidak mengiklankan `refresh_token`,
   dan `code_challenge_methods_supported` tidak diiklankan walau S256
   didukung.

Residual risk sisi klien (bukan tanggung jawab server): bug Anthropic
claude-ai-mcp#228 / #836 / #81020 — kadang claude.ai web tidak memanggil
refresh walaupun server sudah benar. Cek sisi klien dulu sebelum curiga
server jika drop masih terjadi setelah deploy.

## Keputusan design (Faiq pilih Opsi A)

Opsi A (dipilih): **refresh token stateless JWT**, tanpa perubahan skema.
Opsi B (ditolak): DB-backed refresh token + rotasi + tabel `oauth_clients`
(revocable, OAuth 2.1 penuh, tapi +2 tabel & lebih banyak kode).

Alasan: effort minimal, cukup untuk server personal single-user, dan refresh
token self-verifikasi membuatnya survive restart tanpa persistensi DB.

Trade-off sadar:

- Tidak bisa revoke per-token (stateless). Sesi terbatas lifetime refresh
  token (default 30 hari).
- Rotasi refresh token sliding: token lama tetap valid sampai exp-nya
  sendiri (tanpa revocation list).
- Access token lama tanpa claim `use` tetap diterima authMiddleware
  (backward compat rollout); token `use=refresh` selalu ditolak sebagai
  bearer.

## Design

### Claims & token

- `Claims` (oauth.go) ditambah field `Use` (`use` claim): `access` | `refresh`.
- Access token: `use=access`, TTL default **24 jam**.
- Refresh token: `use=refresh`, `sub=client_id`, TTL default **30 hari**.
- Keduanya JWT HS256 dengan `JWT_SECRET`.

### Grant `refresh_token` (token.go)

- Request: `grant_type=refresh_token&refresh_token=...&client_id=...`
  (client_id juga boleh via Basic; opsional).
- Validasi: signature + exp + `use == "refresh"` + binding
  `client_id == sub` token. **Tidak lookup registry client** → restart-proof.
- Response: access token baru + refresh token baru (rotasi sliding).

### Konfigurasi (config.go)

| Env                       | Satuan | Default           |
| ------------------------- | ------ | ----------------- |
| `OAUTH_ACCESS_TOKEN_TTL`  | detik  | 86400 (24 jam)    |
| `OAUTH_REFRESH_TOKEN_TTL` | detik  | 2592000 (30 hari) |

### Metadata (oauth.go)

- `grant_types_supported` += `refresh_token`.
- `code_challenge_methods_supported: ["S256","plain"]`.

### Keamanan (middleware.go)

- `authMiddleware` menolak token dengan `use=refresh` sebagai bearer
  (refresh token tidak boleh dipakai mengakses `/mcp`).

### Perbaikan dispatch grant (token.go)

- Grant tak dikenal → `400 unsupported_grant_type` (sebelumnya jatuh diam-diam
  ke client_credentials). `client_credentials` dan grant_type kosong
  (legacy) tetap dilayani.

## Layer changes

### `internal/config/config.go`

- Field `AccessTokenTTL`, `RefreshTokenTTL` + parsing env (detik).

### `internal/delivery/http/oauth.go`

- `Claims.Use`; metadata grant + PKCE methods.

### `internal/delivery/http/token.go`

- `tokenResponse` struct; `signToken`; `issueToken` mengeluarkan
  `refresh_token` + `expires_in` sesuai TTL konfigurasi;
  `tokenFromRefreshToken`; dispatch switch di `HandleToken`.

### `internal/delivery/http/middleware.go`

- Guard `claims.Use != tokenUseRefresh`.

### `internal/delivery/http/refresh_token_test.go` — BARU

TDD RED→GREEN, 7 test:
issuance refresh_token, refresh grant (valid/expired/access-as-refresh/
client mismatch/missing), **restart survival** (OAuthService baru, registry
kosong, JWT sama), unsupported_grant_type, metadata, refresh-as-bearer
ditolak, TTL configurable end-to-end via `config.Load`.

Verifikasi: `go test ./...`, `gofmt -l`, `go vet`, `go build` bersih.

## Follow-up (belum diimplementasi)

1. **Security gap (prioritas):** `/oauth/authorize` auto-approve tanpa
   autentikasi user + DCR terbuka tanpa initial access token = siapa pun
   yang menemukan endpoint bisa dapat JWT akses penuh ke knowledge graph.
   Rekomendasi: gate authorize dengan login UI (`Session`) atau wajibkan
   initial access token untuk DCR.
2. Opsi B (DB-backed refresh + rotasi + revoke) jika kelak butuh revocation
   atau multi-user.
3. Jika Claude Web masih tidak memanggil refresh (bug klien #228), fallback:
   perpanjang `OAUTH_ACCESS_TOKEN_TTL` (mis. 30 hari) — mitigation, bukan fix
   root cause.
