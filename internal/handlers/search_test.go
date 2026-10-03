package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"visualmath/internal/auth"
	"visualmath/internal/storage"
)

// setupSearchDB создаёт минимальную схему (modules + lectures) с колонками,
// которые читают SearchHandler и FTS-индексы. withFTS=false имитирует окружение
// без FTS5 — для проверки отката на LIKE.
func setupSearchDB(t *testing.T, withFTS bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE modules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			course TEXT NOT NULL DEFAULT '',
			description TEXT,
			content TEXT,
			author_id INTEGER,
			published INTEGER DEFAULT 1
		);
		CREATE TABLE lectures (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			course TEXT NOT NULL DEFAULT '',
			description TEXT,
			author_id INTEGER,
			published INTEGER DEFAULT 1
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	if withFTS {
		storage.SetupFTS(db)
	}
	return db
}

func addModule(t *testing.T, db *sql.DB, title, desc, content string, author int, published int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO modules (title, course, description, content, author_id, published)
		VALUES (?, 'Матанализ', ?, ?, ?, ?)`, title, desc, content, author, published)
	if err != nil {
		t.Fatalf("addModule: %v", err)
	}
}

func addLecture(t *testing.T, db *sql.DB, title, desc string, author int, published int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO lectures (title, course, description, author_id, published)
		VALUES (?, 'Матанализ', ?, ?, ?)`, title, desc, author, published)
	if err != nil {
		t.Fatalf("addLecture: %v", err)
	}
}

// doSearch вызывает SearchHandler с заданным q и claims, возвращает результаты и код.
func doSearch(t *testing.T, db *sql.DB, q string, claims *auth.UserClaims) ([]SearchResult, int) {
	t.Helper()
	h := &SearchHandler{DB: db}
	req := httptest.NewRequest("GET", "/api/search?q="+q, nil)
	if claims != nil {
		// middleware кладёт в контекст значение (не указатель) — повторяем это
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, *claims))
	}
	w := httptest.NewRecorder()
	h.Search(w, req)

	var results []SearchResult
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
			t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
		}
	}
	return results, w.Code
}

func teacher(id int) *auth.UserClaims { return &auth.UserClaims{UserID: id, UserType: "teacher"} }
func student(id int) *auth.UserClaims { return &auth.UserClaims{UserID: id, UserType: "student"} }

func hasResult(results []SearchResult, typ string, id int) bool {
	for _, r := range results {
		if r.Type == typ && r.ID == id {
			return true
		}
	}
	return false
}

// FTS должен находить модуль по слову, спрятанному в content (а не в title/desc),
// и только среди модулей текущего преподавателя.
func TestSearch_TeacherFindsModuleByContent(t *testing.T) {
	db := setupSearchDB(t, true)
	addModule(t, db, "Тест по матану", "проверочный", `{"text":"Вычислите интеграл функции"}`, 10, 1)
	addModule(t, db, "Чужой интеграл", "у другого автора", `{"text":"что-то"}`, 20, 1)

	results, code := doSearch(t, db, "интеграл", teacher(10))
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !hasResult(results, "module", 1) {
		t.Errorf("ожидали найти модуль #1 по содержимому, got %+v", results)
	}
	if hasResult(results, "module", 2) {
		t.Errorf("модуль #2 чужого автора не должен попадать в выдачу")
	}
	for _, r := range results {
		if r.Type == "module" && r.ID == 1 && r.URL != "/modules/view/1" {
			t.Errorf("неверный URL: %q", r.URL)
		}
	}
}

// Студент видит только опубликованные лекции и НЕ видит модули.
func TestSearch_StudentSeesPublishedLecturesOnly(t *testing.T) {
	db := setupSearchDB(t, true)
	addLecture(t, db, "Линейная алгебра", "вектора", 10, 1)  // id 1, published
	addLecture(t, db, "Линейная секретная", "draft", 10, 0)  // id 2, unpublished
	addModule(t, db, "Линейная разминка", "", `{}`, 10, 1)   // модуль — студенту не показываем

	results, code := doSearch(t, db, "Линейная", student(99))
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !hasResult(results, "lecture", 1) {
		t.Errorf("ожидали опубликованную лекцию #1, got %+v", results)
	}
	if hasResult(results, "lecture", 2) {
		t.Errorf("неопубликованная лекция #2 не должна показываться студенту")
	}
	if hasResult(results, "module", 1) {
		t.Errorf("модули не должны показываться студенту")
	}
}

// Без FTS-таблиц (старое окружение) поиск должен молча откатиться на LIKE
// и всё равно найти совпадение в содержимом модуля.
func TestSearch_LikeFallbackWithoutFTS(t *testing.T) {
	db := setupSearchDB(t, false) // FTS-таблицы НЕ создаём
	addModule(t, db, "Безымянный", "пусто", `{"text":"теорема Лагранжа о среднем"}`, 10, 1)

	results, code := doSearch(t, db, "Лагранж", teacher(10))
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !hasResult(results, "module", 1) {
		t.Errorf("LIKE-fallback должен найти модуль по содержимому, got %+v", results)
	}
}

// Короткий запрос (<2 символов) возвращает пустой массив и 200.
func TestSearch_ShortQueryReturnsEmpty(t *testing.T) {
	db := setupSearchDB(t, true)
	addLecture(t, db, "Алгебра", "", 10, 1)

	results, code := doSearch(t, db, "а", student(1))
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if len(results) != 0 {
		t.Errorf("ожидали пустую выдачу для короткого запроса, got %+v", results)
	}
}

// Без авторизации — 401.
func TestSearch_Unauthorized(t *testing.T) {
	db := setupSearchDB(t, true)
	_, code := doSearch(t, db, "интеграл", nil)
	if code != http.StatusUnauthorized {
		t.Errorf("ожидали 401 без claims, got %d", code)
	}
}

// ftsMatchQuery строит безопасный префиксный MATCH и обезвреживает кавычки.
func TestFtsMatchQuery(t *testing.T) {
	cases := map[string]string{
		"лин алг":   `"лин"* "алг"*`,
		"  один  ":  `"один"*`,
		`a"b`:       `"ab"*`, // кавычка внутри терма удаляется
		"":          "",
	}
	for in, want := range cases {
		if got := ftsMatchQuery(in); got != want {
			t.Errorf("ftsMatchQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
