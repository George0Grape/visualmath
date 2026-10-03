package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"visualmath/internal/storage"
)

type LectureHandler struct {
	Repo *storage.LectureRepo
}

type LectureRequest struct {
	Title       string `json:"title"`
	CourseName  string `json:"course_name"`
	Description string `json:"description"`
	ModuleIDs   []int  `json:"module_ids"`
	Published   bool   `json:"published"`
	HideTests   bool   `json:"hide_tests"`
	AllowBack   bool   `json:"allow_back"`
}

func (h *LectureHandler) ListLectures(w http.ResponseWriter, r *http.Request) {
	authorID, err := GetUserIDFromContext(r)
	if err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.Repo.ListByAuthor(authorID)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lectures := make([]map[string]interface{}, 0, len(list))
	for _, l := range list {
		lectures = append(lectures, map[string]interface{}{
			"id":            l.ID,
			"title":         l.Title,
			"course":        l.Course,
			"description":   l.Description,
			"author_name":   l.Author,
			"created_at":    l.CreatedAt,
			"modules_count": l.ModulesCount,
			"published":     l.Published,
			"allow_back":    l.AllowBack,
		})
	}

	respondJSON(w, lectures)
}

func (h *LectureHandler) CreateLecture(w http.ResponseWriter, r *http.Request) {
	authorID, err := GetUserIDFromContext(r)
	if err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req LectureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Title == "" || req.CourseName == "" {
		jsonError(w, "Title and course name are required", http.StatusBadRequest)
		return
	}

	if len(req.ModuleIDs) == 0 {
		jsonError(w, "At least one module is required", http.StatusBadRequest)
		return
	}

	lectureID, err := h.Repo.Create(req.Title, req.CourseName, req.Description, authorID, req.Published, req.HideTests, req.AllowBack, req.ModuleIDs)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "does not exist") {
			statusCode = http.StatusBadRequest
		}
		jsonError(w, err.Error(), statusCode)
		return
	}

	lecture, err := h.getLectureFromDB(int(lectureID))
	if err != nil {
		jsonError(w, "failed to fetch created lecture: "+err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "Лекция создана",
		"lecture": lecture,
	})
}

func (h *LectureHandler) GetLecture(w http.ResponseWriter, r *http.Request) {
	lectureIDStr := r.URL.Path[len("/api/lectures/"):]
	lectureID, err := strconv.Atoi(lectureIDStr)
	if err != nil {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}

	lecture, err := h.getLectureFromDB(lectureID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Lecture not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	respondJSON(w, lecture)
}

func (h *LectureHandler) getLectureFromDB(id int) (map[string]interface{}, error) {
	l, err := h.Repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	var modules []map[string]interface{}
	for _, mod := range l.Modules {
		modules = append(modules, map[string]interface{}{
			"id":          mod.ID,
			"order":       mod.Order,
			"title":       mod.Title,
			"type":        mod.Type,
			"description": mod.Description,
			"content":     mod.Content,
		})
	}

	return map[string]interface{}{
		"id":            l.ID,
		"title":         l.Title,
		"course_name":   l.Course,
		"author_name":   l.AuthorName,
		"author_id":     l.AuthorID,
		"description":   l.Description,
		"published":     l.Published,
		"hide_tests":    l.HideTests,
		"allow_back":    l.AllowBack,
		"created_at":    l.CreatedAt,
		"modules":       modules,
		"modules_count": len(l.Modules),
	}, nil
}

func (h *LectureHandler) UpdateLecture(w http.ResponseWriter, r *http.Request) {
	lectureIDStr := r.URL.Path[len("/api/lectures/"):]
	lectureID, err := strconv.Atoi(lectureIDStr)
	if err != nil {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}

	var req LectureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Title == "" || req.CourseName == "" {
		http.Error(w, "Title and course name are required", http.StatusBadRequest)
		return
	}

	if err := h.Repo.Update(lectureID, req.Title, req.CourseName, req.Description, req.Published, req.HideTests, req.AllowBack, req.ModuleIDs); err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	lecture, err := h.getLectureFromDB(lectureID)
	if err != nil {
		http.Error(w, "Failed to fetch updated lecture: "+err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success":    true,
		"message":    "Лекция успешно обновлена",
		"lecture":    lecture,
		"lecture_id": lectureID,
	})
}

func (h *LectureHandler) DeleteLecture(w http.ResponseWriter, r *http.Request) {
	lectureIDStr := r.URL.Path[len("/api/lectures/"):]
	lectureID, err := strconv.Atoi(lectureIDStr)
	if err != nil {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}

	if err := h.Repo.Delete(lectureID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Lecture not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to delete lecture: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	respondJSON(w, map[string]interface{}{
		"success":    true,
		"message":    "Лекция успешно удалена",
		"lecture_id": lectureID,
	})
}

func (h *LectureHandler) GetAvailableModules(w http.ResponseWriter, r *http.Request) {
	authorID, err := GetUserIDFromContext(r)
	if err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.Repo.GetAvailableModules(authorID)
	if err != nil {
		jsonError(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	modules := make([]map[string]interface{}, 0, len(list))
	for _, m := range list {
		var typeIcon string
		switch m.Type {
		case "text":
			typeIcon = "📝"
case "question":
			typeIcon = "❓"
		case "test":
			typeIcon = "📋"
		default:
			typeIcon = "📄"
		}
		modules = append(modules, map[string]interface{}{
			"id":          m.ID,
			"title":       m.Title,
			"course":      m.Course,
			"type":        m.Type,
			"type_icon":   typeIcon,
			"description": m.Description,
			"author":      m.Author,
			"created_at":  m.CreatedAt,
		})
	}

	respondJSON(w, modules)
}

func (h *LectureHandler) StartLecture(w http.ResponseWriter, r *http.Request) {
	lectureID, err := strconv.Atoi(r.URL.Query().Get("lecture_id"))
	if err != nil || lectureID <= 0 {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}

	studentID := 1

	exists, err := h.Repo.Exists(lectureID)
	if err != nil || !exists {
		http.Error(w, "Lecture not found", http.StatusNotFound)
		return
	}

	allowBack, err := h.Repo.GetAllowBack(lectureID)
	if err != nil {
		http.Error(w, "Failed to fetch lecture settings", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	progress := map[string]interface{}{
		"lecture_id":        lectureID,
		"student_id":        studentID,
		"current_module":    1,
		"completed_modules": []int{},
		"started_at":        now,
		"allow_back":        allowBack,
	}

	respondJSON(w, map[string]interface{}{
		"success":  true,
		"message":  "Лекция начата",
		"progress": progress,
	})
}

func (h *LectureHandler) CompleteModule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LectureID int     `json:"lecture_id"`
		ModuleID  int     `json:"module_id"`
		Score     float64 `json:"score"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	nextModuleID, err := h.Repo.GetNextModuleID(req.LectureID, req.ModuleID)
	if err != nil {
		if err == sql.ErrNoRows {
			nextModuleID = 0
		} else {
			http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	respondJSON(w, map[string]interface{}{
		"success":     true,
		"message":     "Модуль пройден",
		"next_module": nextModuleID,
		"completed":   true,
		"score":       req.Score,
	})
}

func (h *LectureHandler) GetStudentProgress(w http.ResponseWriter, r *http.Request) {
	lectureID, err := strconv.Atoi(r.URL.Query().Get("lecture_id"))
	if err != nil || lectureID <= 0 {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}

	totalModules, err := h.Repo.CountModules(lectureID)
	if err != nil {
		http.Error(w, "Failed to count modules", http.StatusInternalServerError)
		return
	}

	completedModules := []int{1}
	currentModule := 2

	progressPercent := 0
	if totalModules > 0 {
		progressPercent = len(completedModules) * 100 / totalModules
	}

	respondJSON(w, map[string]interface{}{
		"lecture_id":        lectureID,
		"student_id":        1,
		"current_module":    currentModule,
		"completed_modules": completedModules,
		"total_modules":     totalModules,
		"progress_percent":  progressPercent,
		"started_at":        "2024-01-10T10:00:00Z",
	})
}

func (h *LectureHandler) LecturesPage(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "lectures-list.html")
}

func (h *LectureHandler) CreateLecturePage(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "lecture-create.html")
}

func (h *LectureHandler) EditLecturePage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "lecture-edit.html", map[string]string{"LectureID": chi.URLParam(r, "id")})
}

func (h *LectureHandler) ViewLecturePage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "lecture-view.html", map[string]string{"LectureID": chi.URLParam(r, "id")})
}

func (h *LectureHandler) LaunchLectureSelection(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "lecture-launch-selection.html")
}

func (h *LectureHandler) PublishedLecturesPage(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "lectures-published.html")
}

func (h *LectureHandler) PublishedLectureReaderPage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "lecture-reader.html", map[string]string{"LectureID": chi.URLParam(r, "id")})
}

func (h *LectureHandler) PublishedLecturesAPI(w http.ResponseWriter, r *http.Request) {
	course := r.URL.Query().Get("course")
	author := r.URL.Query().Get("author")
	yearStr := r.URL.Query().Get("year")
	year := 0
	if yearStr != "" {
		year, _ = strconv.Atoi(yearStr)
	}

	authorID := 0
	if aid := r.URL.Query().Get("author_id"); aid != "" {
		authorID, _ = strconv.Atoi(aid)
	}

	list, err := h.Repo.ListPublished(course, author, year, authorID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]map[string]interface{}, 0, len(list))
	for _, l := range list {
		result = append(result, map[string]interface{}{
			"id":           l.ID,
			"title":        l.Title,
			"course":       l.Course,
			"description":  l.Description,
			"author":       l.Author,
			"author_id":    l.AuthorID,
			"created_at":   l.CreatedAt,
			"hide_tests":   l.HideTests,
			"modules_count": l.ModulesCount,
			"comment_count": l.CommentCount,
		})
	}

	courses, _ := h.Repo.DistinctCourses()
	years, _ := h.Repo.DistinctYears()

	respondJSON(w, map[string]interface{}{
		"lectures": result,
		"courses":  courses,
		"years":    years,
	})
}

func (h *LectureHandler) GetCommentsAPI(w http.ResponseWriter, r *http.Request) {
	lectureID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}
	comments, err := h.Repo.GetComments(lectureID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if comments == nil {
		comments = []storage.LectureComment{}
	}
	respondJSON(w, comments)
}

func (h *LectureHandler) PostCommentAPI(w http.ResponseWriter, r *http.Request) {
	lectureID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}
	userID, err := GetUserIDFromContext(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		http.Error(w, "Text is required", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if len(req.Text) > 1000 {
		http.Error(w, "Comment too long", http.StatusBadRequest)
		return
	}

	id, err := h.Repo.AddComment(lectureID, userID, req.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	comments, _ := h.Repo.GetComments(lectureID)
	var added storage.LectureComment
	for _, c := range comments {
		if c.ID == int(id) {
			added = c
			break
		}
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"comment": added,
	})
}
