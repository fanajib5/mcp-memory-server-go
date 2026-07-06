-- MCP Memory Server schema
-- Knowledge graph: entities + observations + relations
-- Idempotent: safe to re-run on every startup (EnsureSchema runs the whole file).

-- ---- Lookup: registered entity types (feature #3) ----
-- Seeded BEFORE the entities FK so 'concept' (the column default) always exists.
CREATE TABLE IF NOT EXISTS memory_entity_types (
    name TEXT PRIMARY KEY
);
INSERT INTO memory_entity_types (name) VALUES
    ('project'), ('person'), ('decision'), ('tool'), ('concept'), ('place')
ON CONFLICT (name) DO NOTHING;

-- ---- Entities (feature #1: project_id isolation) ----
-- Identity is the composite (project_id, name), so the same name can exist in
-- different projects. Fresh installs get this shape from CREATE TABLE; existing
-- installs (created by an older schema with a standalone name UNIQUE) are migrated
-- by the idempotent ALTERs below.
CREATE TABLE IF NOT EXISTS memory_entities (
    id           SERIAL PRIMARY KEY,
    project_id   TEXT NOT NULL DEFAULT 'default',
    name         TEXT NOT NULL,
    entity_type  TEXT NOT NULL DEFAULT 'concept',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_entities_project_name UNIQUE (project_id, name)
);

-- Migrations for installs created by an older schema (no-ops on a fresh table):
ALTER TABLE memory_entities ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE memory_entities DROP CONSTRAINT IF EXISTS memory_entities_name_key;
-- Add the composite UNIQUE only if missing (checking pg_constraint is more reliable
-- than trapping duplicate_object, since ADD CONSTRAINT ... UNIQUE raises 42P07).
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_entities_project_name') THEN
        ALTER TABLE memory_entities ADD CONSTRAINT uq_entities_project_name UNIQUE (project_id, name);
    END IF;
END $$;

-- FK: entity_type must be a registered type. Added after seeding the lookup.
-- (ON UPDATE CASCADE lets you rename a type; ON DELETE defaults to RESTRICT so a
--  type in use cannot be dropped. Fails on existing rows with unregistered types —
--  safe here because the project is pre-deployment with no data.)
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_entity_type') THEN
        ALTER TABLE memory_entities
            ADD CONSTRAINT fk_entity_type FOREIGN KEY (entity_type)
            REFERENCES memory_entity_types(name) ON UPDATE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_entities_project ON memory_entities (project_id);

-- ---- Observations ----
CREATE TABLE IF NOT EXISTS memory_observations (
    id           SERIAL PRIMARY KEY,
    entity_id    INTEGER NOT NULL REFERENCES memory_entities(id) ON DELETE CASCADE,
    content      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- Relations ----
-- No project_id column: a relation's project is derived from its from/to entities.
-- All queries therefore JOIN memory_entities to scope by project.
CREATE TABLE IF NOT EXISTS memory_relations (
    id               SERIAL PRIMARY KEY,
    from_entity_id   INTEGER NOT NULL REFERENCES memory_entities(id) ON DELETE CASCADE,
    to_entity_id     INTEGER NOT NULL REFERENCES memory_entities(id) ON DELETE CASCADE,
    relation_type    TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (from_entity_id, to_entity_id, relation_type)
);

-- Full-text search across entity names + observation content
CREATE INDEX IF NOT EXISTS idx_entities_name_trgm
    ON memory_entities USING GIN (to_tsvector('simple', name));

CREATE INDEX IF NOT EXISTS idx_observations_content_fts
    ON memory_observations USING GIN (to_tsvector('simple', content));

CREATE INDEX IF NOT EXISTS idx_relations_from ON memory_relations (from_entity_id);
CREATE INDEX IF NOT EXISTS idx_relations_to ON memory_relations (to_entity_id);

-- Keep updated_at fresh
CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_touch_entities ON memory_entities;
CREATE TRIGGER trg_touch_entities
    BEFORE UPDATE ON memory_entities
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- ---- AI Memory Quality (Phase 1) ----
-- confidence: per-observation AI keyakinan (0.0-1.0, nullable NULL=netral).
--   Scoring treatas NULL as 1.0 via COALESCE.
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS confidence REAL;

-- last_accessed_at: per-entity decay tracking (nullable NULL=belum pernah di-access,
--   fallback ke created_at untuk hitung umur). Computed-on-read decay, no scheduler.
ALTER TABLE memory_entities ADD COLUMN IF NOT EXISTS last_accessed_at TIMESTAMPTZ;

-- ---- AI Memory Quality (Phase 2: pgvector semantic search) ----
-- Requires the pgvector extension (image: pgvector/pgvector:pg17).
CREATE EXTENSION IF NOT EXISTS vector;

-- embedding: per-observation semantic vector (nullable NULL=belum di-embed/backfill).
--   Semantic path ignores NULL rows; lexical path unaffected.
--   Dimension 1024 matches the default model bge-m3.
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS embedding vector(1024);

-- Resize existing embedding columns to 1024 (e.g. legacy installs created at
-- 768-dim for nomic-embed-text). Old vectors are model-specific and cannot be
-- converted, so they are NULLed first; the index is dropped because ALTER COLUMN
-- TYPE rebuilds it and the old index was dimension-specific. Re-embed afterward
-- with: mcp-memory-server -backfill-embeddings
DO $$
DECLARE cur_type text;
BEGIN
    SELECT format_type(a.atttypid, a.atttypmod) INTO cur_type
    FROM pg_attribute a
    WHERE a.attrelid = 'memory_observations'::regclass
      AND a.attname = 'embedding';
    IF cur_type IS NOT NULL AND cur_type <> 'vector(1024)' THEN
        DROP INDEX IF EXISTS idx_observations_embedding;
        UPDATE memory_observations SET embedding = NULL;
        ALTER TABLE memory_observations ALTER COLUMN embedding TYPE vector(1024);
    END IF;
END $$;

-- ivfflat approximate nearest-neighbor index for fast cosine search.
-- lists=100 suits small-to-medium datasets; re-build if rows > 100k.
CREATE INDEX IF NOT EXISTS idx_observations_embedding
    ON memory_observations USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- ---- Observation Versioning ----
-- Append-only audit trail. Populated by repo methods (application-level, in tx).
CREATE TABLE IF NOT EXISTS memory_history (
    id BIGSERIAL PRIMARY KEY,
    entity_id INT,
    entity_name TEXT NOT NULL,
    observation_id INT,
    action TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    confidence REAL,
    happened_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_history_entity ON memory_history (entity_name, happened_at DESC);
