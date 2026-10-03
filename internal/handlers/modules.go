package handlers

import (
	"database/sql"
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"visualmath/internal/storage"
)

type ModuleHandler struct {
	Repo *storage.ModuleRepo
}

func (h *ModuleHandler) CreateModule(w http.ResponseWriter, r *http.Request) {
	authorID, err := GetUserIDFromContext(r)
	if err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var request struct {
		Title          string      `json:"title"`
		Course         string      `json:"course"`
		Description    string      `json:"description"`
		Type           string      `json:"type"`
		Content        interface{} `json:"content"`
		QuestionsCount int         `json:"questions_count"`
		QuestionsMode  string      `json:"questions_mode"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if request.Title == "" || request.Course == "" || request.Type == "" {
		jsonError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	contentJSON, err := json.Marshal(request.Content)
	if err != nil {
		jsonError(w, "Invalid content format", http.StatusBadRequest)
		return
	}

	moduleID, err := h.Repo.Create(request.Title, request.Course, request.Description, request.Type, string(contentJSON), authorID, request.QuestionsCount, request.QuestionsMode)
	if err != nil {
		jsonError(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "Module created successfully",
		"module": map[string]interface{}{
			"id":              moduleID,
			"title":           request.Title,
			"course":          request.Course,
			"description":     request.Description,
			"type":            request.Type,
			"content":         request.Content,
			"questions_count": request.QuestionsCount,
			"created_at":      time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}

func (h *ModuleHandler) ListModules(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "modules-list.html")
}

func (h *ModuleHandler) CreateModulePage(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, "module-create.html")
}

func (h *ModuleHandler) GetModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")

	m, err := h.Repo.GetByID(moduleID)
	if err != nil {
		http.Error(w, "Module not found", http.StatusNotFound)
		return
	}

	var contentObj interface{}
	if err := json.Unmarshal([]byte(m.ContentJSON), &contentObj); err != nil {
		contentObj = m.ContentJSON
	}

	respondJSON(w, map[string]interface{}{
		"id":              m.ID,
		"title":           m.Title,
		"course":          m.Course,
		"description":     m.Description,
		"type":            m.Type,
		"content":         contentObj,
		"author":          m.Author,
		"created_at":      m.CreatedAt,
		"questions_count": m.QuestionsCount,
		"questions_mode":  m.QuestionsMode,
	})
}

func (h *ModuleHandler) UpdateModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")

	var request struct {
		Title          string      `json:"title"`
		Course         string      `json:"course"`
		Description    string      `json:"description"`
		Type           string      `json:"type"`
		Content        interface{} `json:"content"`
		QuestionsCount int         `json:"questions_count"`
		QuestionsMode  string      `json:"questions_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	contentJSON, err := json.Marshal(request.Content)
	if err != nil {
		http.Error(w, "Invalid content", http.StatusBadRequest)
		return
	}

	if err := h.Repo.Update(moduleID, request.Title, request.Course, request.Description, request.Type, string(contentJSON), request.QuestionsCount, request.QuestionsMode); err != nil {
		http.Error(w, "DB error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]interface{}{"success": true, "message": "Модуль обновлён"})
}

func (h *ModuleHandler) DeleteModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")

	if err := h.Repo.Delete(moduleID); err != nil {
		http.Error(w, "DB error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]interface{}{"success": true, "message": "Модуль удалён"})
}

func (h *ModuleHandler) ListModulesAPI(w http.ResponseWriter, r *http.Request) {
	if _, err := GetUserIDFromContext(r); err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	summaries, err := h.Repo.ListAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	modules := make([]map[string]interface{}, 0, len(summaries))
	for _, m := range summaries {
		modules = append(modules, map[string]interface{}{
			"id":          m.ID,
			"author_id":   m.AuthorID,
			"title":       m.Title,
			"course":      m.Course,
			"description": m.Description,
			"type":        m.Type,
			"author":      m.Author,
			"created_at":  m.CreatedAt,
		})
	}

	respondJSON(w, modules)
}

func (h *ModuleHandler) ViewModulePage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "module-view.html", map[string]string{"ModuleID": chi.URLParam(r, "id")})
}

func (h *ModuleHandler) EditModulePage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "module-edit.html", map[string]string{"ModuleID": chi.URLParam(r, "id")})
}

// AssignModule выдаёт студенту персональный набор вопросов для тестового модуля.
// При повторном запросе возвращает уже зафиксированный набор из БД.
func (h *ModuleHandler) AssignModule(w http.ResponseWriter, r *http.Request) {
	studentID, err := GetUserIDFromContext(r)
	if err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	moduleIDStr := chi.URLParam(r, "id")
	moduleIDInt, err := strconv.Atoi(moduleIDStr)
	if err != nil || moduleIDInt <= 0 {
		jsonError(w, "invalid module id", http.StatusBadRequest)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		jsonError(w, "session_id required", http.StatusBadRequest)
		return
	}

	// Уже есть персональный набор — вернуть его
	existing, err := h.Repo.GetAssignment(sessionID, studentID, moduleIDInt)
	if err == nil {
		var questions interface{}
		json.Unmarshal([]byte(existing), &questions)
		respondJSON(w, map[string]interface{}{"questions": questions})
		return
	}
	if err != sql.ErrNoRows {
		jsonError(w, "db error", http.StatusInternalServerError)
		return
	}

	m, err := h.Repo.GetByID(moduleIDStr)
	if err != nil {
		jsonError(w, "module not found", http.StatusNotFound)
		return
	}

	var allQuestions []map[string]interface{}
	if err := json.Unmarshal([]byte(m.ContentJSON), &allQuestions); err != nil || len(allQuestions) == 0 {
		jsonError(w, "module has no questions", http.StatusBadRequest)
		return
	}

	var selected []map[string]interface{}

	mode := m.QuestionsMode
	if mode == "" {
		mode = "per_student"
	}

	count := m.QuestionsCount
	if count <= 0 || count > len(allQuestions) {
		count = len(allQuestions)
	}

	if mode == "same_for_all" {
		// Ищем набор, уже выданный любому студенту в этой сессии для этого модуля
		sharedJSON, sharedErr := h.Repo.GetSharedAssignment(sessionID, moduleIDInt)
		if sharedErr == nil {
			// Копируем шаблон этому студенту
			h.Repo.SaveAssignment(sessionID, studentID, moduleIDInt, sharedJSON)
			var questions interface{}
			json.Unmarshal([]byte(sharedJSON), &questions)
			respondJSON(w, map[string]interface{}{"questions": questions})
			return
		}
		if sharedErr != sql.ErrNoRows {
			jsonError(w, "db error reading shared template", http.StatusInternalServerError)
			return
		}
		// Первый студент — генерируем набор (он станет шаблоном для остальных)
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		shuffled := make([]map[string]interface{}, len(allQuestions))
		copy(shuffled, allQuestions)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		selected = shuffled[:count]
		// Для режима same_for_all ответы не перемешиваем — порядок одинаков для всех
	} else {
		// per_student: полностью случайный набор и порядок ответов
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(allQuestions), func(i, j int) { allQuestions[i], allQuestions[j] = allQuestions[j], allQuestions[i] })
		selected = allQuestions[:count]
		for i, q := range selected {
			selected[i] = shuffleAnswers(q, rng)
		}
	}

	questionsJSON, err := json.Marshal(selected)
	if err != nil {
		jsonError(w, "marshal error", http.StatusInternalServerError)
		return
	}

	h.Repo.SaveAssignment(sessionID, studentID, moduleIDInt, string(questionsJSON))
	respondJSON(w, map[string]interface{}{"questions": selected})
}

func shuffleAnswers(q map[string]interface{}, rng *rand.Rand) map[string]interface{} {
	answers, ok := q["answers"].([]interface{})
	if !ok || len(answers) == 0 {
		return q
	}

	perm := rng.Perm(len(answers))

	newAnswers := make([]interface{}, len(answers))
	for newIdx, oldIdx := range perm {
		newAnswers[newIdx] = answers[oldIdx]
	}

	// Обратная карта: oldIdx → newIdx
	reverse := make(map[int]int, len(perm))
	for newIdx, oldIdx := range perm {
		reverse[oldIdx] = newIdx
	}

	var newCorrect []int
	switch c := q["correct"].(type) {
	case []interface{}:
		for _, v := range c {
			if oldIdx, ok := toIntVal(v); ok {
				newCorrect = append(newCorrect, reverse[oldIdx])
			}
		}
	case float64:
		newCorrect = []int{reverse[int(c)]}
	}

	result := make(map[string]interface{}, len(q))
	for k, v := range q {
		result[k] = v
	}
	result["answers"] = newAnswers
	result["correct"] = newCorrect
	return result
}

func toIntVal(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

