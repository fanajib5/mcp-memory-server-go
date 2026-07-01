-- MCP Memory Server schema
-- Knowledge graph: entities + observations + relations

CREATE TABLE IF NOT EXISTS memory_entities (
    id           SERIAL PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    entity_type  TEXT NOT NULL DEFAULT 'concept',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    project_id   TEXT NOT NULL DEFAULT 'default'
);

CREATE TABLE IF NOT EXISTS memory_observations (
    id           SERIAL PRIMARY KEY,
    entity_id    INTEGER NOT NULL REFERENCES memory_entities(id) ON DELETE CASCADE,
    content      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

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
