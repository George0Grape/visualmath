package handlers

import (
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// Recoverer ловит panic в любом хендлере, пишет метод/путь/req-id и полный стек
// в лог и отдаёт аккуратную страницу 500 (или JSON для /api/). Заменяет
// chi middleware.Recoverer, который отдаёт голый текст "Internal Server Error".
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// http.ErrAbortHandler — намеренный сигнал прервать обработку
			// (например, при разрыве соединения). Его глушить нельзя.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			reqID := middleware.GetReqID(r.Context())
			log.Printf("PANIC %s %s [req=%s]: %v\n%s", r.Method, r.URL.Path, reqID, rec, debug.Stack())
			ServerError(w, r)
		}()
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy: после самохостинга всех зависимостей (MathJax, Plotly,
// шрифты, PDF.js) внешних источников нет — всё ограничено 'self'. Послабления:
//
//	script-src 'unsafe-inline' — в шаблонах много inline-<script> и on*-обработчиков;
//	script-src 'unsafe-eval'   — MathJax/Plotly используют eval/Function;
//	img-src data: blob:        — инлайн-картинки и blob из PDF.js/Plotly;
//	worker-src blob:           — воркер PDF.js (и его blob-фолбэк);
//	connect-src ws: wss:       — WebSocket живых лекций (same-origin).
//
// frame-ancestors 'self' дублирует X-Frame-Options для современных браузеров.
const contentSecurityPolicy = "default-src 'self'; " +
	"base-uri 'self'; " +
	"object-src 'none'; " +
	"frame-ancestors 'self'; " +
	"form-action 'self'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
	"worker-src 'self' blob:; " +
	"connect-src 'self' ws: wss:"

// SecurityHeaders проставляет защитные HTTP-заголовки на все ответы.
// HSTS оставляем nginx (TLS терминируется там).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff") // запрет MIME-sniffing
		h.Set("X-Frame-Options", "SAMEORIGIN")     // анти-кликджекинг
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(self)")
		// Аварийный клапан: если CSP что-то ломает в проде, выставить CSP_DISABLE=1
		// в окружении и перезапустить — без пересборки.
		if os.Getenv("CSP_DISABLE") == "" {
			h.Set("Content-Security-Policy", contentSecurityPolicy)
		}
		next.ServeHTTP(w, r)
	})
}

// ServerError отдаёт ответ 500: JSON для API-маршрутов и HTML-страницу 500.html
// для обычных страниц. Используется и в Recoverer, и может вызываться вручную.
func ServerError(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false,"message":"Внутренняя ошибка сервера"}`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	// Если шаблон ещё не загружен или сам сломан — отдаём текстовый фолбэк,
	// чтобы рекурсивно не упасть внутри обработчика ошибки.
	if tmplCache == nil || tmplCache.ExecuteTemplate(w, "500.html", nil) != nil {
		w.Write([]byte("500 — внутренняя ошибка сервера"))
	}
}
