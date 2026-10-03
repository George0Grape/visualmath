package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type SlidesHandler struct {
	DB *sql.DB
}

// stripCorrectAnswers убирает поле correct из вопросов перед отправкой студенту.
// question-модули всегда одиночный выбор, test — зависит от количества правильных ответов.
func stripCorrectAnswers(content interface{}, moduleType string) interface{} {
	questions, ok := content.([]interface{})
	if !ok {
		return content
	}
	result := make([]interface{}, 0, len(questions))
	for _, q := range questions {
		qmap, ok := q.(map[string]interface{})
		if !ok {
			result = append(result, q)
			continue
		}
		clean := make(map[string]interface{}, len(qmap))
		for k, v := range qmap {
			if k != "correct" {
				clean[k] = v
			}
		}
		if moduleType == "question" {
			clean["is_multi"] = false
		} else if correct, exists := qmap["correct"]; exists {
			switch c := correct.(type) {
			case []interface{}:
				clean["is_multi"] = len(c) > 1
			default:
				clean["is_multi"] = false
			}
		}
		result = append(result, clean)
	}
	return result
}

func (h *SlidesHandler) StudentSlidesPage(w http.ResponseWriter, r *http.Request) {
	lectureIDInt, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || lectureIDInt <= 0 {
		http.Error(w, "Invalid lecture ID", http.StatusBadRequest)
		return
	}
	lectureID := strconv.Itoa(lectureIDInt)

	type LectureData struct {
		ID      int                      `json:"id"`
		Title   string                   `json:"title"`
		Modules []map[string]interface{} `json:"modules"`
	}
	var lectureJSON string
	var lectureTitle string
	row := h.DB.QueryRow("SELECT id, title FROM lectures WHERE id = ?", lectureIDInt)
	if err := row.Scan(&lectureIDInt, &lectureTitle); err == nil {
		rows, err := h.DB.Query(`
            SELECT m.id, m.title, m.module_type, COALESCE(m.content,'')
            FROM lecture_modules lm
            JOIN modules m ON lm.module_id = m.id
            WHERE lm.lecture_id = ?
            ORDER BY lm.module_order ASC
        `, lectureID)
		if err == nil {
			defer rows.Close()
			var modules []map[string]interface{}
			for rows.Next() {
				var mid int
				var mtitle, mtype, mcontent string
				if err := rows.Scan(&mid, &mtitle, &mtype, &mcontent); err == nil {
					var contentObj interface{}
					if err := json.Unmarshal([]byte(mcontent), &contentObj); err != nil {
						contentObj = mcontent
					}
					// правильные ответы студенту не показываем
					if mtype == "test" || mtype == "question" {
						contentObj = stripCorrectAnswers(contentObj, mtype)
					}
					modules = append(modules, map[string]interface{}{
						"id": mid, "title": mtitle, "type": mtype, "content": contentObj,
					})
				}
			}
			ld := LectureData{ID: lectureIDInt, Title: lectureTitle, Modules: modules}
			if b, err := json.Marshal(ld); err == nil {
				lectureJSON = string(b)
			}
		}
	}
	if lectureJSON == "" {
		// lectureIDInt уже проверен как число — строковая конкатенация безопасна
		lectureJSON = fmt.Sprintf(`{"id":%d,"title":"","modules":[]}`, lectureIDInt)
	}

	execTemplate(w, "student-slides.html", map[string]interface{}{
		"LectureID":   lectureID,
		"LectureData": template.JS(lectureJSON),
	})
}
