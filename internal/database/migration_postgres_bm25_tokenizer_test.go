package database

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
)

// bm25TokenizerMigration rebuilds embeddings_search_idx with the icu tokenizer
// for Vietnamese; its down migration restores chinese_lindera from 000002.
const bm25TokenizerMigration = 134

// TestPostgresBM25TokenizerMigration runs the versioned migrations against a
// real ParadeDB; see TestPostgresMigrationsServeAgentHistory for how to start
// one. Both directions rebuild the index over a row that already exists, and
// each step checks the declared tokenizer and how the index actually split
// the row: icu keeps "11a" as one token, chinese_lindera splits it.
func TestPostgresBM25TokenizerMigration(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to run PostgreSQL migration tests")
	}
	chdirAndRestore(t, sqliteRepoRoot(t))
	require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}))

	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var id int
	require.NoError(t, db.QueryRow(`INSERT INTO embeddings (source_id, source_type, content, dimension)
		VALUES ('bm25-tokenizer-test', 0, 'Điều 11a khoản 1: điều kiện cấp Giấy phép theo 117/2026/VBHN-NĐ-NHNN.', 1024)
		RETURNING id`).Scan(&id))
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM embeddings WHERE id = $1", id) })

	requireBM25Tokenizer(t, db, id, "icu", true)

	m, err := migrate.New("file://migrations/versioned", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })

	require.NoError(t, m.Migrate(bm25TokenizerMigration-1))
	requireBM25Tokenizer(t, db, id, "chinese_lindera", false)

	require.NoError(t, m.Up())
	requireBM25Tokenizer(t, db, id, "icu", true)
}

func requireBM25Tokenizer(t *testing.T, db *sql.DB, id int, want string, wholeToken bool) {
	t.Helper()
	var version int
	var dirty bool
	require.NoError(t, db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	require.False(t, dirty)

	var raw string
	require.NoError(t, db.QueryRow(`SELECT option_value FROM pg_class c,
		pg_options_to_table(c.reloptions) WHERE c.relname = 'embeddings_search_idx'
		AND option_name = 'text_fields'`).Scan(&raw), "version %d has no BM25 index", version)
	var fields struct {
		Content struct {
			Tokenizer struct {
				Type string `json:"type"`
			} `json:"tokenizer"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &fields), raw)
	require.Equal(t, want, fields.Content.Tokenizer.Type, "version %d text_fields %s", version, raw)

	var indexed, whole int
	require.NoError(t, db.QueryRow(`SELECT
		(SELECT count(*) FROM embeddings WHERE id @@@ paradedb.all() AND id = $1),
		(SELECT count(*) FROM embeddings WHERE id @@@ paradedb.term('content', '11a') AND id = $1)`,
		id).Scan(&indexed, &whole))
	require.Equal(t, 1, indexed, "version %d: the rebuilt index must cover existing rows", version)
	require.Equal(t, wholeToken, whole == 1, "version %d: is \"11a\" one token under %s", version, want)
}
