-- Down migration: rebuild embeddings_search_idx with chinese_lindera, as 000002 created it.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'embeddings') THEN
        RAISE NOTICE '[Migration 000134 down] embeddings table does not exist · skipping';
        RETURN;
    END IF;

    DROP INDEX IF EXISTS embeddings_search_idx;
    CREATE INDEX embeddings_search_idx ON embeddings
    USING bm25 (id, knowledge_base_id, content, knowledge_id, chunk_id)
    WITH (
        key_field = 'id',
        text_fields = '{
            "content": {
              "tokenizer": {"type": "chinese_lindera"}
            }
        }'
    );
    RAISE NOTICE '[Migration 000134 down] Rebuilt embeddings_search_idx with the chinese_lindera tokenizer';
END $$;
