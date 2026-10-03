package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── LoginLimiter ─────────────────────────────────────────────────────────────

func newTestLimiter() *LoginLimiter {
	// через конструктор — чтобы поля порога (maxAttempts/window/lockDuration)
	// заполнились значениями login-лимитера (5 / 15 мин / 15 мин).
	return NewLoginLimiter()
}

func TestLoginLimiter_NotBlockedInitially(t *testing.T) {
	l := newTestLimiter()
	if l.IsBlocked("1.2.3.4") {
		t.Error("new IP should not be blocked initially")
	}
}

func TestLoginLimiter_BlocksAfterMaxAttempts(t *testing.T) {
	l := newTestLimiter()
	ip := "1.2.3.4"
	for i := 0; i < maxFailedAttempts-1; i++ {
		blocked := l.RecordFailure(ip)
		if blocked {
			t.Errorf("should not be blocked after attempt %d of %d", i+1, maxFailedAttempts)
		}
		if l.IsBlocked(ip) {
			t.Errorf("IsBlocked should be false after attempt %d", i+1)
		}
	}
	// 5-я попытка должна заблокировать
	blocked := l.RecordFailure(ip)
	if !blocked {
		t.Error("5th failure should return blocked=true")
	}
	if !l.IsBlocked(ip) {
		t.Error("IsBlocked should return true after max attempts")
	}
}

func TestLoginLimiter_RecordSuccessResetsCounter(t *testing.T) {
	l := newTestLimiter()
	ip := "5.5.5.5"

	for i := 0; i < maxFailedAttempts-1; i++ {
		l.RecordFailure(ip)
	}
	l.RecordSuccess(ip)

	// после сброса — снова можно пробовать
	if l.IsBlocked(ip) {
		t.Error("after RecordSuccess IP should not be blocked")
	}
	// можно сделать ещё maxFailedAttempts-1 попыток без блокировки
	for i := 0; i < maxFailedAttempts-1; i++ {
		if l.RecordFailure(ip) {
			t.Errorf("should not block again after reset, attempt %d", i+1)
		}
	}
}

func TestLoginLimiter_DifferentIPsIndependent(t *testing.T) {
	l := newTestLimiter()
	ip1, ip2 := "10.0.0.1", "10.0.0.2"

	for i := 0; i < maxFailedAttempts; i++ {
		l.RecordFailure(ip1)
	}

	if !l.IsBlocked(ip1) {
		t.Error("ip1 should be blocked")
	}
	if l.IsBlocked(ip2) {
		t.Error("ip2 should not be affected by ip1 failures")
	}
}

func TestLoginLimiter_ReturnsTrueOnExactlyMaxAttempts(t *testing.T) {
	l := newTestLimiter()
	ip := "9.9.9.9"

	var lastResult bool
	for i := 0; i < maxFailedAttempts; i++ {
		lastResult = l.RecordFailure(ip)
	}
	if !lastResult {
		t.Error("RecordFailure must return true on the attempt that causes block")
	}
}

func TestLoginLimiter_BlockedIPStaysBlocked(t *testing.T) {
	l := newTestLimiter()
	ip := "2.2.2.2"

	for i := 0; i < maxFailedAttempts; i++ {
		l.RecordFailure(ip)
	}
	// дополнительные попытки — IP всё ещё заблокирован
	for i := 0; i < 5; i++ {
		l.RecordFailure(ip)
		if !l.IsBlocked(ip) {
			t.Errorf("blocked IP should remain blocked on extra attempt %d", i+1)
		}
	}
}

func TestLoginLimiter_Concurrent(t *testing.T) {
	l := newTestLimiter()
	ip := "3.3.3.3"

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.RecordFailure(ip)
		}()
	}
	wg.Wait()

	// после 20 конкурентных попыток IP должен быть заблокирован
	if !l.IsBlocked(ip) {
		t.Error("IP must be blocked after concurrent failures")
	}
}

// ─── realIP ──────────────────────────────────────────────────────────────────

func TestRealIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.1:8080"
	got := realIP(r)
	if got != "192.168.1.1" {
		t.Errorf("realIP(RemoteAddr) = %q, want %q", got, "192.168.1.1")
	}
}

func TestRealIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "203.0.113.5")
	got := realIP(r)
	if got != "203.0.113.5" {
		t.Errorf("realIP(X-Real-IP) = %q, want %q", got, "203.0.113.5")
	}
}

func TestRealIP_XForwardedFor_Single(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.10")
	got := realIP(r)
	if got != "198.51.100.10" {
		t.Errorf("realIP(X-Forwarded-For single) = %q, want %q", got, "198.51.100.10")
	}
}

func TestRealIP_XForwardedFor_Chain(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.2, 10.0.0.3")
	got := realIP(r)
	if got != "203.0.113.1" {
		t.Errorf("realIP(X-Forwarded-For chain) = %q, want %q (first in chain)", got, "203.0.113.1")
	}
}

func TestRealIP_XRealIP_TakesPriorityOverXForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "1.1.1.1")
	r.Header.Set("X-Forwarded-For", "2.2.2.2")
	got := realIP(r)
	if got != "1.1.1.1" {
		t.Errorf("X-Real-IP should take priority, got %q", got)
	}
}

// ─── LoginLimiter интеграция с Login-хендлером ──────────────────────────────

func TestLogin_RateLimitBlocks(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "correctpass", "student")

	l := newTestLimiter()
	h := &AuthHandler{DB: db, JWTSecret: "secret", Limiter: l}

	// 5 неудачных попыток с одного IP
	for i := 0; i < maxFailedAttempts; i++ {
		req := newLoginRequest("wrongpass")
		req.RemoteAddr = "7.7.7.7:9999"
		w := httptest.NewRecorder()
		http.HandlerFunc(h.Login).ServeHTTP(w, req)
	}

	// следующий запрос должен вернуть 429
	req := newLoginRequest("wrongpass")
	req.RemoteAddr = "7.7.7.7:9999"
	w := httptest.NewRecorder()
	http.HandlerFunc(h.Login).ServeHTTP(w, req)
	if w.Code != 429 {
		t.Errorf("after %d failures → %d, want 429", maxFailedAttempts, w.Code)
	}
	if w.Header().Get("Retry-After") != "900" {
		t.Errorf("Retry-After header missing or wrong: %q", w.Header().Get("Retry-After"))
	}
}

func TestLogin_SuccessResetsLimiter(t *testing.T) {
	db := setupTestDB(t)
	insertUser(t, db, "alice", "alice@example.com", "correctpass", "student")

	l := newTestLimiter()
	h := &AuthHandler{DB: db, JWTSecret: "secret", Limiter: l}

	// 4 неудачные попытки
	for i := 0; i < maxFailedAttempts-1; i++ {
		req := newLoginRequest("wrongpass")
		req.RemoteAddr = "8.8.8.8:9999"
		w := httptest.NewRecorder()
		http.HandlerFunc(h.Login).ServeHTTP(w, req)
	}

	// успешный вход — сбрасывает счётчик
	req := newLoginRequest("correctpass")
	req.RemoteAddr = "8.8.8.8:9999"
	w := httptest.NewRecorder()
	http.HandlerFunc(h.Login).ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("correct login → %d, want 200", w.Code)
	}

	// теперь ещё 4 неудачных — не должно блокировать
	for i := 0; i < maxFailedAttempts-1; i++ {
		req = newLoginRequest("wrongpass")
		req.RemoteAddr = "8.8.8.8:9999"
		w = httptest.NewRecorder()
		http.HandlerFunc(h.Login).ServeHTTP(w, req)
		if w.Code == 429 {
			t.Errorf("should not be blocked after reset, attempt %d", i+1)
		}
	}
}

func TestLogin_BlockedIPGets429Immediately(t *testing.T) {
	l := newTestLimiter()
	// вручную блокируем IP
	l.mu.Lock()
	l.entries["6.6.6.6"] = &ipEntry{
		failures:    maxFailedAttempts,
		lockedUntil: time.Now().Add(lockDuration),
	}
	l.mu.Unlock()

	h := &AuthHandler{DB: setupTestDB(t), JWTSecret: "secret", Limiter: l}
	req := newLoginRequest("anypass")
	req.RemoteAddr = "6.6.6.6:1234"
	w := httptest.NewRecorder()
	http.HandlerFunc(h.Login).ServeHTTP(w, req)
	if w.Code != 429 {
		t.Errorf("pre-blocked IP → %d, want 429", w.Code)
	}
}

// helper: создаёт POST /api/login запрос с заданным паролем для пользователя alice
func newLoginRequest(password string) *http.Request {
	body := `{"login":"alice","password":"` + password + `"}`
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
