package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const testSecret = "test-secret-key-32-bytes-long!!"

// makeToken создаёт валидный JWT с заданными параметрами.
func makeToken(secret string, userID int, userType string, exp time.Time) string {
	claims := jwt.MapClaims{
		"user_id":   float64(userID),
		"login":     "testuser",
		"user_type": userType,
		"full_name": "Test User",
		"exp":       exp.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(secret))
	return s
}

// okHandler — заглушка, пишет 200 если middleware пропустил.
func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func TestAuthMiddleware_NoToken_APIRoute(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest("GET", "/api/profile", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token on /api/ → %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_NoToken_PageRoute(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("no token on /dashboard → %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("redirect location = %q, want /login", loc)
	}
}

func TestAuthMiddleware_ValidBearerToken(t *testing.T) {
	mw := AuthMiddleware(testSecret)

	var capturedClaims *UserClaims
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := GetUserFromContext(r.Context())
		if !ok {
			t.Error("claims not in context")
		}
		capturedClaims = c
		w.WriteHeader(http.StatusOK)
	}))

	token := makeToken(testSecret, 42, "teacher", time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid token → %d, want 200", w.Code)
	}
	if capturedClaims == nil || capturedClaims.UserID != 42 {
		t.Errorf("claims.UserID = %v, want 42", capturedClaims)
	}
	if capturedClaims.UserType != "teacher" {
		t.Errorf("claims.UserType = %q, want teacher", capturedClaims.UserType)
	}
}

func TestAuthMiddleware_ValidCookieToken(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	token := makeToken(testSecret, 7, "student", time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.AddCookie(&http.Cookie{
		Name:  "token",
		Value: url.QueryEscape(token),
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid cookie token → %d, want 200", w.Code)
	}
}

func TestAuthMiddleware_ValidQueryToken(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	token := makeToken(testSecret, 7, "student", time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/profile?token="+url.QueryEscape(token), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid query token → %d, want 200", w.Code)
	}
}

// ── Безопасность: атаки на JWT ────────────────────────────────────────────────

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	token := makeToken(testSecret, 1, "student", time.Now().Add(-time.Hour)) // истёк час назад
	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expired token should be rejected, got 200")
	}
}

func TestAuthMiddleware_WrongSecret(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	token := makeToken("wrong-secret-completely-different!", 1, "student", time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("token signed with wrong secret should be rejected")
	}
}

func TestAuthMiddleware_AlgorithmNoneAttack(t *testing.T) {
	// Атака: токен с alg=none — злоумышленник убирает подпись
	// Middleware должен отклонить, т.к. явно проверяет HMAC
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	claims := jwt.MapClaims{
		"user_id":   float64(1),
		"login":     "hacker",
		"user_type": "teacher",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	// jwt.UnsafeAllowNoneSignatureType позволяет создать alg=none токен
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	req := httptest.NewRequest("GET", "/api/modules", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("alg=none token must be rejected — algorithm confusion attack!")
	}
}

func TestAuthMiddleware_TamperedPayload(t *testing.T) {
	// Изменяем user_type в payload без переподписи
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	// создаём student-токен
	token := makeToken(testSecret, 5, "student", time.Now().Add(time.Hour))
	// обрезаем подпись и ставим чужую (имитация подбора)
	tampered := token[:len(token)-5] + "XXXXX"

	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("tampered token should be rejected")
	}
}

func TestAuthMiddleware_PublicRoutes(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	publicPaths := []string{
		"/",
		"/login",
		"/register",
		"/api/login",
		"/api/register",
		"/static/js/main.js",
		"/test",
	}

	for _, path := range publicPaths {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized || w.Code == http.StatusFound {
			t.Errorf("public path %q → %d (should not require auth)", path, w.Code)
		}
	}
}

func TestAuthMiddleware_EmptyBearerHeader(t *testing.T) {
	mw := AuthMiddleware(testSecret)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("empty Bearer token should be rejected")
	}
}

func TestAuthMiddleware_MalformedJWT(t *testing.T) {
	h := AuthMiddleware(testSecret)(http.HandlerFunc(okHandler))

	malformed := []string{
		"not.a.jwt",
		"eyJhbGciOiJIUzI1NiJ9",
		"aaa.bbb",
		"",
	}

	for _, tok := range malformed {
		req := httptest.NewRequest("GET", "/api/profile", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Errorf("malformed token %q should be rejected", tok)
		}
	}
}

func TestGetUserFromContext_Missing(t *testing.T) {
	ctx := context.Background()
	_, ok := GetUserFromContext(ctx)
	if ok {
		t.Error("GetUserFromContext on empty context should return ok=false")
	}
}

func TestGetUserFromContext_Present(t *testing.T) {
	expected := UserClaims{UserID: 99, Login: "alice", UserType: "teacher", FullName: "Alice"}
	ctx := context.WithValue(context.Background(), UserContextKey, expected)
	got, ok := GetUserFromContext(ctx)
	if !ok {
		t.Fatal("GetUserFromContext should return ok=true")
	}
	if got.UserID != expected.UserID || got.Login != expected.Login {
		t.Errorf("got %+v, want %+v", got, expected)
	}
}
