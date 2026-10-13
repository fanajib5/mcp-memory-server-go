# AGENTS.md — Shared Working Brain & Agent Protocol

Panduan integrasi dan protokol kerja agent AI (Claude Code, Antigravity, Kilo Code, dll.) dengan `mcp-memory-server-go`.

---

## 1. Multi-Agent Environment Variables

Untuk mendukung atribusi multi-agent, multi-device, dan audit trail yang akurat di knowledge graph serta timeline, pastikan konfigurasi environment variable berikut disetel pada environment mesin / agent:

```bash
export MCP_AGENT_LABEL="Claude Code @ laptop kantor"
export MCP_SOURCE="claude-code"
```

- `MCP_AGENT_LABEL`: Label human-readable yang mengidentifikasi agent dan device (mis. `"Claude Code @ laptop kantor"`, `"Antigravity @ PC rumah"`, `"Kilo @ MacBook Air"`).
- `MCP_SOURCE`: Identifier aplikasi / runtime client (mis. `"claude-code"`, `"antigravity"`, `"kilo"`).

Jika agent berjalan tanpa `MCP_AGENT_LABEL`, bootstrap akan mengembalikan `warning` non-blocking agar user diingatkan untuk mengonfigurasikannya.

---

## 2. Agent Session Lifecycle Protocol

### A. Session Start (Bootstrap)
Setiap awal sesi atau pergantian context baru pada suatu project, **WAJIB** panggil `memory_session_bootstrap`:

```json
{
  "project": "nama-project",
  "focus": "topik atau task yang akan dikerjakan",
  "source": "claude-code",
  "agent_label": "Claude Code @ laptop kantor"
}
```

- **Periksa Response Bootstrap**:
  - `checkpoint`: Berisi state aktif sesi terakhir (`focus`, `uncommitted`, `blockers`, `next_step`, `saved_at`, `saved_by`). Gunakan ini untuk langsung melanjutkan pekerjaan tanpa perlu user mengulang penjelasan.
  - `breadcrumb_trail`: Rekam jejak progres naratif sesi-sesi sebelumnya (hingga 20 entri terbaru).
  - `open_tasks`: Entity bertipe `task` yang belum berstatus done.
  - `recent_decisions`: Keputusan arsitektur dalam 14 hari terakhir.
  - `top_entities`: Entity kunci yang relevan dengan fokus kerja.
- **Handling Warning**:
  - Jika response bootstrap memiliki field `warning` (menandakan `agent_label` belum disetel), ingatkan user secara sopan:
    > "⚠️ `MCP_AGENT_LABEL` dan `MCP_SOURCE` belum disetel pada environment mesin. Checkpoint dan event dicatat sebagai 'unknown'. Tambahkan `export MCP_AGENT_LABEL='...'` dan `export MCP_SOURCE='...'` agar riwayat terlacak per device."

### B. During Session (Work & Decisions)
- **Pencarian Memori**: Selalu gunakan `memory_search` (dengan filter opsional `entity_types` dan `tags_any`) sebelum membaca graph penuh.
- **Pencatatan Fakta & Keputusan**: Catat keputusan arsitektur (entity type `decision`), task (entity type `task`), dan fakta penting menggunakan `memory_create_entities` atau `memory_add_observations`. Sertakan `agent_label` dan `source`.
- **Investigasi Timeline**: Jika ingin melihat kronologi perubahan naratif lintas session/device, gunakan `memory_timeline`.

### C. Session End / Task Wrap-up (Save Checkpoint)
Sebelum sesi berakhir, saat berganti task, atau sebelum jeda/pindah device, **WAJIB** panggil `memory_save_checkpoint`:

```json
{
  "project": "nama-project",
  "focus": "Implementasi endpoint OAuth flow",
  "uncommitted": "Ada perubahan di internal/delivery/http/oauth.go yang belum di-commit",
  "blockers": "Menunggu konfirmasi format token dari team frontend",
  "next_step": "Lanjutkan unit test handler oauth setelah format token confirmed",
  "breadcrumb": "Wiring route OAuth dan middleware validation",
  "source": "claude-code",
  "agent_label": "Claude Code @ laptop kantor"
}
```

- `memory_save_checkpoint` akan memperbarui checkpoint aktif `<project>:checkpoint:active` dan menambahkan breadcrumb ke `<project>:checkpoint:trail`, serta mencatat event `checkpoint_saved` ke history audit.

---

## 3. Dokumentasi MCP Tools Baru & Parameter Tambahan

### `memory_session_bootstrap`
Memuat konteks kerja lengkap sesi sebelumnya dalam satu tool call.
- **Parameters**:
  - `project` (string, required): Namespace project.
  - `focus` (string, optional): Topik atau kata kunci untuk membiaskan pencarian entity relevan.
  - `source` (string, optional): Client identifier (mis. `"claude-code"`).
  - `agent_label` (string, optional): Label agent/device (mis. `"Claude Code @ laptop kantor"`).
  - `session_id` (string, optional): ID sesi jika tersedia.
- **Output**:
  - `warning` (string jika agent_label tidak diisi).
  - `checkpoint` (object: `focus`, `uncommitted`, `blockers`, `next_step`, `saved_at`, `saved_by`).
  - `breadcrumb_trail` (array string, max 20 entri terbaru).
  - `open_tasks` (array entity).
  - `recent_decisions` (array entity).
  - `top_entities` (array entity).

### `memory_save_checkpoint`
Menyimpan snapshot pekerjaan aktif dan mencatat jejak naratif sesi.
- **Parameters**:
  - `project` (string, required): Namespace project.
  - `focus` (string, required): Hal atau task yang sedang dikerjakan.
  - `uncommitted` (string, optional): Ringkasan perubahan lokal yang belum di-commit ke git.
  - `blockers` (string, optional): Kendala atau pertanyaan yang belum selesai.
  - `next_step` (string, optional): Tindakan selanjutnya yang disarankan untuk sesi berikutnya.
  - `breadcrumb` (string, optional): Ringkasan pendek (≤80 karakter) untuk trail (default: 80 karakter pertama dari focus).
  - `source` (string, optional): Client identifier.
  - `agent_label` (string, optional): Label agent/device.
  - `session_id` (string, optional): ID sesi.

### `memory_timeline`
Mengambil urutan kronologis naratif dari event dan observasi dalam suatu project.
- **Parameters**:
  - `project` (string, required): Namespace project.
  - `since` (string, optional): Cutoff timestamp ISO 8601 / RFC 3339 (mis. `"2026-10-13T00:00:00Z"`).
  - `limit` (int, optional): Jumlah event maksimal (default 50, max 500).
  - `entity_name` (string, optional): Filter untuk satu nama entity spesifik.
  - `source` (string, optional): Filter berdasarkan source agent/device.

### Parameter Baru pada `memory_search`
- `entity_types` (array string, optional): Filter tipe entity (mis. `["decision", "task"]`).
- `tags_any` (array string, optional): Filter entity yang memiliki minimal satu tag dari daftar yang diberikan.
