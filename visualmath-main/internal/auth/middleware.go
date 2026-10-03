package auth

import (
	"context"
	"net/http"
)

type contextKey string

const UserContextKey contextKey = "user"

type UserClaims struct {
	UserID   int    `json:"user_id"`
	Login    string `json:"login"`
	UserType string `json:"user_type"`
}

// AuthMiddleware теперь принимает jwtSecret и возвращает middleware
func AuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Здесь будет логика проверки JWT токена с использованием jwtSecret.
            // Пока оставляем заглушку, чтобы код работал.
            
            // TODO: Раскомментировать и реализовать проверку токена:
            /*
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                http.Error(w, "Authorization header required", http.StatusUnauthorized)
                return
            }
            // ... парсинг токена ...
            */

			// ЗАГЛУШКА (всегда авторизован как учитель с ID=1)
			userClaims := UserClaims{
				UserID:   1,
				Login:    "test_user",
				UserType: "teacher", 
			}

			ctx := context.WithValue(r.Context(), UserContextKey, userClaims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Логика проверки ролей
			next.ServeHTTP(w, r)
		})
	}
}

func GetUserFromContext(ctx context.Context) (*UserClaims, bool) {
	user, ok := ctx.Value(UserContextKey).(UserClaims)
	return &user, ok
}