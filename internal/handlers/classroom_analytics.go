package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"
	"visualmath/internal/auth"
	"visualmath/internal/storage"
)

// SubjectAnalyticsPage — страница со статистикой по предмету (только преподаватель).
func (h *ClassroomHandler) SubjectAnalyticsPage(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok || claims.UserType != "teacher" {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	isTeacher, _ := h.Repo.IsTeacherOfSubject(subjectID, claims.UserID)
	if !isTeacher {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	subject, err := h.Repo.GetSubjectByID(subjectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	analytics, err := h.Repo.GetSubjectAnalytics(subjectID)
	if err != nil {
		http.Error(w, "Ошибка аналитики", http.StatusInternalServerError)
		return
	}
	execTemplate(w, "subject-analytics.html", map[string]interface{}{
		"Subject":   subject,
		"Analytics": analytics,
	})
}

// SubjectGradebookXLSX — сводная ведомость по всему предмету: студенты × задания.
func (h *ClassroomHandler) SubjectGradebookXLSX(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok || claims.UserType != "teacher" {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	isTeacher, _ := h.Repo.IsTeacherOfSubject(subjectID, claims.UserID)
	if !isTeacher {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	assignments, _ := h.Repo.GetAssignmentsBySubject(subjectID)
	students, _ := h.Repo.GetStudentsBySubject(subjectID)
	grades, _ := h.Repo.GetGradesForSubject(subjectID)

	data := buildGradebookXLSX(assignments, students, grades)
	fname := fmt.Sprintf("gradebook_subject_%d.xlsx", subjectID)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fname+"\"")
	w.Write(data)
}

// xlsxSet записывает значение в ячейку (col, row — 1-based). Число пишется
// числом, строка — строкой; excelize сам экранирует спецсимволы.
func xlsxSet(f *excelize.File, sheet string, col, row int, v interface{}) {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	f.SetCellValue(sheet, cell, v)
}

// xlsxStyleHeader делает первую строку жирной и замораживает её при прокрутке.
func xlsxStyleHeader(f *excelize.File, sheet string, cols int) {
	last, _ := excelize.CoordinatesToCellName(cols, 1)
	if style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}}); err == nil {
		f.SetCellStyle(sheet, "A1", last, style)
	}
	f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
}

// buildGradebookXLSX строит матрицу: строки — студенты, колонки — задания + «Итого».
// Задания идут в хронологическом порядке (GetAssignmentsBySubject отдаёт DESC,
// поэтому разворачиваем, чтобы первое задание было слева).
func buildGradebookXLSX(assignments []storage.Assignment, students []storage.SubjectStudent, grades []storage.GradeCell) []byte {
	// разворачиваем порядок заданий: старые слева
	ordered := make([]storage.Assignment, len(assignments))
	for i, a := range assignments {
		ordered[len(assignments)-1-i] = a
	}

	// grade[studentID][assignmentID] = *int
	gmap := map[int]map[int]*int{}
	for _, g := range grades {
		if gmap[g.StudentID] == nil {
			gmap[g.StudentID] = map[int]*int{}
		}
		gmap[g.StudentID][g.AssignmentID] = g.Grade
	}

	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Ведомость"
	f.SetSheetName("Sheet1", sheet)

	// Заголовок: Студент | Группа | <задания...> | Итого
	headers := []string{"Студент", "Группа"}
	for _, a := range ordered {
		h := a.Title
		if a.MaxScore > 0 {
			h = fmt.Sprintf("%s (макс. %d)", a.Title, a.MaxScore)
		}
		headers = append(headers, h)
	}
	headers = append(headers, "Итого")
	for i, h := range headers {
		xlsxSet(f, sheet, i+1, 1, h)
	}

	totalCol := 3 + len(ordered)

	// Строки студентов
	for si, s := range students {
		rowNum := si + 2
		xlsxSet(f, sheet, 1, rowNum, s.FullName)
		xlsxSet(f, sheet, 2, rowNum, s.Group)
		total := 0
		hasAny := false
		for i, a := range ordered {
			col := 3 + i
			if g, ok := gmap[s.ID][a.ID]; ok && g != nil {
				xlsxSet(f, sheet, col, rowNum, *g)
				total += *g
				hasAny = true
			} else {
				xlsxSet(f, sheet, col, rowNum, "—")
			}
		}
		if hasAny {
			xlsxSet(f, sheet, totalCol, rowNum, total)
		} else {
			xlsxSet(f, sheet, totalCol, rowNum, "—")
		}
	}

	f.SetColWidth(sheet, "A", "A", 28)
	f.SetColWidth(sheet, "B", "B", 14)
	xlsxStyleHeader(f, sheet, len(headers))

	buf, _ := f.WriteToBuffer()
	return buf.Bytes()
}
