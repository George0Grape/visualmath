package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Проверяем, что FTS5 доступен в modernc.org/sqlite и индекс находит модуль
// по тексту, спрятанному в JSON-содержимом (а не только по названию).
func TestSetupFTS_SearchesContent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fts.db")
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE modules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		course TEXT NOT NULL,
		description TEXT,
		content TEXT,
		author_id INTEGER
	)`); err != nil {
		t.Fatalf("create modules: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE lectures (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		course TEXT NOT NULL,
		description TEXT,
		author_id INTEGER,
		published INTEGER DEFAULT 1
	)`); err != nil {
		t.Fatalf("create lectures: %v", err)
	}

	// Слово "интеграл" есть только в content, не в title/description
	if _, err := db.Exec(`INSERT INTO modules (title, course, description, content, author_id)
		VALUES ('Тест по матану', 'Матанализ', 'проверочный', '{"text":"Вычислите интеграл функции"}', 7)`); err != nil {
		t.Fatalf("insert module: %v", err)
	}

	SetupFTS(db)

	var id, author int
	var title string
	err = db.QueryRow(`
		SELECT m.id, m.title, m.author_id
		FROM modules_fts JOIN modules m ON m.id = modules_fts.rowid
		WHERE modules_fts MATCH ? ORDER BY rank LIMIT 1`, `"интеграл"*`).Scan(&id, &title, &author)
	if err != nil {
		t.Fatalf("FTS поиск по содержимому не сработал: %v", err)
	}
	if id != 1 || author != 7 {
		t.Errorf("нашли не тот модуль: id=%d author=%d", id, author)
	}

	// Триггер на INSERT: новая запись должна сразу попасть в индекс
	if _, err := db.Exec(`INSERT INTO modules (title, course, description, content, author_id)
		VALUES ('Производные', 'Матанализ', '', '{"text":"дифференцирование сложной функции"}', 7)`); err != nil {
		t.Fatalf("insert module 2: %v", err)
	}
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM modules_fts WHERE modules_fts MATCH ?`, `"дифференц"*`).Scan(&cnt); err != nil {
		t.Fatalf("FTS после insert-триггера: %v", err)
	}
	if cnt != 1 {
		t.Errorf("триггер INSERT не обновил индекс: cnt=%d", cnt)
	}
}
