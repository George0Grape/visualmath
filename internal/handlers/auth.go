package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"visualmath/internal/auth"
	"visualmath/internal/email"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

// registerLimiter — простой in-memory IP-рейт-лимитер для /api/register
var registerLimiter = struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}{buckets: make(map[string][]time.Time)}

const (
	regMaxAttempts = 5
	regWindow      = 15 * time.Minute
)

func checkRegisterLimit(ip string) bool {
	registerLimiter.mu.Lock()
	defer registerLimiter.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-regWindow)
	prev := registerLimiter.buckets[ip]
	var fresh []time.Time
	for _, t := range prev {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= regMaxAttempts {
		registerLimiter.buckets[ip] = fresh
		return false
	}
	registerLimiter.buckets[ip] = append(fresh, now)
	return true
}

// cookieDomain берётся из env COOKIE_DOMAIN (например ".visualmath.ru").
// Без него cookie работают только для текущего хоста, не для www.
func cookieDomain() string {
	return os.Getenv("COOKIE_DOMAIN")
}

// cookieSecure включает флаг Secure (cookie только по HTTPS).
// На проде COOKIE_SECURE=true; в локальной разработке по HTTP — не задаём, иначе cookie не выставятся.
func cookieSecure() bool {
	return os.Getenv("COOKIE_SECURE") == "true"
}

type User struct {
	ID           int
	Login        string
	PasswordHash string
	FullName     string
	UserType     string
	GroupNumber  sql.NullString
	Email        string
}

type AuthHandler struct {
	DB        *sql.DB
	JWTSecret string
	Limiter   *LoginLimiter
}

type RegisterRequest struct {
	Login       string `json:"login"      validate:"required,min=3,max=30,login_chars"`
	Password    string `json:"password"   validate:"required"`
	FullName    string `json:"full_name"  validate:"omitempty,min=2,max=100"`
	UserType    string `json:"user_type"`
	GroupNumber string `json:"group_number"`
	Email       string `json:"email"      validate:"required,email"`
}

type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = strings.SplitN(fwd, ",", 2)[0]
	}
	if !checkRegisterLimit(ip) {
		jsonError(w, "Слишком много попыток. Повторите через 15 минут.", http.StatusTooManyRequests)
		return
	}

	var req RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	req.UserType = "student"

	if err := validate.Struct(&req); err != nil {
		http.Error(w, validationError(err), http.StatusBadRequest)
		return
	}

	// проверяем, не занят ли логин/email
	var existing int
	h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE login=? OR email=?`, req.Login, req.Email).Scan(&existing)
	if existing > 0 {
		jsonError(w, "Пользователь с таким логином или email уже существует", http.StatusConflict)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Ошибка при создании пользователя", http.StatusInternalServerError)
		return
	}

	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	expiresAt := time.Now().Add(15 * time.Minute)

	data, _ := json.Marshal(map[string]string{
		"login":        req.Login,
		"password_hash": string(hashedPassword),
		"full_name":    req.FullName,
		"group_number": req.GroupNumber,
	})

	_, err = h.DB.Exec(
		`INSERT OR REPLACE INTO email_verifications (email, code, data_json, expires_at) VALUES (?, ?, ?, ?)`,
		req.Email, code, string(data), expiresAt,
	)
	if err != nil {
		http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	go func() {
		if err := email.SendVerificationCode(req.Email, code); err != nil {
			log.Printf("[email] failed to send verification to %s: %v", req.Email, err)
		}
	}()

	respondJSON(w, map[string]interface{}{
		"success":     true,
		"need_verify": true,
		"message":     "Код подтверждения отправлен на " + req.Email,
	})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	var storedCode, dataJSON string
	var expiresAt time.Time
	err := h.DB.QueryRow(
		`SELECT code, data_json, expires_at FROM email_verifications WHERE email=?`, req.Email,
	).Scan(&storedCode, &dataJSON, &expiresAt)

	if err == sql.ErrNoRows {
		jsonError(w, "Сначала пройдите регистрацию", http.StatusBadRequest)
		return
	}
	if time.Now().After(expiresAt) {
		h.DB.Exec(`DELETE FROM email_verifications WHERE email=?`, req.Email)
		jsonError(w, "Код истёк, зарегистрируйтесь снова", http.StatusBadRequest)
		return
	}
	if storedCode != strings.TrimSpace(req.Code) {
		jsonError(w, "Неверный код", http.StatusBadRequest)
		return
	}

	var data map[string]string
	json.Unmarshal([]byte(dataJSON), &data)

	res, err := h.DB.Exec(
		`INSERT INTO users (login, password_hash, full_name, user_type, group_number, email) VALUES (?, ?, ?, 'student', ?, ?)`,
		data["login"], data["password_hash"], data["full_name"], data["group_number"], req.Email,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "Пользователь с таким логином или email уже существует", http.StatusConflict)
			return
		}
		http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	h.DB.Exec(`DELETE FROM email_verifications WHERE email=?`, req.Email)
	lastID, _ := res.LastInsertId()

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "Аккаунт создан",
		"user_id": lastID,
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	identifier := strings.TrimSpace(req.Login)
	if identifier == "" || req.Password == "" {
		http.Error(w, "Логин/email и пароль обязательны", http.StatusBadRequest)
		return
	}

	if h.Limiter != nil {
		ip := realIP(r)
		if h.Limiter.IsBlocked(ip) {
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Слишком много неудачных попыток. Повторите через 15 минут.", http.StatusTooManyRequests)
			return
		}
	}

	// ищем по логину или email — пользователю не нужно знать, как именно он зарегистрирован
	query := `
        SELECT id, login, password_hash, full_name, user_type, group_number, email
        FROM users
        WHERE login = ? OR email = ?
    `

	var user User
	err := h.DB.QueryRow(query, identifier, identifier).Scan(
		&user.ID,
		&user.Login,
		&user.PasswordHash,
		&user.FullName,
		&user.UserType,
		&user.GroupNumber,
		&user.Email,
	)

	if err == sql.ErrNoRows {
		if h.Limiter != nil {
			h.Limiter.RecordFailure(realIP(r))
		}
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		if h.Limiter != nil {
			h.Limiter.RecordFailure(realIP(r))
		}
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	}
	if h.Limiter != nil {
		h.Limiter.RecordSuccess(realIP(r))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":   user.ID,
		"login":     user.Login,
		"user_type": user.UserType,
		"full_name": user.FullName,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		http.Error(w, "Ошибка генерации токена", http.StatusInternalServerError)
		return
	}

	domain := cookieDomain()
	secure := cookieSecure()
	http.SetCookie(w, &http.Cookie{
		Name:     "user_type",
		Value:    user.UserType,
		Path:     "/",
		Domain:   domain,
		MaxAge:   24 * 60 * 60,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "user_id",
		Value:    strconv.Itoa(user.ID),
		Path:     "/",
		Domain:   domain,
		MaxAge:   24 * 60 * 60,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "full_name",
		Value:    url.QueryEscape(user.FullName),
		Path:     "/",
		Domain:   domain,
		MaxAge:   24 * 60 * 60,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	encodedToken := url.QueryEscape(tokenString)
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    encodedToken,
		Path:     "/",
		Domain:   domain,
		MaxAge:   24 * 60 * 60,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	response := map[string]interface{}{
		"success": true,
		"message": "Вход выполнен успешно",
		"token":   tokenString,
		"user": map[string]interface{}{
			"id":           user.ID,
			"login":        user.Login,
			"full_name":    user.FullName,
			"user_type":    user.UserType,
			"group_number": user.GroupNumber.String,
			"email":        user.Email,
		},
	}

	respondJSON(w, response)
}

func GetUserIDFromContext(r *http.Request) (int, error) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return 0, errors.New("user not authenticated")
	}
	return claims.UserID, nil
}

func GetUserTypeFromContext(r *http.Request) (string, error) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return "", errors.New("user not authenticated")
	}
	return claims.UserType, nil
}

