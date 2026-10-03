package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"visualmath/internal/auth"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

type SocialAuthHandler struct {
	DB        *sql.DB
	JWTSecret string
	Providers map[string]auth.OAuthProvider
}

func NewSocialAuthHandler(db *sql.DB, jwtSecret string) *SocialAuthHandler {
	return &SocialAuthHandler{
		DB:        db,
		JWTSecret: jwtSecret,
		Providers: make(map[string]auth.OAuthProvider),
	}
}

func (h *SocialAuthHandler) Add(name string, p auth.OAuthProvider) {
	h.Providers[name] = p
}

// providerFromPath вытаскивает имя провайдера из пути /auth/{name} или /auth/{name}/callback
func (h *SocialAuthHandler) providerFromPath(path string) (string, auth.OAuthProvider) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// parts: ["auth", "vk"] или ["auth", "vk", "callback"]
	if len(parts) < 2 {
		return "", nil
	}
	name := parts[1]
	p, ok := h.Providers[name]
	if !ok {
		return "", nil
	}
	return name, p
}

// Redirect — начало OAuth-потока, редиректит на страницу провайдера
func (h *SocialAuthHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	_, provider := h.providerFromPath(r.URL.Path)
	if provider == nil {
		http.Error(w, "Неизвестный провайдер", http.StatusNotFound)
		return
	}

	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   cookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, provider.GetAuthURL(state), http.StatusFound)
}

// Callback — обрабатывает возврат от провайдера с кодом авторизации
func (h *SocialAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	providerName, provider := h.providerFromPath(r.URL.Path)
	if provider == nil {
		http.Error(w, "Неизвестный провайдер", http.StatusNotFound)
		return
	}

	// Проверяем CSRF state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Redirect(w, r, "/login?error=invalid_state", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/login?error=no_code", http.StatusFound)
		return
	}

	userInfo, err := provider.ExchangeCode(code)
	if err != nil {
		http.Redirect(w, r, "/login?error=oauth_failed", http.StatusFound)
		return
	}

	user, err := h.findOrCreate(providerName, userInfo)
	if err != nil {
		http.Redirect(w, r, "/login?error=account_error", http.StatusFound)
		return
	}

	tokenString := h.makeJWT(user)
	h.setCookies(w, user, tokenString)

	// Рендерим страницу-переходник: ставим токен в localStorage (как при обычном входе),
	// потом редиректим на дашборд
	tokenJSON, _ := json.Marshal(tokenString)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body><script>
try{localStorage.setItem('token',%s);}catch(e){}
window.location.replace('/dashboard');
</script></body></html>`, tokenJSON)
}

type socialUser struct {
	ID       int
	Login    string
	FullName string
	UserType string
	Email    string
}

func (h *SocialAuthHandler) findOrCreate(providerName string, info *auth.OAuthUserInfo) (*socialUser, error) {
	// 1. Ищем существующую OAuth-связку
	var userID int
	err := h.DB.QueryRow(
		`SELECT user_id FROM oauth_connections WHERE provider=? AND provider_user_id=?`,
		providerName, info.ProviderID,
	).Scan(&userID)
	if err == nil {
		var u socialUser
		if e := h.DB.QueryRow(
			`SELECT id, login, full_name, user_type, email FROM users WHERE id=?`, userID,
		).Scan(&u.ID, &u.Login, &u.FullName, &u.UserType, &u.Email); e != nil {
			return nil, e
		}
		return &u, nil
	}

	// 2. Ищем пользователя по email и привязываем OAuth
	if info.Email != "" {
		var u socialUser
		err = h.DB.QueryRow(
			`SELECT id, login, full_name, user_type, email FROM users WHERE email=?`, info.Email,
		).Scan(&u.ID, &u.Login, &u.FullName, &u.UserType, &u.Email)
		if err == nil {
			h.DB.Exec(
				`INSERT OR IGNORE INTO oauth_connections (user_id, provider, provider_user_id, email, full_name, avatar_url) VALUES (?,?,?,?,?,?)`,
				u.ID, providerName, info.ProviderID, info.Email, info.FullName, info.AvatarURL,
			)
			return &u, nil
		}
	}

	// 3. Создаём нового пользователя
	login := info.Login
	base := login
	for i := 2; ; i++ {
		var cnt int
		h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE login=?`, login).Scan(&cnt)
		if cnt == 0 {
			break
		}
		login = fmt.Sprintf("%s_%d", base, i)
	}

	email := info.Email
	if email == "" {
		email = login + "@oauth.noreply"
	}
	fullName := info.FullName
	if fullName == "" {
		fullName = login
	}

	// Случайный хэш — войти по паролю не получится
	rb := make([]byte, 16)
	rand.Read(rb)
	hash, _ := bcrypt.GenerateFromPassword(rb, bcrypt.DefaultCost)

	res, err := h.DB.Exec(
		`INSERT INTO users (login, password_hash, full_name, user_type, email, email_verified) VALUES (?,?,?,'student',?,1)`,
		login, string(hash), fullName, email,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	newID, _ := res.LastInsertId()
	h.DB.Exec(
		`INSERT OR IGNORE INTO oauth_connections (user_id, provider, provider_user_id, email, full_name, avatar_url) VALUES (?,?,?,?,?,?)`,
		newID, providerName, info.ProviderID, info.Email, info.FullName, info.AvatarURL,
	)

	return &socialUser{
		ID:       int(newID),
		Login:    login,
		FullName: fullName,
		UserType: "student",
		Email:    email,
	}, nil
}

func (h *SocialAuthHandler) makeJWT(u *socialUser) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":   u.ID,
		"login":     u.Login,
		"user_type": u.UserType,
		"full_name": u.FullName,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})
	s, _ := token.SignedString([]byte(h.JWTSecret))
	return s
}

func (h *SocialAuthHandler) setCookies(w http.ResponseWriter, u *socialUser, tokenString string) {
	domain := cookieDomain()
	secure := cookieSecure()
	for _, c := range []http.Cookie{
		{Name: "token", Value: url.QueryEscape(tokenString), Path: "/", Domain: domain, MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode},
		{Name: "user_type", Value: u.UserType, Path: "/", Domain: domain, MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode},
		{Name: "user_id", Value: strconv.Itoa(u.ID), Path: "/", Domain: domain, MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode},
		{Name: "full_name", Value: url.QueryEscape(u.FullName), Path: "/", Domain: domain, MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode},
	} {
		cc := c
		http.SetCookie(w, &cc)
	}
}
