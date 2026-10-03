package handlers

import (
	"database/sql"
	"net/http"
	"visualmath/internal/auth"
)

// DashboardHandler обрабатывает страницу личного кабинета
type DashboardHandler struct {
	DB *sql.DB
}

// Dashboard показывает личный кабинет в зависимости от роли пользователя
func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	fullName := claims.FullName
	if fullName == "" {
		fullName = claims.Login
	}
	if fullName == "" {
		fullName = "Пользователь"
	}

	var avatarURL string
	if h.DB != nil {
		h.DB.QueryRow(`SELECT COALESCE(avatar_url,'') FROM users WHERE id = ?`, claims.UserID).Scan(&avatarURL)
	}

	tmplName := "dashboard-student.html"
	if claims.UserType == "teacher" {
		tmplName = "dashboard-teacher.html"
	}

	execTemplate(w, tmplName, map[string]interface{}{
		"FullName":  fullName,
		"AvatarURL": avatarURL,
	})
}
