package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
	"visualmath/internal/auth"
)

type ProfileHandler struct {
	DB        *sql.DB
	JWTSecret string
	BasePath  string
}

func (h *ProfileHandler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	execTemplate(w, "profile.html", map[string]string{"FullName": claims.FullName, "UserType": claims.UserType})
}

func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var fullName, email, login, userType string
	var groupNumber, university, avatarURL, about sql.NullString
	var lastEdit sql.NullTime
	err := h.DB.QueryRow(
		`SELECT login, full_name, email, user_type, group_number, university, last_profile_edit, COALESCE(avatar_url,''), COALESCE(about,'') FROM users WHERE id = ?`,
		claims.UserID,
	).Scan(&login, &fullName, &email, &userType, &groupNumber, &university, &lastEdit, &avatarURL, &about)
	if err != nil {
		http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	canEditAt := ""
	if lastEdit.Valid {
		next := lastEdit.Time.Add(24 * time.Hour)
		if time.Now().Before(next) {
			canEditAt = next.Format("02.01.2006 15:04")
		}
	}

	respondJSON(w, map[string]interface{}{
		"login":        login,
		"full_name":    fullName,
		"email":        email,
		"user_type":    userType,
		"group_number": groupNumber.String,
		"university":   university.String,
		"avatar_url":   avatarURL.String,
		"about":        about.String,
		"can_edit_at":  canEditAt,
	})
}

func (h *ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		FullName        string `json:"full_name"`
		Email           string `json:"email"`
		GroupNumber     string `json:"group_number"`
		University      string `json:"university"`
		About           string `json:"about"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	req.FullName = strings.TrimSpace(req.FullName)
	req.Email = strings.TrimSpace(req.Email)
	req.GroupNumber = strings.TrimSpace(req.GroupNumber)
	req.University = strings.TrimSpace(req.University)
	req.About = strings.TrimSpace(req.About)
	if len([]rune(req.About)) > 500 {
		req.About = string([]rune(req.About)[:500])
	}

	if req.Email == "" {
		jsonError(w, "Email обязателен", http.StatusBadRequest)
		return
	}

	var existingID int
	err := h.DB.QueryRow(`SELECT id FROM users WHERE email = ? AND id != ?`, req.Email, claims.UserID).Scan(&existingID)
	if err == nil {
		jsonError(w, "Этот email уже используется другим аккаунтом", http.StatusConflict)
		return
	}

	// Троттл "раз в день" на редактирование профиля временно снят - он мешал
	// менять пароль (форма шлёт имя вместе с паролем -> считалось правкой профиля).

	if req.NewPassword != "" {
		if req.CurrentPassword == "" {
			jsonError(w, "Для смены пароля введите текущий пароль", http.StatusBadRequest)
			return
		}

		var hash string
		if err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, claims.UserID).Scan(&hash); err != nil {
			jsonError(w, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
			jsonError(w, "Неверный текущий пароль", http.StatusUnauthorized)
			return
		}

		if len(req.NewPassword) < 6 {
			jsonError(w, "Новый пароль должен быть не менее 6 символов", http.StatusBadRequest)
			return
		}

		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
			return
		}

		_, err = h.DB.Exec(
			`UPDATE users SET full_name = ?, email = ?, group_number = ?, university = ?, about = ?, password_hash = ?, last_profile_edit = ? WHERE id = ?`,
			req.FullName, req.Email, req.GroupNumber, req.University, req.About, string(newHash), time.Now(), claims.UserID,
		)
		if err != nil {
			http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}
	} else {
		_, err = h.DB.Exec(
			`UPDATE users SET full_name = ?, email = ?, group_number = ?, university = ?, about = ?, last_profile_edit = ? WHERE id = ?`,
			req.FullName, req.Email, req.GroupNumber, req.University, req.About, time.Now(), claims.UserID,
		)
		if err != nil {
			http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}
	}

	// обновляем токен с актуальным именем - иначе в шапке будет старое ФИО
	var updatedFullName, updatedLogin, updatedUserType string
	h.DB.QueryRow(`SELECT login, full_name, user_type FROM users WHERE id = ?`, claims.UserID).
		Scan(&updatedLogin, &updatedFullName, &updatedUserType)

	newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":   claims.UserID,
		"login":     updatedLogin,
		"user_type": updatedUserType,
		"full_name": updatedFullName,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})
	tokenString, err := newToken.SignedString([]byte(h.JWTSecret))
	if err == nil {
		domain := cookieDomain()
		http.SetCookie(w, &http.Cookie{
			Name:     "token",
			Value:    url.QueryEscape(tokenString),
			Path:     "/",
			Domain:   domain,
			MaxAge:   24 * 60 * 60,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "full_name",
			Value:    url.QueryEscape(updatedFullName),
			Path:     "/",
			Domain:   domain,
			MaxAge:   24 * 60 * 60,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	respondJSON(w, map[string]interface{}{
		"message": "Профиль обновлён",
		"token":   tokenString,
	})
}

func (h *ProfileHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		jsonError(w, "Файл слишком большой (максимум 5 МБ)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		jsonError(w, "Файл не найден", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	switch ct {
	case "image/jpeg", "image/jpg", "image/png", "image/webp":
	default:
		jsonError(w, "Допустимы только JPEG, PNG или WebP", http.StatusBadRequest)
		return
	}

	var src image.Image
	var decodeErr error
	switch ct {
	case "image/jpeg", "image/jpg":
		src, decodeErr = jpeg.Decode(file)
	case "image/png":
		src, decodeErr = png.Decode(file)
	case "image/webp":
		src, decodeErr = webp.Decode(file)
	}
	if decodeErr != nil {
		jsonError(w, "Не удалось прочитать изображение", http.StatusBadRequest)
		return
	}

	const maxSize = 300
	bounds := src.Bounds()
	w0, h0 := bounds.Dx(), bounds.Dy()
	nw, nh := w0, h0
	if w0 > maxSize || h0 > maxSize {
		if w0 > h0 {
			nw = maxSize
			nh = h0 * maxSize / w0
		} else {
			nh = maxSize
			nw = w0 * maxSize / h0
		}
	}
	dst2 := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.BiLinear.Scale(dst2, dst2.Bounds(), src, src.Bounds(), draw.Src, nil)

	uploadsDir := filepath.Join(h.BasePath, "web", "static", "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	// всегда сохраняем как JPEG для единообразия и меньшего размера
	filename := fmt.Sprintf("avatar_%d.jpg", claims.UserID)
	dstFile, err := os.Create(filepath.Join(uploadsDir, filename))
	if err != nil {
		http.Error(w, "Ошибка сохранения файла", http.StatusInternalServerError)
		return
	}
	defer dstFile.Close()
	if err := jpeg.Encode(dstFile, dst2, &jpeg.Options{Quality: 85}); err != nil {
		http.Error(w, "Ошибка кодирования", http.StatusInternalServerError)
		return
	}

	avatarURL := "/static/uploads/" + filename
	h.DB.Exec(`UPDATE users SET avatar_url = ? WHERE id = ?`, avatarURL, claims.UserID)

	respondJSON(w, map[string]interface{}{
		"success":    true,
		"avatar_url": avatarURL,
	})
}

func (h *ProfileHandler) PublicProfilePage(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	claims, ok := auth.GetUserFromContext(r.Context())
	if ok && strconv.Itoa(claims.UserID) == profileID {
		http.Redirect(w, r, "/profile", http.StatusFound)
		return
	}
	execTemplate(w, "profile-public.html", map[string]string{"ProfileID": profileID})
}

func (h *ProfileHandler) GetPublicProfile(w http.ResponseWriter, r *http.Request) {
	profileID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || profileID <= 0 {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// email НЕ отдаём: это публичный профиль, доступный по любому id -
	// иначе любой залогиненный перебором собрал бы почты всех пользователей
	var fullName, userType string
	var groupNumber, university, avatarURL, about sql.NullString
	err = h.DB.QueryRow(
		`SELECT full_name, user_type, group_number, university, COALESCE(avatar_url,''), COALESCE(about,'') FROM users WHERE id = ?`,
		profileID,
	).Scan(&fullName, &userType, &groupNumber, &university, &avatarURL, &about)
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{
		"id":           profileID,
		"full_name":    fullName,
		"user_type":    userType,
		"group_number": groupNumber.String,
		"university":   university.String,
		"avatar_url":   avatarURL.String,
		"about":        about.String,
	})
}

// UploadModuleImage сохраняет изображение для текстового модуля, возвращает URL
func (h *ProfileHandler) UploadModuleImage(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		jsonError(w, "Файл слишком большой (максимум 10 МБ)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		jsonError(w, "Файл не найден", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	var src image.Image
	switch ct {
	case "image/jpeg", "image/jpg":
		src, err = jpeg.Decode(file)
	case "image/png":
		src, err = png.Decode(file)
	case "image/webp":
		src, err = webp.Decode(file)
	default:
		jsonError(w, "Допустимы только JPEG, PNG или WebP", http.StatusBadRequest)
		return
	}
	if err != nil {
		jsonError(w, "Не удалось прочитать изображение", http.StatusBadRequest)
		return
	}

	uploadsDir := filepath.Join(h.BasePath, "web", "static", "uploads")
	os.MkdirAll(uploadsDir, 0755)

	filename := fmt.Sprintf("img_%d_%d.jpg", time.Now().UnixNano(), rand.Intn(9999))
	dstFile, err := os.Create(filepath.Join(uploadsDir, filename))
	if err != nil {
		http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
		return
	}
	defer dstFile.Close()
	jpeg.Encode(dstFile, src, &jpeg.Options{Quality: 88})

	respondJSON(w, map[string]string{"url": "/static/uploads/" + filename})
}
