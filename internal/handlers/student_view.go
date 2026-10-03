package handlers

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type StudentViewHandler struct {
	DB *sql.DB
}

func (h *StudentViewHandler) StudentLectureView(w http.ResponseWriter, r *http.Request) {
	lectureID := chi.URLParam(r, "id")
	execTemplate(w, "student-lecture-view.html", map[string]string{"LectureID": lectureID})
}

func (h *StudentViewHandler) JoinLecture(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "join-lecture.html")
}
