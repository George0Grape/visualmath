package handlers
/*
import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"
    
    "github.com/golang-jwt/jwt/v4"
	"visualmath/internal/auth"
	"visualmath/internal/models"
)

type OAuthHandler struct {
    DB      *sql.DB
    OAuth   *auth.OAuthConfig
}

// NewOAuthHandler создает новый обработчик OAuth
func NewOAuthHandler(db *sql.DB) *OAuthHandler {
    return &OAuthHandler{
        DB:    db,
        OAuth: auth.NewOAuthConfig(),
    }
}

// VKAuthHandler перенаправляет на страницу авторизации VK
func (h *OAuthHandler) VKAuthHandler(w http.ResponseWriter, r *http.Request) {
    state := auth.GenerateStateToken()
    
    // Сохраняем state в сессию (упрощенно - в cookie)
    http.SetCookie(w, &http.Cookie{
        Name:     "oauth_state",
        Value:    state,
        Expires:  time.Now().Add(10 * time.Minute),
        HttpOnly: true,
        Path:     "/",
    })
    
    authURL := h.OAuth.GetVKAuthURL(state)
    http.Redirect(w, r, authURL, http.StatusFound)
}

// GoogleAuthHandler перенаправляет на страницу авторизации Google
func (h *OAuthHandler) GoogleAuthHandler(w http.ResponseWriter, r *http.Request) {
    state := auth.GenerateStateToken()
    
    // Сохраняем state в сессию
    http.SetCookie(w, &http.Cookie{
        Name:     "oauth_state",
        Value:    state,
        Expires:  time.Now().Add(10 * time.Minute),
        HttpOnly: true,
        Path:     "/",
    })
    
    authURL := h.OAuth.GetGoogleAuthURL(state)
    http.Redirect(w, r, authURL, http.StatusFound)
}

// VKCallbackHandler обрабатывает callback от VK
func (h *OAuthHandler) VKCallbackHandler(w http.ResponseWriter, r *http.Request) {
    h.handleOAuthCallback(w, r, "vk")
}

// GoogleCallbackHandler обрабатывает callback от Google
func (h *OAuthHandler) GoogleCallbackHandler(w http.ResponseWriter, r *http.Request) {
    h.handleOAuthCallback(w, r, "google")
}

// handleOAuthCallback общая функция обработки callback
func (h *OAuthHandler) handleOAuthCallback(w http.ResponseWriter, r *http.Request, provider string) {
    // Получаем код из URL
    code := r.URL.Query().Get("code")
    if code == "" {
        http.Error(w, "Код авторизации не получен", http.StatusBadRequest)
        return
    }
    
    // Проверяем state для защиты от CSRF
    state := r.URL.Query().Get("state")
    cookie, err := r.Cookie("oauth_state")
    if err != nil || cookie.Value != state {
        http.Error(w, "Неверный state токен", http.StatusBadRequest)
        return
    }
    
    var userInfo *auth.OAuthUserInfo
    ctx := context.Background()
    
    // Получаем информацию о пользователе в зависимости от провайдера
    switch provider {
    case "vk":
        userInfo, err = h.OAuth.ExchangeVKCode(ctx, code)
    case "google":
        userInfo, err = h.OAuth.ExchangeGoogleCode(ctx, code)
    default:
        http.Error(w, "Неизвестный провайдер", http.StatusBadRequest)
        return
    }
    
    if err != nil {
        http.Error(w, fmt.Sprintf("Ошибка OAuth: %v", err), http.StatusInternalServerError)
        return
    }
    
    // Ищем или создаем пользователя в базе данных
    user, err := h.findOrCreateUser(userInfo)
    if err != nil {
        http.Error(w, fmt.Sprintf("Ошибка базы данных: %v", err), http.StatusInternalServerError)
        return
    }
    
    // Генерируем JWT токен
    token, err := h.generateJWTToken(user)
    if err != nil {
        http.Error(w, "Ошибка генерации токена", http.StatusInternalServerError)
        return
    }
    
    // Перенаправляем на страницу с токеном
    redirectURL := fmt.Sprintf("/oauth/success?token=%s&user_id=%d", token, user.ID)
    http.Redirect(w, r, redirectURL, http.StatusFound)
}

// findOrCreateUser ищет пользователя по OAuth ID или создает нового
func (h *OAuthHandler) findOrCreateUser(oauthUser *auth.OAuthUserInfo) (*models.User, error) {
    var user models.User
    
    // Сначала ищем по OAuth ID (provider_id)
    query := `
        SELECT u.id, u.login, u.full_name, u.user_type, u.group_number, u.email
        FROM users u
        INNER JOIN oauth_connections oc ON u.id = oc.user_id
        WHERE oc.provider = $1 AND oc.provider_user_id = $2
    `
    
    err := h.DB.QueryRow(query, oauthUser.Provider, oauthUser.ID).Scan(
        &user.ID,
        &user.Login,
        &user.FullName,
        &user.UserType,
        &user.GroupNumber,
        &user.Email,
    )
    
    if err == nil {
        // Пользователь найден
        return &user, nil
    }
    
    if err != sql.ErrNoRows {
        // Какая-то другая ошибка
        return nil, err
    }
    
    // Пользователь не найден, создаем нового
    
    // Генерируем логин из email
    login := strings.Split(oauthUser.Email, "@")[0]
    
    // Проверяем, не занят ли логин
    var count int
    h.DB.QueryRow("SELECT COUNT(*) FROM users WHERE login = $1", login).Scan(&count)
    if count > 0 {
        // Если логин занят, добавляем случайное число
        login = fmt.Sprintf("%s_%d", login, time.Now().Unix()%1000)
    }
    
    // Создаем полное имя
    fullName := strings.TrimSpace(fmt.Sprintf("%s %s", oauthUser.FirstName, oauthUser.LastName))
    if fullName == "" {
        fullName = login
    }
    
    // Вставляем нового пользователя
    tx, err := h.DB.Begin()
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()
    
    // Создаем пользователя
    var userID int
    err = tx.QueryRow(`
        INSERT INTO users (login, password_hash, full_name, user_type, email, email_verified)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `, login, "", fullName, "student", oauthUser.Email, true).Scan(&userID)
    
    if err != nil {
        return nil, err
    }
    
    // Создаем связь с OAuth провайдером
    _, err = tx.Exec(`
        INSERT INTO oauth_connections (user_id, provider, provider_user_id, email, full_name, avatar_url)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, userID, oauthUser.Provider, oauthUser.ID, oauthUser.Email, fullName, oauthUser.AvatarURL)
    
    if err != nil {
        return nil, err
    }
    
    err = tx.Commit()
    if err != nil {
        return nil, err
    }
    
    // Получаем созданного пользователя
    err = h.DB.QueryRow(`
        SELECT id, login, full_name, user_type, group_number, email
        FROM users WHERE id = $1
    `, userID).Scan(
        &user.ID,
        &user.Login,
        &user.FullName,
        &user.UserType,
        &user.GroupNumber,
        &user.Email,
    )
    
    return &user, err
}

// generateJWTToken генерирует JWT токен для пользователя
func (h *OAuthHandler) generateJWTToken(user *models.User) (string, error) {
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
        "user_id":   user.ID,
        "login":     user.Login,
        "user_type": user.UserType,
        "exp":       time.Now().Add(24 * time.Hour).Unix(),
    })
    
    return token.SignedString([]byte(h.OAuth.JWTKey))
}

// OAuthSuccessHandler отображает страницу успешной авторизации
func (h *OAuthHandler) OAuthSuccessHandler(w http.ResponseWriter, r *http.Request) {
    token := r.URL.Query().Get("token")
    userID := r.URL.Query().Get("user_id")
    
    if token == "" || userID == "" {
        http.Error(w, "Неверные параметры", http.StatusBadRequest)
        return
    }
    
    // Рендерим страницу успеха с JavaScript для сохранения токена
    html := `
    <!DOCTYPE html>
    <html>
    <head>
        <title>Успешная авторизация - VisualMath</title>
        <script>
            window.onload = function() {
                // Сохраняем токен в localStorage
                const urlParams = new URLSearchParams(window.location.search);
                const token = urlParams.get('token');
                const userId = urlParams.get('user_id');
                
                if (token && userId) {
                    localStorage.setItem('token', token);
                    localStorage.setItem('user_id', userId);
                    
                    // Перенаправляем на главную страницу
                    setTimeout(function() {
                        window.location.href = '/dashboard';
                    }, 1000);
                }
            }
        </script>
    </head>
    <body style="text-align: center; padding: 50px;">
        <h1>✅ Авторизация успешна!</h1>
        <p>Перенаправление на главную страницу...</p>
    </body>
    </html>
    `
    
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    fmt.Fprintf(w, html)
}*/