package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
)

// tmplCache — все шаблоны в памяти, парсятся один раз при старте
var tmplCache *template.Template

// InitTemplates парсит все шаблоны при старте.
// basePath — директория рядом с web/templates/ (обычно папка с бинарником).
func InitTemplates(basePath string) error {
	glob := filepath.Join(basePath, "web", "templates", "*.html")
	bannerPalette := [][2]string{
		{"#1a73e8", "#0d47a1"}, {"#188038", "#1b5e20"}, {"#b5152b", "#7f0000"},
		{"#e37400", "#bf360c"}, {"#7b1fa2", "#4a148c"}, {"#00838f", "#004d40"},
		{"#4a148c", "#1a237e"}, {"#558b2f", "#33691e"},
	}
	funcs := template.FuncMap{
		"mod": func(a, b int) int { return a % b },
		"not": func(b bool) bool { return !b },
		"firstChar": func(s string) string {
			s = strings.TrimSpace(s)
			if len(s) == 0 {
				return "?"
			}
			for _, r := range s {
				return strings.ToUpper(string(r))
			}
			return "?"
		},
		"bannerColor": func(id int) string {
			return bannerPalette[id%len(bannerPalette)][0]
		},
		"bannerColor2": func(id int) string {
			return bannerPalette[id%len(bannerPalette)][1]
		},
	}
	t, err := template.New("").Funcs(funcs).ParseGlob(glob)
	if err != nil {
		return err
	}
	tmplCache = t
	return nil
}

func serveTemplate(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplCache.ExecuteTemplate(w, name, nil); err != nil {
		http.Error(w, "Ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
	}
}

func execTemplate(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplCache.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
	}
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

type PageHandler struct{}

func (h *PageHandler) Home(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "home.html")
}

func (h *PageHandler) Login(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("token")
	if err == nil && cookie.Value != "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	serveTemplate(w, "login.html")
}

func (h *PageHandler) Register(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "register.html")
}

// простая страница чтобы убедиться, что сервер живой
func (h *PageHandler) Test(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html><html><head><title>Тест</title></head>
<body style="padding:50px;text-align:center;">
<h1 style="color:green;">✅ Тест пройден!</h1>
<p>Сервер работает правильно</p>
<p><a href="/">Вернуться на главную</a></p>
</body></html>`))
}

func (h *PageHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := tmplCache.ExecuteTemplate(w, "404.html", nil); err != nil {
		http.Error(w, "404 — Страница не найдена", http.StatusNotFound)
	}
}

func (h *PageHandler) Graph3D(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "graph3d.html")
}

func (h *PageHandler) Logout(w http.ResponseWriter, r *http.Request) {
	domain := cookieDomain()
	for _, name := range []string{"user_type", "user_id", "full_name", "token"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Domain:   domain,
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	serveTemplate(w, "logout.html")
}
