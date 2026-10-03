package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

type contextKey string

const UserContextKey contextKey = "user"

type UserClaims struct {
	UserID   int    `json:"user_id"`
	Login    string `json:"login"`
	UserType string `json:"user_type"`
	FullName string `json:"full_name"`
}

func AuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			publicRoutes := []string{
				"/login",
				"/register",
				"/api/login",
				"/api/register",
				"/static/",
				"/test",
				"/test-start",
				"/ws/",
				"/student/join-lecture",
				"/student-view/",
				"/auth/",
				"/oauth/",
			}

			currentPath := r.URL.Path

			isPublic := currentPath == "/"
			if !isPublic {
				for _, route := range publicRoutes {
					if strings.HasPrefix(currentPath, route) {
						isPublic = true
						break
					}
				}
			}

			if isPublic {
				next.ServeHTTP(w, r)
				return
			}

			var tokenString string

			// сначала смотрим заголовок Authorization
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				tokenString = strings.TrimPrefix(authHeader, "Bearer ")
			}

			// потом куку — браузер её шлёт автоматически
			if tokenString == "" {
				cookie, err := r.Cookie("token")
				if err == nil && cookie.Value != "" {
					decodedToken, err := url.QueryUnescape(cookie.Value)
					if err == nil {
						tokenString = decodedToken
					}
				}
			}

			// и напоследок query-параметр (для WebSocket и прямых ссылок)
			if tokenString == "" {
				tokenString = r.URL.Query().Get("token")
			}

			if tokenString == "" {
				if strings.HasPrefix(currentPath, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"success": false, "message": "Unauthorized"}`))
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     "token",
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				// проверяем что токен подписан HMAC — блокирует alg=none и alg confusion атаки
				if _, isHMAC := token.Method.(*jwt.SigningMethodHMAC); !isHMAC {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			})

			if err != nil || !token.Valid {
				if strings.HasPrefix(currentPath, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"success": false, "message": "Invalid token"}`))
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     "token",
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
				})
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			userID, ok := claims["user_id"].(float64)
			if !ok {
				if strings.HasPrefix(currentPath, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"success": false, "message": "Invalid token claims"}`))
					return
				}
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			login, _ := claims["login"].(string)
			userType, _ := claims["user_type"].(string)
			fullName, _ := claims["full_name"].(string)

			userClaims := UserClaims{
				UserID:   int(userID),
				Login:    login,
				UserType: userType,
				FullName: fullName,
			}

			ctx := context.WithValue(r.Context(), UserContextKey, userClaims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserFromContext(ctx context.Context) (*UserClaims, bool) {
	user, ok := ctx.Value(UserContextKey).(UserClaims)
	if !ok {
		return nil, false
	}
	return &user, true
}
