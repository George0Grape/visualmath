package handlers

import (
	"net/http"
	"strings"
)

// RateLimiter — псевдоним LoginLimiter. То же хранилище попыток по IP, но имя
// точнее отражает использование как общего ограничителя частоты (не только login).
type RateLimiter = LoginLimiter

// RateLimit — middleware, ограничивающее частоту запросов по IP. Каждый запрос
// учитывается как попытка; при исчерпании лимита IP получает 429 на время блока.
// Применяется к register / вводу join-кодов / постингу комментариев, где важно
// не дать перебор кодов или спам.
func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			if rl.IsBlocked(ip) {
				w.Header().Set("Retry-After", "600")
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusTooManyRequests)
					w.Write([]byte(`{"success":false,"message":"Слишком много запросов, попробуйте позже"}`))
				} else {
					http.Error(w, "Слишком много запросов, попробуйте позже", http.StatusTooManyRequests)
				}
				return
			}
			rl.RecordFailure(ip) // считаем каждый запрос; превышение порога → блок
			next.ServeHTTP(w, r)
		})
	}
}
