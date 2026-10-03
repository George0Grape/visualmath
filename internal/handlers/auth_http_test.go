package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			login TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			full_name TEXT NOT NULL,
			user_type TEXT DEFAULT 'student',
			group_number TEXT,
			university TEXT,
			email TEXT UNIQUE NOT NULL,
			last_profile_edit DATETIME,
			avatar_url TEXT DEFAULT '',
			about TEXT DEFAULT ''
		);
		CREATE TABLE email_verifications (
			email TEXT PRIMARY KEY,
			code TEXT NOT NULL,
			data_json TEXT NOT NULL,
			expires_at DATETIME NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertUser(t *testing.T, db *sql.DB, login, email, password, userType string) int64 {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	res, err := db.Exec(
		`INSERT INTO users (login, password_hash, full_name, user_type, email) VALUES (?,?,?,?,?)`,
		login, string(hash), "Test User", userType, email,
	)
	if err != nil {
		t.Fatalf("insertUser: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func postJSON(t *testing.T, handler http.Handler, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegister_MissingFields(t *testing.T) {
	resetLimiter()
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}

	cases := []struct {
		name string
		body map[string]string
	}{
		{"no login", map[string]string{"password": "pass123", "email": "a@b.com"}},
		{"no password", map[string]string{"login": "alice", "email": "a@b.com"}},
		{"no email", map[string]string{"login": "alice", "password": "pass123"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := postJSON(t, http.HandlerFunc(h.Register), "/api/register", c.body)
			if w.Code == http.StatusOK {
				t.Errorf("%s: expected non-200 for missing fields, got 200", c.name)
			}
		})
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	resetLimiter()
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}
	w := postJSON(t, http.HandlerFunc(h.Register), "/api/register", map[string]string{
		"login": "alice", "password": "ab", "email": "a@b.com",
	})
	// короткий пароль: код подтверждения генерируется, но пароль слишком мал —
	// bcrypt не проверяет длину, но мы проверяем при verify. Просто убеждаемся, что ответ не 500.
	if w.Code == http.StatusInternalServerError {
		t.Error("register with short password should not cause 500")
	}
}

func TestRegister_InvalidLogin_SQLInjection(t *testing.T) {
	resetLimiter()
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}
	w := postJSON(t, http.HandlerFunc(h.Register), "/api/register", map[string]string{
		"login": "'; DROP TABLE users;--", "password": "pass123", "email": "x@x.com",
	})
	if w.Code == http.StatusOK {
		t.Error("SQL injection in login should be rejected by validation")
	}

	// убеждаемся, что таблица users всё ещё существует
	var count int
	err := h.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Errorf("users table was dropped! SQL injection succeeded: %v", err)
	}
}

func TestRegister_InvalidLogin_XSS(t *testing.T) {
	resetLimiter()
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}
	w := postJSON(t, http.HandlerFunc(h.Register), "/api/register", map[string]string{
		"login": "<script>alert(1)</script>", "password": "pass123", "email": "x@x.com",
	})
	if w.Code == http.StatusOK {
		t.Error("XSS payload in login should be rejected by validation")
	}
}

func TestRegister_DuplicateLogin(t *testing.T) {
	resetLimiter()
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "pass123", "student")
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.Register), "/api/register", map[string]string{
		"login": "alice", "password": "pass123", "email": "other@example.com",
	})
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate login → %d, want 409", w.Code)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	resetLimiter()
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "pass123", "student")
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.Register), "/api/register", map[string]string{
		"login": "bob", "password": "pass123", "email": "alice@example.com",
	})
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate email → %d, want 409", w.Code)
	}
}

func TestRegister_RateLimit(t *testing.T) {
	resetLimiter()
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}

	for i := 0; i < regMaxAttempts; i++ {
		req := httptest.NewRequest("POST", "/api/register", bytes.NewReader([]byte(
			fmt.Sprintf(`{"login":"user%d","password":"pass123","email":"u%d@x.com"}`, i, i),
		)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "1.1.1.1:9999"
		w := httptest.NewRecorder()
		http.HandlerFunc(h.Register).ServeHTTP(w, req)
	}

	// 6-й запрос с того же IP должен получить 429
	req := httptest.NewRequest("POST", "/api/register", bytes.NewReader([]byte(
		`{"login":"userX","password":"pass123","email":"ux@x.com"}`,
	)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.1.1.1:9999"
	w := httptest.NewRecorder()
	http.HandlerFunc(h.Register).ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th register attempt → %d, want 429", w.Code)
	}
}

// ── VerifyEmail ───────────────────────────────────────────────────────────────

func TestVerifyEmail_WrongCode(t *testing.T) {
	db := setupTestDB(t)
	db.Exec(`INSERT INTO email_verifications VALUES ('u@x.com','123456','{}',?)`,
		time.Now().Add(15*time.Minute))
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.VerifyEmail), "/api/verify-email", map[string]string{
		"email": "u@x.com", "code": "999999",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("wrong code → %d, want 400", w.Code)
	}
}

func TestVerifyEmail_ExpiredCode(t *testing.T) {
	db := setupTestDB(t)
	db.Exec(`INSERT INTO email_verifications VALUES ('u@x.com','123456','{}',?)`,
		time.Now().Add(-time.Minute)) // уже истёк
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.VerifyEmail), "/api/verify-email", map[string]string{
		"email": "u@x.com", "code": "123456",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expired code → %d, want 400", w.Code)
	}
}

func TestVerifyEmail_NoRecord(t *testing.T) {
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}
	w := postJSON(t, http.HandlerFunc(h.VerifyEmail), "/api/verify-email", map[string]string{
		"email": "ghost@x.com", "code": "000000",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown email → %d, want 400", w.Code)
	}
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestLogin_CorrectCredentials(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "correctpass", "student")
	h := &AuthHandler{DB: db, JWTSecret: "test-secret"}

	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "alice", "password": "correctpass",
	})
	if w.Code != http.StatusOK {
		t.Errorf("correct login → %d, want 200. body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] == nil {
		t.Error("response should contain token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "correctpass", "student")
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "alice", "password": "wrongpass",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong password → %d, want 401", w.Code)
	}
}

func TestLogin_NonExistentUser(t *testing.T) {
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}
	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "nobody", "password": "pass",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("non-existent user → %d, want 401", w.Code)
	}
}

func TestLogin_ByEmail(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "pass123", "student")
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "alice@example.com", "password": "pass123",
	})
	if w.Code != http.StatusOK {
		t.Errorf("login by email → %d, want 200", w.Code)
	}
}

func TestLogin_SQLInjection(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "admin", "admin@example.com", "secret", "teacher")
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	// классическая инъекция: ' OR '1'='1
	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "' OR '1'='1", "password": "anything",
	})
	if w.Code == http.StatusOK {
		t.Error("SQL injection in login field should not succeed — parameterized queries must be used")
	}
}

func TestLogin_EmptyFields(t *testing.T) {
	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret"}
	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "", "password": "",
	})
	if w.Code == http.StatusOK {
		t.Error("empty credentials should not succeed")
	}
}

func TestLogin_ResponseDoesNotLeakHash(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "pass123", "student")
	h := &AuthHandler{DB: db, JWTSecret: "secret"}

	w := postJSON(t, http.HandlerFunc(h.Login), "/api/login", map[string]string{
		"login": "alice", "password": "pass123",
	})
	body := w.Body.String()
	if contains(body, "$2a$") || contains(body, "password_hash") {
		t.Error("login response must not contain password hash")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}
func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
