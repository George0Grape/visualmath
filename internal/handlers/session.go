package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"
	"visualmath/internal/sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type SessionHandler struct {
	DB  *sql.DB
	WSM *sync.WebSocketManager
}

func (h *SessionHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	teacherID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		LectureID   int    `json:"lecture_id"`
		SessionName string `json:"session_name"`
		JoinCode    string `json:"join_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var lectureExists bool
	err = h.DB.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM lectures
            WHERE id = ? AND author_id = ?
        )
    `, req.LectureID, teacherID).Scan(&lectureExists)

	if err != nil || !lectureExists {
		http.Error(w, "Lecture not found or access denied", http.StatusNotFound)
		return
	}

	var lectureTitle string
	err = h.DB.QueryRow("SELECT title FROM lectures WHERE id = ?", req.LectureID).Scan(&lectureTitle)
	if err == sql.ErrNoRows {
		http.Error(w, "Lecture not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()

	if req.JoinCode == "" {
		req.JoinCode = generateJoinCode()
	}

	_, err = h.DB.Exec(`
        INSERT INTO active_sessions
        (session_id, lecture_id, teacher_id, session_name, join_code, is_active)
        VALUES (?, ?, ?, ?, ?, 1)
    `, sessionID, req.LectureID, teacherID, req.SessionName, req.JoinCode)

	if err != nil {
		http.Error(w, "Failed to create session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success":    true,
		"message":    "Session started successfully",
		"session_id": sessionID,
		"session": map[string]interface{}{
			"id":            sessionID,
			"lecture_id":    req.LectureID,
			"lecture_title": lectureTitle,
			"session_name":  req.SessionName,
			"join_code":     req.JoinCode,
			"teacher_id":    teacherID,
			"created_at":    time.Now().Format(time.RFC3339),
			"student_count": 0,
		},
	}

	respondJSON(w, response)
}

func (h *SessionHandler) GetActiveSessions(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userType string
	err = h.DB.QueryRow("SELECT user_type FROM users WHERE id = ?", userID).Scan(&userType)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// автоматически завершаем сессии старше 3 часов
	h.DB.Exec(`UPDATE active_sessions SET is_active = 0, ended_at = CURRENT_TIMESTAMP
		WHERE is_active = 1 AND created_at < datetime('now', '-3 hours')`)

	var rows *sql.Rows

	if userType == "teacher" {
		rows, err = h.DB.Query(`
            SELECT
                s.session_id,
                s.lecture_id,
                s.session_name,
                s.join_code,
                strftime('%Y-%m-%dT%H:%M:%SZ', s.created_at) as created_at,
                l.title as lecture_title,
                u.full_name as teacher_name,
                COUNT(DISTINCT ss.student_id) as student_count
            FROM active_sessions s
            JOIN lectures l ON s.lecture_id = l.id
            JOIN users u ON s.teacher_id = u.id
            LEFT JOIN session_students ss ON s.session_id = ss.session_id AND ss.is_active = 1
            WHERE s.is_active = 1 AND s.teacher_id = ?
            GROUP BY s.session_id
            ORDER BY s.created_at DESC
        `, userID)
	} else {
		rows, err = h.DB.Query(`
            SELECT
                s.session_id,
                s.lecture_id,
                s.session_name,
                s.join_code,
                strftime('%Y-%m-%dT%H:%M:%SZ', s.created_at) as created_at,
                l.title as lecture_title,
                u.full_name as teacher_name,
                COUNT(DISTINCT ss.student_id) as student_count,
                EXISTS(SELECT 1 FROM session_students WHERE session_id = s.session_id AND student_id = ?) as is_joined
            FROM active_sessions s
            JOIN lectures l ON s.lecture_id = l.id
            JOIN users u ON s.teacher_id = u.id
            LEFT JOIN session_students ss ON s.session_id = ss.session_id AND ss.is_active = 1
            WHERE s.is_active = 1
            GROUP BY s.session_id
            ORDER BY s.created_at DESC
        `, userID)
	}

	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sessions []map[string]interface{}
	for rows.Next() {
		var (
			sessionID                                                   string
			lectureID, studentCount                                     int
			sessionName, joinCode, createdAt, lectureTitle, teacherName string
			isJoined                                                    bool
		)

		if userType == "teacher" {
			err = rows.Scan(&sessionID, &lectureID, &sessionName, &joinCode, &createdAt,
				&lectureTitle, &teacherName, &studentCount)
		} else {
			err = rows.Scan(&sessionID, &lectureID, &sessionName, &joinCode, &createdAt,
				&lectureTitle, &teacherName, &studentCount, &isJoined)
		}

		if err != nil {
			continue
		}

		session := map[string]interface{}{
			"session_id":    sessionID,
			"lecture_id":    lectureID,
			"session_name":  sessionName,
			"lecture_title": lectureTitle,
			"teacher_name":  teacherName,
			"join_code":     joinCode,
			"created_at":    createdAt,
			"student_count": studentCount,
			"is_active":     true,
		}

		if userType == "student" {
			session["is_joined"] = isJoined
		}

		sessions = append(sessions, session)
	}

	respondJSON(w, sessions)
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": message})
}

func (h *SessionHandler) JoinSession(w http.ResponseWriter, r *http.Request) {
	studentID, err := GetUserIDFromContext(r)
	if err != nil {
		jsonError(w, "Необходима авторизация", http.StatusUnauthorized)
		return
	}

	var userType string
	err = h.DB.QueryRow("SELECT user_type FROM users WHERE id = ?", studentID).Scan(&userType)
	if err != nil || userType != "student" {
		jsonError(w, "Только студенты могут присоединяться к лекциям", http.StatusForbidden)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	joinCode := r.URL.Query().Get("join_code")

	if sessionID == "" && joinCode == "" {
		jsonError(w, "Укажите session_id или join_code", http.StatusBadRequest)
		return
	}

	var (
		dbSessionID             string
		lectureID, teacherID    int
		sessionName, dbJoinCode string
		isActive                bool
	)

	query := `
        SELECT session_id, lecture_id, teacher_id, session_name, join_code, is_active
        FROM active_sessions
        WHERE is_active = 1
    `
	var args []interface{}
	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	} else {
		query += " AND join_code = ?"
		args = append(args, joinCode)
	}

	err = h.DB.QueryRow(query, args...).Scan(&dbSessionID, &lectureID, &teacherID,
		&sessionName, &dbJoinCode, &isActive)
	if err != nil {
		jsonError(w, "Лекция не найдена или уже завершена", http.StatusNotFound)
		return
	}

	if !isActive {
		jsonError(w, "Лекция уже завершена", http.StatusBadRequest)
		return
	}

	var alreadyJoined bool
	h.DB.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM session_students
            WHERE session_id = ? AND student_id = ? AND is_active = 1
        )
    `, dbSessionID, studentID).Scan(&alreadyJoined)

	if alreadyJoined {
		respondJSON(w, map[string]interface{}{
			"success":      true,
			"message":      "Already joined",
			"session_id":   dbSessionID,
			"lecture_id":   lectureID,
			"redirect_url": fmt.Sprintf("/student/lecture/slides/%d?session=%s", lectureID, dbSessionID),
		})
		return
	}

	_, err = h.DB.Exec(`
        INSERT INTO session_students (session_id, student_id, joined_at, is_active)
        VALUES (?, ?, CURRENT_TIMESTAMP, 1)
        ON CONFLICT(session_id, student_id) DO UPDATE SET
        is_active = 1,
        left_at = NULL,
        joined_at = CURRENT_TIMESTAMP
    `, dbSessionID, studentID)
	if err != nil {
		jsonError(w, "Ошибка подключения: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.DB.Exec(`INSERT INTO session_activity (session_id, student_id, action_type) VALUES (?, ?, 'join')`, dbSessionID, studentID)

	respondJSON(w, map[string]interface{}{
		"success":      true,
		"message":      "Successfully joined session",
		"session_id":   dbSessionID,
		"lecture_id":   lectureID,
		"redirect_url": fmt.Sprintf("/student/lecture/slides/%d?session=%s", lectureID, dbSessionID),
	})
}

func (h *SessionHandler) EndSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	callerID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var ownerID int
	err = h.DB.QueryRow(`SELECT teacher_id FROM active_sessions WHERE session_id = ?`, sessionID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ownerID != callerID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_, err = h.DB.Exec(`
        UPDATE active_sessions
        SET is_active = 0, ended_at = CURRENT_TIMESTAMP
        WHERE session_id = ?
    `, sessionID)

	if err != nil {
		http.Error(w, "Failed to end session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = h.DB.Exec(`
        UPDATE session_students
        SET is_active = 0, left_at = CURRENT_TIMESTAMP
        WHERE session_id = ? AND is_active = 1
    `, sessionID)

	respondJSON(w, map[string]interface{}{
		"success":    true,
		"message":    "Session ended successfully",
		"session_id": sessionID,
	})
}

func (h *SessionHandler) GetSessionStudents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	callerID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var ownerID int
	err = h.DB.QueryRow(`SELECT teacher_id FROM active_sessions WHERE session_id = ?`, sessionID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ownerID != callerID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(`
        SELECT
            u.id,
            u.full_name,
            u.login,
            u.group_number,
            ss.joined_at
        FROM session_students ss
        JOIN users u ON ss.student_id = u.id
        WHERE ss.session_id = ? AND ss.is_active = 1
        ORDER BY ss.joined_at
    `, sessionID)

	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var students []map[string]interface{}
	for rows.Next() {
		var id int
		var fullName, login, groupNumber, joinedAt string

		err := rows.Scan(&id, &fullName, &login, &groupNumber, &joinedAt)
		if err != nil {
			continue
		}

		students = append(students, map[string]interface{}{
			"id":           id,
			"full_name":    fullName,
			"login":        login,
			"group_number": groupNumber,
			"joined_at":    joinedAt,
		})
	}

	respondJSON(w, students)
}

func (h *SessionHandler) GetSessionInfo(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	var (
		sessionIDStr, sessionName, joinCode, createdAt string
		lectureID, teacherID                           int
		lectureTitle, teacherName                      string
		studentCount                                   int
	)

	err := h.DB.QueryRow(`
        SELECT
            s.session_id,
            s.session_name,
            s.join_code,
            s.created_at,
            s.lecture_id,
            l.title as lecture_title,
            s.teacher_id,
            u.full_name as teacher_name,
            COUNT(DISTINCT ss.student_id) as student_count
        FROM active_sessions s
        JOIN lectures l ON s.lecture_id = l.id
        JOIN users u ON s.teacher_id = u.id
        LEFT JOIN session_students ss ON s.session_id = ss.session_id AND ss.is_active = 1
        WHERE s.session_id = ? AND s.is_active = 1
        GROUP BY s.session_id
    `, sessionID).Scan(&sessionIDStr, &sessionName, &joinCode, &createdAt,
		&lectureID, &lectureTitle, &teacherID, &teacherName, &studentCount)

	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	sessionInfo := map[string]interface{}{
		"session_id":    sessionIDStr,
		"session_name":  sessionName,
		"lecture_id":    lectureID,
		"lecture_title": lectureTitle,
		"teacher_id":    teacherID,
		"teacher_name":  teacherName,
		"join_code":     joinCode,
		"created_at":    createdAt,
		"student_count": studentCount,
		"is_active":     true,
		"student_url":   fmt.Sprintf("/student/lecture/slides/%d?session=%s", lectureID, sessionIDStr),
	}

	respondJSON(w, sessionInfo)
}

func (h *SessionHandler) TeacherSessionPage(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	var lectureID int
	err := h.DB.QueryRow(`
        SELECT lecture_id FROM active_sessions
        WHERE session_id = ? AND is_active = 1
    `, sessionID).Scan(&lectureID)

	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/teacher/lecture/slides/%d?session=%s", lectureID, sessionID), http.StatusFound)
}

func (h *SessionHandler) StudentSessionPage(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	var lectureID int
	err := h.DB.QueryRow(`
        SELECT lecture_id FROM active_sessions
        WHERE session_id = ? AND is_active = 1
    `, sessionID).Scan(&lectureID)

	if err != nil {
		http.Error(w, "Session not found or ended", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/student/lecture/slides/%d?session=%s", lectureID, sessionID), http.StatusFound)
}

func (h *SessionHandler) ResultsPage(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	execTemplate(w, "session-results.html", map[string]interface{}{
		"SessionID": sessionID,
	})
}

// генерирует короткий код для входа — убраны похожие символы (0/O, 1/I/L)
func generateJoinCode() string {
	const charset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	result := make([]byte, 6)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}
