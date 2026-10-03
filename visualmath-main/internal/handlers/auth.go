package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
	//"visualmath/internal/models"
)

type AuthHandler struct {
	DB        *sql.DB
	JWTSecret string
}

// RegisterRequest структура для регистрации
type RegisterRequest struct {
	Login       string `json:"login"`
	Password    string `json:"password"`
	FullName    string `json:"full_name"`
	UserType    string `json:"user_type"`
	GroupNumber string `json:"group_number"`
	Email       string `json:"email"`
}

// LoginRequest структура для входа
type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// Register обрабатывает регистрацию нового пользователя
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	// Декодируем JSON тело запроса
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	// Проверяем обязательные поля
	if req.Login == "" || req.Password == "" || req.FullName == "" ||
		req.UserType == "" || req.Email == "" {
		http.Error(w, "Все обязательные поля должны быть заполнены", http.StatusBadRequest)
		return
	}

	// Проверяем валидность типа пользователя
	if req.UserType != "student" && req.UserType != "teacher" && req.UserType != "admin" {
		http.Error(w, "Неверный тип пользователя", http.StatusBadRequest)
		return
	}

	// Для студентов проверяем наличие номера группы
	if req.UserType == "student" && req.GroupNumber == "" {
		http.Error(w, "Для студентов номер группы обязателен", http.StatusBadRequest)
		return
	}

	// Хэшируем пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Ошибка при создании пользователя", http.StatusInternalServerError)
		return
	}

	// Сохраняем пользователя в базу данных
	query := `
        INSERT INTO users (login, password_hash, full_name, user_type, group_number, email)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `

	var userID int
	err = h.DB.QueryRow(query,
		req.Login,
		string(hashedPassword),
		req.FullName,
		req.UserType,
		req.GroupNumber,
		req.Email,
	).Scan(&userID)

	if err != nil {
		// Проверяем, если пользователь уже существует
		if err.Error() == "pq: duplicate key value violates unique constraint" {
			http.Error(w, "Пользователь с таким логином или email уже существует", http.StatusConflict)
			return
		}
		http.Error(w, "Ошибка базы данных: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Отправляем успешный ответ
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Пользователь успешно зарегистрирован",
		"user_id": userID,
	})
}

// Login обрабатывает вход пользователя
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	// Декодируем JSON тело запроса
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	// Проверяем обязательные поля
	if req.Login == "" || req.Password == "" {
		http.Error(w, "Логин и пароль обязательны", http.StatusBadRequest)
		return
	}

	// Ищем пользователя в базе данных
	query := `
        SELECT id, login, password_hash, full_name, user_type, group_number, email
        FROM users 
        WHERE login = $1 OR email = $1
    `

	var user struct {
		ID           int
		Login        string
		PasswordHash string
		FullName     string
		UserType     string
		GroupNumber  sql.NullString
		Email        string
	}

	err := h.DB.QueryRow(query, req.Login).Scan(
		&user.ID,
		&user.Login,
		&user.PasswordHash,
		&user.FullName,
		&user.UserType,
		&user.GroupNumber,
		&user.Email,
	)

	if err == sql.ErrNoRows {
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	// Проверяем пароль
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	}

	// Генерируем JWT токен
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":   user.ID,
		"login":     user.Login,
		"user_type": user.UserType,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		http.Error(w, "Ошибка генерации токена", http.StatusInternalServerError)
		return
	}

	// Формируем ответ
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
