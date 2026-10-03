package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"visualmath/internal/auth"
)

type SearchHandler struct {
	DB *sql.DB
}

type SearchResult struct {
	Type  string `json:"type"`
	ID    int    `json:"id"`
	Title string `json:"title"`
	Sub   string `json:"sub"`
	URL   string `json:"url"`
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// Считаем символы, а не байты: одна кириллическая буква — 2 байта,
	// иначе поиск срабатывал бы уже на одном символе.
	if utf8.RuneCountInString(q) < 2 {
		respondJSON(w, []SearchResult{})
		return
	}

	var results []SearchResult

	// Модули: только для преподавателя (свои). FTS ищет в т.ч. по содержимому
	// (текст/вопросы), а не только по названию/описанию.
	if claims.UserType == "teacher" {
		results = append(results, h.searchModules(q, claims.UserID)...)
	}

	// Лекции: для препода — свои, для студента — опубликованные
	results = append(results, h.searchLectures(q, claims)...)

	if results == nil {
		results = []SearchResult{}
	}
	respondJSON(w, results)
}

func (h *SearchHandler) searchModules(q string, authorID int) []SearchResult {
	match := ftsMatchQuery(q)
	if match != "" {
		rows, err := h.DB.Query(`
			SELECT m.id, m.title, COALESCE(m.description,'')
			FROM modules_fts JOIN modules m ON m.id = modules_fts.rowid
			WHERE modules_fts MATCH ? AND m.author_id = ?
			ORDER BY rank LIMIT 5`, match, authorID)
		if err == nil {
			return scanResults(rows, "module", "/modules/view/")
		}
		// err → FTS-таблицы нет или запрос невалиден: тихо падаем на LIKE
	}
	like := "%" + q + "%"
	rows, err := h.DB.Query(`
		SELECT id, title, COALESCE(description,'')
		FROM modules
		WHERE author_id=? AND (title LIKE ? OR description LIKE ? OR content LIKE ?)
		LIMIT 5`, authorID, like, like, like)
	if err != nil {
		return nil
	}
	return scanResults(rows, "module", "/modules/view/")
}

func (h *SearchHandler) searchLectures(q string, claims *auth.UserClaims) []SearchResult {
	teacher := claims.UserType == "teacher"
	match := ftsMatchQuery(q)
	if match != "" {
		var rows *sql.Rows
		var err error
		if teacher {
			rows, err = h.DB.Query(`
				SELECT l.id, l.title, COALESCE(l.description,'')
				FROM lectures_fts JOIN lectures l ON l.id = lectures_fts.rowid
				WHERE lectures_fts MATCH ? AND l.author_id = ?
				ORDER BY rank LIMIT 5`, match, claims.UserID)
		} else {
			rows, err = h.DB.Query(`
				SELECT l.id, l.title, COALESCE(l.description,'')
				FROM lectures_fts JOIN lectures l ON l.id = lectures_fts.rowid
				WHERE lectures_fts MATCH ? AND l.published = 1
				ORDER BY rank LIMIT 5`, match)
		}
		if err == nil {
			return scanResults(rows, "lecture", "/lectures/view/")
		}
	}
	like := "%" + q + "%"
	var rows *sql.Rows
	var err error
	if teacher {
		rows, err = h.DB.Query(`SELECT id, title, COALESCE(description,'') FROM lectures
			WHERE author_id=? AND (title LIKE ? OR description LIKE ?) LIMIT 5`, claims.UserID, like, like)
	} else {
		rows, err = h.DB.Query(`SELECT id, title, COALESCE(description,'') FROM lectures
			WHERE published=1 AND (title LIKE ? OR description LIKE ?) LIMIT 5`, like, like)
	}
	if err != nil {
		return nil
	}
	return scanResults(rows, "lecture", "/lectures/view/")
}

func scanResults(rows *sql.Rows, typ, urlPrefix string) []SearchResult {
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var res SearchResult
		var desc string
		if rows.Scan(&res.ID, &res.Title, &desc) != nil {
			continue
		}
		res.Type = typ
		res.Sub = truncate(desc, 60)
		res.URL = urlPrefix + itoa(res.ID)
		out = append(out, res)
	}
	return out
}

// ftsMatchQuery превращает пользовательский ввод в безопасный FTS5 MATCH-запрос
// с префиксным поиском: «лин алг» → `"лин"* "алг"*` (все термы обязательны, AND).
// Кавычки внутри терма удаляем, чтобы не сломать синтаксис и не дать инъекций в MATCH.
func ftsMatchQuery(q string) string {
	var terms []string
	for _, t := range strings.Fields(q) {
		t = strings.ReplaceAll(t, `"`, "")
		if t == "" {
			continue
		}
		terms = append(terms, `"`+t+`"*`)
	}
	return strings.Join(terms, " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func itoa(n int) string { return strconv.Itoa(n) }
