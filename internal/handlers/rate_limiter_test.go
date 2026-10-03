package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// окно/блок большие, чтобы внутри теста счётчик не «протух» сам по себе
func newReq(ip string) *http.Request {
	r := httptest.NewRequest("POST", "/api/register", nil)
	r.RemoteAddr = ip + ":12345"
	return r
}

func TestRateLimit_BlocksAfterThreshold(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute, time.Minute)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := RateLimit(rl)(ok)

	// первые 3 запроса проходят (учёт идёт ПОСЛЕ проверки блокировки)
	for i := 1; i <= 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, newReq("9.9.9.9"))
		if w.Code != 200 {
			t.Fatalf("запрос %d должен пройти, получили %d", i, w.Code)
		}
	}
	// 4-й — заблокирован
	w := httptest.NewRecorder()
	h.ServeHTTP(w, newReq("9.9.9.9"))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("4-й запрос → %d, ожидали 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("на 429 должен быть заголовок Retry-After")
	}
}

func TestRateLimit_DifferentIPsIndependent(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute, time.Minute)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := RateLimit(rl)(ok)

	for i := 0; i < 3; i++ { // исчерпываем лимит для первого IP
		h.ServeHTTP(httptest.NewRecorder(), newReq("1.1.1.1"))
	}
	// другой IP не должен быть затронут
	w := httptest.NewRecorder()
	h.ServeHTTP(w, newReq("2.2.2.2"))
	if w.Code != 200 {
		t.Errorf("независимый IP → %d, ожидали 200", w.Code)
	}
}
