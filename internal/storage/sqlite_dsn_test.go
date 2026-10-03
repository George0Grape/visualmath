package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Проверяем, что продовый DSN целиком (включая _txlock=immediate и все _pragma)
// принимается драйвером modernc и реально применяет WAL + foreign_keys.
func TestSqliteDSN_OpensAndAppliesPragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping (DSN отвергнут драйвером?): %v", err)
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	// Базовая транзакция записи должна проходить (косвенно проверяет _txlock=immediate).
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY); INSERT INTO t (id) VALUES (1)`); err != nil {
		t.Fatalf("write tx: %v", err)
	}
}
