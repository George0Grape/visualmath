package handlers

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// TeacherSlidesPage показывает управление лекцией для преподавателя
func (h *SlidesHandler) TeacherSlidesPage(w http.ResponseWriter, r *http.Request) {
	lectureID := chi.URLParam(r, "id")
	sessionID := r.URL.Query().Get("session")

	if sessionID == "" {
		sessionID = uuid.New().String()[:8]
		http.Redirect(w, r,
			"/teacher/lecture/slides/"+lectureID+"?session="+sessionID,
			http.StatusFound)
		return
	}

	teacherID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var exists int
	h.DB.QueryRow("SELECT COUNT(*) FROM active_sessions WHERE session_id = ?", sessionID).Scan(&exists)
	if exists == 0 {
		_, err := h.DB.Exec(`
            INSERT OR IGNORE INTO active_sessions
            (session_id, lecture_id, teacher_id, session_name, join_code, is_active)
            VALUES (?, ?, ?, ?, ?, 1)
        `, sessionID, lectureID, teacherID, "Лекция #"+lectureID, generateJoinCode())
		if err != nil {
			log.Printf("Failed to create session: %v", err)
		} else {
			log.Printf("✅ Created session %s for lecture %s (teacher %d)", sessionID, lectureID, teacherID)
		}
	}

	execTemplate(w, "teacher-slides.html", map[string]string{"LectureID": lectureID})
}
