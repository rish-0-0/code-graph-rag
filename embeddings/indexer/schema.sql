CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS code_embeddings (
    node_id   text PRIMARY KEY,
    kind      text NOT NULL,
    module    text,
    pkg       text,
    name      text,
    text      text NOT NULL,
    pos_file  text,
    pos_line  int,
    embedding vector(768),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- HNSW for cosine distance — good recall at low latency for our scale.
CREATE INDEX IF NOT EXISTS code_embeddings_hnsw
    ON code_embeddings USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS code_embeddings_pkg_idx ON code_embeddings (pkg);
CREATE INDEX IF NOT EXISTS code_embeddings_kind_idx ON code_embeddings (kind);
