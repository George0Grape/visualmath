package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/go-chi/chi/v5"

	"visualmath/internal/auth"
)

func setupSessionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE active_sessions (
			session_id TEXT PRIMARY KEY,
			teacher_id INTEGER,
			is_active INTEGER DEFAULT 1,
			ended_at DATETIME
		);
		CREATE TABLE session_students (
			session_id TEXT,
			is_active INTEGER DEFAULT 1,
			left_at DATETIME
		);
	`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// sessionReq строит запрос с chi-параметром id (строковый session_id) и claims.
func sessionReq(sessionID string, claims *auth.UserClaims) *http.Request {
	req := httptest.NewRequest("POST", "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", sessionID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if claims != nil {
		ctx = context.WithValue(ctx, auth.UserContextKey, *claims)
	}
	return req.WithContext(ctx)
}

// Владелец сессии завершает её — is_active становится 0.
func TestEndSession_OwnerEnds(t *testing.T) {
	db := setupSessionDB(t)
	db.Exec(`INSERT INTO active_sessions (session_id, teacher_id, is_active) VALUES ('s1', 10, 1)`)
	h := &SessionHandler{DB: db}

	w := httptest.NewRecorder()
	h.EndSession(w, sessionReq("s1", teacher(10)))

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var active int
	db.QueryRow(`SELECT is_active FROM active_sessions WHERE session_id='s1'`).Scan(&active)
	if active != 0 {
		t.Errorf("сессия должна быть завершена (is_active=0), got %d", active)
	}
}

// Чужой преподаватель не может завершить сессию (403), она остаётся активной.
func TestEndSession_NotOwnerForbidden(t *testing.T) {
	db := setupSessionDB(t)
	db.Exec(`INSERT INTO active_sessions (session_id, teacher_id, is_active) VALUES ('s1', 10, 1)`)
	h := &SessionHandler{DB: db}

	w := httptest.NewRecorder()
	h.EndSession(w, sessionReq("s1", teacher(999)))

	if w.Code != http.StatusForbidden {
		t.Fatalf("ожидали 403, got %d", w.Code)
	}
	var active int
	db.QueryRow(`SELECT is_active FROM active_sessions WHERE session_id='s1'`).Scan(&active)
	if active != 1 {
		t.Errorf("чужая сессия не должна завершаться")
	}
}

// Несуществующая сессия → 404.
func TestEndSession_NotFound(t *testing.T) {
	db := setupSessionDB(t)
	h := &SessionHandler{DB: db}

	w := httptest.NewRecorder()
	h.EndSession(w, sessionReq("nope", teacher(10)))

	if w.Code != http.StatusNotFound {
		t.Fatalf("ожидали 404, got %d", w.Code)
	}
}

// Без авторизации → 401.
func TestEndSession_Unauthorized(t *testing.T) {
	db := setupSessionDB(t)
	db.Exec(`INSERT INTO active_sessions (session_id, teacher_id, is_active) VALUES ('s1', 10, 1)`)
	h := &SessionHandler{DB: db}

	w := httptest.NewRecorder()
	h.EndSession(w, sessionReq("s1", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ожидали 401, got %d", w.Code)
	}
}
