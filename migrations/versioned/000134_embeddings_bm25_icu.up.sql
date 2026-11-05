-- Migration 000134: rebuild the BM25 index on embeddings.content with icu.
--
-- 000002 tokenizes content with chinese_lindera, which emits whitespace and
-- punctuation as tokens for Vietnamese and uses a Chinese dictionary. icu
-- splits on Unicode word boundaries, keeps diacritics and drops punctuation;
-- it was chosen by keyword recall on the Vietnamese keyword set (ADR 0002).
-- Columns, key_field and the text_fields JSON form stay as in 000002, so
-- KeywordsRetrieve and the `|||` query are unchanged.
--
-- The rebuild covers every existing row and locks embeddings while it runs.
-- Deployments without the postgres retriever have no embeddings table
-- (app.skip_embedding=true in 000002) and skip this migration.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'embeddings') THEN
        RAISE NOTICE '[Migration 000134] embeddings table does not exist · skipping';
        RETURN;
    END IF;

    DROP INDEX IF EXISTS embeddings_search_idx;
    CREATE INDEX embeddings_search_idx ON embeddings
    USING bm25 (id, knowledge_base_id, content, knowledge_id, chunk_id)
    WITH (
        key_field = 'id',
        text_fields = '{
            "content": {
              "tokenizer": {"type": "icu"}
            }
        }'
    );
    RAISE NOTICE '[Migration 000134] Rebuilt embeddings_search_idx with the icu tokenizer';
END $$;
