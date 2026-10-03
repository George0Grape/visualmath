package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"
	"visualmath/internal/auth"
	"visualmath/internal/storage"
)

// SubjectCard — что показываем на странице предметов: сам предмет + ближайшее задание
type SubjectCard struct {
	storage.SubjectWithRole
	NearestTitle   string
	NearestDay     string // "пятница" или пусто
	NearestTime    string // "08:30" или пусто
	HasNearest     bool
	HasDeadline    bool
}

var ruWeekdays = [7]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}

type ClassroomHandler struct {
	Repo    *storage.ClassroomRepo
	BaseDir string // корень проекта, uploads/ находится внутри
}

func NewClassroomHandler(repo *storage.ClassroomRepo, baseDir string) *ClassroomHandler {
	os.MkdirAll(filepath.Join(baseDir, "uploads", "submissions"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "uploads", "conditions"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "uploads", "variants"), 0755)
	return &ClassroomHandler{Repo: repo, BaseDir: baseDir}
}

func generateInviteCode() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func (h *ClassroomHandler) SubjectsPage(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	userType, _ := GetUserTypeFromContext(r)

	var subjects []storage.SubjectWithRole
	if userType == "teacher" {
		subjects, err = h.Repo.GetSubjectsForTeacher(userID)
	} else {
		subjects, err = h.Repo.GetSubjectsForStudent(userID)
	}
	if err != nil {
		log.Printf("SubjectsPage: %v", err)
	}

	// для каждого предмета подтягиваем ближайшее задание чтобы показать его в карточке
	cards := make([]SubjectCard, 0, len(subjects))
	for _, s := range subjects {
		card := SubjectCard{SubjectWithRole: s}
		if nearest, _ := h.Repo.GetNearestAssignment(s.ID); nearest != nil {
			card.HasNearest = true
			card.NearestTitle = nearest.Title
			if nearest.Deadline != nil {
				dl := *nearest.Deadline
				card.HasDeadline = true
				card.NearestDay = ruWeekdays[dl.Weekday()]
				card.NearestTime = dl.Format("15:04")
			}
		}
		cards = append(cards, card)
	}

	execTemplate(w, "subjects.html", map[string]interface{}{
		"Subjects": cards,
		"UserType": userType,
	})
}

func (h *ClassroomHandler) CreateSubjectPage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "subject-create.html", nil)
}

func (h *ClassroomHandler) CreateSubjectSubmit(w http.ResponseWriter, r *http.Request) {
	teacherID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	if title == "" {
		http.Error(w, "Название обязательно", http.StatusBadRequest)
		return
	}
	code := generateInviteCode()
	id, err := h.Repo.CreateSubject(title, description, code, teacherID)
	if err != nil {
		http.Error(w, "Ошибка создания предмета", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/classroom/subjects/%d", id), http.StatusFound)
}

func (h *ClassroomHandler) SubjectPage(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	subject, err := h.Repo.GetSubjectByID(subjectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if userType == "teacher" {
		ok, _ := h.Repo.IsTeacherOfSubject(subjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	} else {
		ok, _ := h.Repo.IsStudentOfSubject(subjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	}

	assignments, err := h.Repo.GetAssignmentsBySubject(subjectID)
	if err != nil {
		log.Printf("SubjectPage: %v", err)
	}

	// Студентам не показываем задания, время выдачи которых ещё не наступило
	if userType != "teacher" {
		visible := assignments[:0]
		for _, a := range assignments {
			if !a.IsScheduled() {
				visible = append(visible, a)
			}
		}
		assignments = visible
	}

	isOwner := subject.CreatedBy == userID

	execTemplate(w, "subject-detail.html", map[string]interface{}{
		"Subject":     subject,
		"Assignments": assignments,
		"UserType":    userType,
		"UserID":      userID,
		"IsOwner":     isOwner,
	})
}

func (h *ClassroomHandler) StudentFilesAPI(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	files, err := h.Repo.GetStudentFilesForSubject(subjectID, userID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	type fileItem struct {
		ID              int    `json:"id"`
		AssignmentTitle string `json:"assignment_title"`
		Status          string `json:"status"`
		SubmittedAt     string `json:"submitted_at"`
		DownloadURL     string `json:"download_url"`
		AnnotatedURL    string `json:"annotated_url,omitempty"`
	}
	result := make([]fileItem, 0, len(files))
	for _, f := range files {
		item := fileItem{
			ID:              f.ID,
			AssignmentTitle: f.AssignmentTitle,
			Status:          f.Status,
			SubmittedAt:     f.SubmittedAt.Format("02.01.2006 15:04"),
			DownloadURL:     fmt.Sprintf("/classroom/submissions/%d/download", f.ID),
		}
		if f.AnnotatedFilePath != "" {
			item.AnnotatedURL = fmt.Sprintf("/classroom/submissions/%d/annotated", f.ID)
		}
		result = append(result, item)
	}

	respondJSON(w, result)
}

func (h *ClassroomHandler) DeleteSubject(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	subject, err := h.Repo.GetSubjectByID(subjectID)
	if err != nil || subject.CreatedBy != userID {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	h.Repo.DeleteSubject(subjectID)
	http.Redirect(w, r, "/classroom/subjects", http.StatusFound)
}

func (h *ClassroomHandler) JoinSubjectPage(w http.ResponseWriter, r *http.Request) {
	execTemplate(w, "subject-join.html", nil)
}

func (h *ClassroomHandler) JoinSubjectSubmit(w http.ResponseWriter, r *http.Request) {
	studentID, err := GetUserIDFromContext(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("invite_code")))
	subject, err := h.Repo.GetSubjectByInviteCode(code)
	if err != nil {
		execTemplate(w, "subject-join.html", map[string]interface{}{"Error": "Предмет с таким кодом не найден"})
		return
	}
	h.Repo.JoinSubject(subject.ID, studentID)
	http.Redirect(w, r, fmt.Sprintf("/classroom/subjects/%d", subject.ID), http.StatusFound)
}

func (h *ClassroomHandler) CreateAssignmentPage(w http.ResponseWriter, r *http.Request) {
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	execTemplate(w, "assignment-create.html", map[string]interface{}{"SubjectID": subjectID, "IsEdit": false})
}

func (h *ClassroomHandler) CreateAssignmentSubmit(w http.ResponseWriter, r *http.Request) {
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	teacherID, _ := GetUserIDFromContext(r)

	ok, _ := h.Repo.IsTeacherOfSubject(subjectID, teacherID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	r.ParseMultipartForm(32 << 20)
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	deadlineStr := r.FormValue("deadline")

	maxScore := 100
	if ms, err := strconv.Atoi(r.FormValue("max_score")); err == nil && ms > 0 {
		maxScore = ms
	}

	var deadline *time.Time
	if deadlineStr != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", deadlineStr, time.Local)
		if err == nil {
			deadline = &t
		}
	}

	var publishAt *time.Time
	if s := r.FormValue("publish_at"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local); err == nil {
			publishAt = &t
		}
	}

	// файл условия необязателен — задание можно создать без него
	conditionFile := ""
	file, header, err := r.FormFile("condition_file")
	if err == nil {
		defer file.Close()
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
			http.Error(w, "Только PDF файлы", http.StatusBadRequest)
			return
		}
		if !looksLikePDF(file) {
			http.Error(w, "Файл не является PDF", http.StatusBadRequest)
			return
		}
		fname := fmt.Sprintf("cond_%d_%d_%s", subjectID, time.Now().UnixNano(), filepath.Base(header.Filename))
		dst := filepath.Join(h.BaseDir, "uploads", "conditions", fname)
		if out, err := os.Create(dst); err == nil {
			io.Copy(out, file)
			out.Close()
			conditionFile = fname
		}
	}

	strictDeadline := r.FormValue("strict_deadline") == "1"

	gradingMode := r.FormValue("grading_mode")
	if gradingMode != "per_task" {
		gradingMode = "total"
	}

	// В режиме per_task пересчитываем maxScore как сумму задач
	var tasks []storage.AssignmentTask
	if gradingMode == "per_task" {
		taskLabels := r.Form["task_label[]"]
		taskScores := r.Form["task_score[]"]
		total := 0
		for i, label := range taskLabels {
			score := 0
			if i < len(taskScores) {
				score, _ = strconv.Atoi(taskScores[i])
			}
			if score < 0 {
				score = 0
			}
			tasks = append(tasks, storage.AssignmentTask{
				TaskNumber: i + 1,
				Label:      strings.TrimSpace(label),
				MaxScore:   score,
			})
			total += score
		}
		if total > 0 {
			maxScore = total
		}
	}

	allowComments := r.FormValue("allow_comments") == "1"

	id, err := h.Repo.CreateAssignment(subjectID, title, description, conditionFile, deadline, maxScore, strictDeadline, gradingMode, allowComments, publishAt)
	if err != nil {
		http.Error(w, "Ошибка создания задания", http.StatusInternalServerError)
		return
	}
	if gradingMode == "per_task" && len(tasks) > 0 {
		h.Repo.CreateAssignmentTasks(id, tasks)
	}

	// Раздача вариантов
	if r.FormValue("variant_distribution") == "1" {
		files := r.MultipartForm.File["variant_files[]"]
		var variantPaths []string
		varDir := filepath.Join(h.BaseDir, "uploads", "variants")
		os.MkdirAll(varDir, 0755)
		for i, fh := range files {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			if !looksLikePDF(f) { // пропускаем не-PDF варианты
				f.Close()
				continue
			}
			fname := fmt.Sprintf("var_%d_%d_%d_%s", id, i+1, time.Now().UnixNano(), filepath.Base(fh.Filename))
			dst := filepath.Join(varDir, fname)
			if out, err2 := os.Create(dst); err2 == nil {
				io.Copy(out, f)
				out.Close()
				variantPaths = append(variantPaths, fname)
			}
			f.Close()
		}
		if len(variantPaths) > 0 {
			h.Repo.SaveVariants(id, variantPaths)
			h.Repo.AssignVariantsToStudents(id, variantPaths)
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/classroom/assignments/%d", id), http.StatusFound)
}

func (h *ClassroomHandler) EditAssignmentPage(w http.ResponseWriter, r *http.Request) {
	teacherID, _ := GetUserIDFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, teacherID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	tasks, _ := h.Repo.GetTasksByAssignment(assignmentID)
	assignment.Tasks = tasks
	execTemplate(w, "assignment-create.html", map[string]interface{}{
		"SubjectID":  assignment.SubjectID,
		"IsEdit":    true,
		"Assignment": assignment,
	})
}

func (h *ClassroomHandler) EditAssignmentSubmit(w http.ResponseWriter, r *http.Request) {
	teacherID, _ := GetUserIDFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, teacherID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	r.ParseMultipartForm(32 << 20)
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	deadlineStr := r.FormValue("deadline")
	maxScore := 100
	if ms, err := strconv.Atoi(r.FormValue("max_score")); err == nil && ms > 0 {
		maxScore = ms
	}
	var deadline *time.Time
	if deadlineStr != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", deadlineStr, time.Local)
		if err == nil {
			deadline = &t
		}
	}
	var publishAt *time.Time
	if s := r.FormValue("publish_at"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local); err == nil {
			publishAt = &t
		}
	}
	conditionFile := assignment.ConditionFile
	file, header, err := r.FormFile("condition_file")
	if err == nil {
		defer file.Close()
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
			http.Error(w, "Только PDF файлы", http.StatusBadRequest)
			return
		}
		if !looksLikePDF(file) {
			http.Error(w, "Файл не является PDF", http.StatusBadRequest)
			return
		}
		fname := fmt.Sprintf("cond_%d_%d_%s", assignment.SubjectID, time.Now().UnixNano(), filepath.Base(header.Filename))
		dst := filepath.Join(h.BaseDir, "uploads", "conditions", fname)
		if out, err2 := os.Create(dst); err2 == nil {
			io.Copy(out, file)
			out.Close()
			conditionFile = fname
		}
	}
	strictDeadline := r.FormValue("strict_deadline") == "1"
	gradingMode := r.FormValue("grading_mode")
	if gradingMode != "per_task" {
		gradingMode = "total"
	}
	var tasks []storage.AssignmentTask
	if gradingMode == "per_task" {
		taskLabels := r.Form["task_label[]"]
		taskScores := r.Form["task_score[]"]
		total := 0
		for i, label := range taskLabels {
			score := 0
			if i < len(taskScores) {
				score, _ = strconv.Atoi(taskScores[i])
			}
			if score < 0 {
				score = 0
			}
			tasks = append(tasks, storage.AssignmentTask{TaskNumber: i + 1, Label: strings.TrimSpace(label), MaxScore: score})
			total += score
		}
		if total > 0 {
			maxScore = total
		}
	}
	allowComments := r.FormValue("allow_comments") == "1"

	if err := h.Repo.UpdateAssignment(assignmentID, title, description, conditionFile, deadline, maxScore, strictDeadline, gradingMode, allowComments, publishAt); err != nil {
		http.Error(w, "Ошибка обновления задания", http.StatusInternalServerError)
		return
	}
	if gradingMode == "per_task" && len(tasks) > 0 {
		h.Repo.DB.Exec(`DELETE FROM assignment_tasks WHERE assignment_id=?`, assignmentID)
		h.Repo.CreateAssignmentTasks(assignmentID, tasks)
	}
	http.Redirect(w, r, fmt.Sprintf("/classroom/assignments/%d", assignmentID), http.StatusFound)
}

func (h *ClassroomHandler) AssignmentPage(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tasks, _ := h.Repo.GetTasksByAssignment(assignmentID)
	assignment.Tasks = tasks

	if userType == "teacher" {
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
		submissions, _ := h.Repo.GetSubmissionsByAssignment(assignmentID)
		if assignment.Deadline != nil {
			for i := range submissions {
				if submissions[i].SubmittedAt.After(*assignment.Deadline) {
					submissions[i].IsLate = true
				}
			}
		}
		execTemplate(w, "assignment-teacher.html", map[string]interface{}{
			"Assignment":  assignment,
			"Submissions": submissions,
		})
	} else {
		ok, _ := h.Repo.IsStudentOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
		// Задание ещё не выдано — для студента его как будто не существует
		if assignment.IsScheduled() {
			http.NotFound(w, r)
			return
		}
		submission, _ := h.Repo.GetSubmissionByStudentAndAssignment(assignmentID, userID)

		var taskGrades []storage.SubmissionTaskGrade
		deadlinePassed := assignment.Deadline != nil && time.Now().After(*assignment.Deadline)
		if submission != nil && assignment.GradingMode == "per_task" {
			taskGrades, _ = h.Repo.GetTaskGrades(submission.ID)
		}
		if submission != nil && assignment.Deadline != nil && submission.SubmittedAt.After(*assignment.Deadline) {
			submission.IsLate = true
		}
		variantFile, variantIdx, _ := h.Repo.GetStudentVariant(assignmentID, userID)
		if variantFile == "" {
			variantFile, variantIdx, _ = h.Repo.AutoAssignVariant(assignmentID, userID)
		}
		execTemplate(w, "assignment-student.html", map[string]interface{}{
			"Assignment":     assignment,
			"Submission":     submission,
			"TaskGrades":     taskGrades,
			"DeadlinePassed": deadlinePassed,
			"VariantFile":    variantFile,
			"VariantIdx":     variantIdx,
			"AllowComments":  assignment.AllowComments,
		})
	}
}

func (h *ClassroomHandler) DeleteAssignment(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	// удаляем файлы с диска перед удалением из БД
	submissions, _ := h.Repo.GetSubmissionsByAssignment(assignmentID)
	for _, sub := range submissions {
		if sub.FilePath != "" {
			os.Remove(filepath.Join(h.BaseDir, "uploads", "submissions", sub.FilePath))
		}
		if sub.AnnotatedFilePath != "" {
			os.Remove(filepath.Join(h.BaseDir, "uploads", "submissions", sub.AnnotatedFilePath))
		}
	}
	if assignment.ConditionFile != "" {
		os.Remove(filepath.Join(h.BaseDir, "uploads", "conditions", assignment.ConditionFile))
	}

	h.Repo.DeleteAssignment(assignmentID)

	respondJSON(w, map[string]interface{}{
		"success":    true,
		"subject_id": assignment.SubjectID,
	})
}

func (h *ClassroomHandler) SubmitWork(w http.ResponseWriter, r *http.Request) {
	studentID, _ := GetUserIDFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsStudentOfSubject(assignment.SubjectID, studentID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	// задание ещё не выдано — сдавать нечего (защита от прямого POST в обход 404 страницы)
	if assignment.IsScheduled() {
		http.NotFound(w, r)
		return
	}

	if assignment.StrictDeadline && assignment.Deadline != nil && time.Now().After(*assignment.Deadline) {
		http.Error(w, "Срок сдачи истёк", http.StatusForbidden)
		return
	}

	existing, _ := h.Repo.GetSubmissionByStudentAndAssignment(assignmentID, studentID)
	if existing != nil {
		http.Error(w, "Работа уже сдана", http.StatusBadRequest)
		return
	}

	r.ParseMultipartForm(32 << 20)
	file, header, err := r.FormFile("pdf_file")
	if err != nil {
		http.Error(w, "Файл не загружен", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
		http.Error(w, "Только PDF файлы", http.StatusBadRequest)
		return
	}
	if !looksLikePDF(file) {
		http.Error(w, "Файл не является PDF", http.StatusBadRequest)
		return
	}

	fname := fmt.Sprintf("sub_%d_%d_%d.pdf", assignmentID, studentID, time.Now().UnixNano())
	dst := filepath.Join(h.BaseDir, "uploads", "submissions", fname)
	out, err := os.Create(dst)
	if err != nil {
		http.Error(w, "Ошибка сохранения файла", http.StatusInternalServerError)
		return
	}
	io.Copy(out, file)
	out.Close()

	// Переводим PDF в оттенки серого без изменения разрешения (in-place).
	// При любой ошибке оригинал остаётся нетронутым — работу студента не теряем.
	processPDFGrayscale(dst)

	_, err = h.Repo.CreateSubmission(assignmentID, studentID, fname)
	if err != nil {
		http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/classroom/assignments/%d", assignmentID), http.StatusFound)
}

// looksLikePDF проверяет сигнатуру %PDF- в начале файла и возвращает курсор в
// начало (чтобы последующее io.Copy не потеряло прочитанные байты). Защита от
// подмены типа: расширения .pdf недостаточно — содержимое может быть любым.
func looksLikePDF(f io.ReadSeeker) bool {
	buf := make([]byte, 5)
	n, _ := io.ReadFull(f, buf)
	f.Seek(0, io.SeekStart)
	return n == 5 && string(buf) == "%PDF-"
}

func (h *ClassroomHandler) DownloadSubmission(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if userType == "student" && sub.StudentID != userID {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	if userType == "teacher" {
		assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
		if aerr != nil || assignment == nil {
			http.NotFound(w, r)
			return
		}
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	}

	fpath := filepath.Join(h.BaseDir, "uploads", "submissions", sub.FilePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sub.FilePath))
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeFile(w, r, fpath)
}

func (h *ClassroomHandler) DownloadAnnotated(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sub.StudentID != userID {
		assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
		if aerr != nil || assignment == nil {
			http.NotFound(w, r)
			return
		}
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	}

	filePath := sub.AnnotatedFilePath
	if filePath == "" {
		filePath = sub.FilePath
	}
	fpath := filepath.Join(h.BaseDir, "uploads", "submissions", filePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", sub.FilePath))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, fpath)
}

func (h *ClassroomHandler) DownloadVariant(w http.ResponseWriter, r *http.Request) {
	studentID, _ := GetUserIDFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsStudentOfSubject(assignment.SubjectID, studentID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	// запланированное задание студенту недоступно даже по прямой ссылке
	if assignment.IsScheduled() {
		http.NotFound(w, r)
		return
	}
	filePath, _, err := h.Repo.GetStudentVariant(assignmentID, studentID)
	if err != nil || filePath == "" {
		http.NotFound(w, r)
		return
	}
	fpath := filepath.Join(h.BaseDir, "uploads", "variants", filePath)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, fpath)
}

func (h *ClassroomHandler) DownloadCondition(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil || assignment.ConditionFile == "" {
		http.NotFound(w, r)
		return
	}

	// только участники предмета могут скачивать условие
	if userType == "teacher" {
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	} else {
		ok, _ := h.Repo.IsStudentOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
		// запланированное задание студенту недоступно даже по прямой ссылке
		if assignment.IsScheduled() {
			http.NotFound(w, r)
			return
		}
	}

	fpath := filepath.Join(h.BaseDir, "uploads", "conditions", assignment.ConditionFile)
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeFile(w, r, fpath)
}

func (h *ClassroomHandler) AnnotatePage(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	assignment, err := h.Repo.GetAssignmentByID(sub.AssignmentID)
	if err != nil || assignment == nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	tasks, _ := h.Repo.GetTasksByAssignment(assignment.ID)
	assignment.Tasks = tasks
	existingGrades, _ := h.Repo.GetTaskGrades(submissionID)
	// карта задача→балл для предзаполнения полей оценивания в редакторе
	taskScores := map[int]int{}
	for _, g := range existingGrades {
		taskScores[g.TaskNumber] = g.Score
	}
	tsJSON, _ := json.Marshal(taskScores)
	execTemplate(w, "annotate.html", map[string]interface{}{
		"Submission":     sub,
		"Assignment":     assignment,
		"ExistingGrades": existingGrades,
		"TaskScoresJSON": template.JS(tsJSON),
	})
}

type PinData struct {
	Page    int     `json:"page"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Comment string  `json:"comment"`
}

type AnnotationItem struct {
	Type    string  `json:"type"`    // "underline" | "highlight"
	Page    int     `json:"page"`
	X1      float64 `json:"x1"`
	Y1      float64 `json:"y1"`
	X2      float64 `json:"x2"`
	Y2      float64 `json:"y2"`
	Color   string  `json:"color"`
	Comment string  `json:"comment"`
}

type AnnotationPayload struct {
	Pages       []PageAnnotations `json:"pages"`
	Comment     string            `json:"comment"`
	Pins        []PinData         `json:"pins"`
	Annotations []AnnotationItem  `json:"annotations"`
}

type PageAnnotations struct {
	Page       int          `json:"page"`
	PageWidth  float64      `json:"page_width"`  // в PDF points; если 0 — подставляем размер A4
	PageHeight float64      `json:"page_height"` // в PDF points; если 0 — подставляем размер A4
	Strokes    []StrokeData `json:"strokes"`
}

type StrokeData struct {
	Tool    string      `json:"tool"`  // "pen", "marker", "underline", "text"
	Color   string      `json:"color"`
	Width   float64     `json:"width"`
	Opacity float64     `json:"opacity"`
	Points  [][2]float64 `json:"points"`
	Text    string      `json:"text"`   // только для tool=text
	X       float64     `json:"x"`
	Y       float64     `json:"y"`
}

func (h *ClassroomHandler) SaveAnnotations(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	assignment, err := h.Repo.GetAssignmentByID(sub.AssignmentID)
	if err != nil || assignment == nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	var payload AnnotationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Сохраняем сырые мазки (как прислал редактор, ДО инжекта подчёркиваний/
	// хайлайтов) — чтобы при повторном открытии редактор восстановил их.
	strokesJSON := "[]"
	if len(payload.Pages) > 0 {
		if b, err := json.Marshal(payload.Pages); err == nil {
			strokesJSON = string(b)
		}
	}

	// Добавляем страницы, у которых есть аннотации/пины, но нет обычных штрихов —
	// иначе их пометки и текст комментариев не попадут в запечённый PDF
	pagesWithStrokes := make(map[int]bool)
	for _, pg := range payload.Pages {
		pagesWithStrokes[pg.Page] = true
	}
	for _, ann := range payload.Annotations {
		if !pagesWithStrokes[ann.Page] {
			payload.Pages = append(payload.Pages, PageAnnotations{Page: ann.Page, PageWidth: 595, PageHeight: 842})
			pagesWithStrokes[ann.Page] = true
		}
	}
	for _, pin := range payload.Pins {
		if !pagesWithStrokes[pin.Page] {
			payload.Pages = append(payload.Pages, PageAnnotations{Page: pin.Page, PageWidth: 595, PageHeight: 842})
			pagesWithStrokes[pin.Page] = true
		}
	}

	// Инжектируем аннотации как визуальные штрихи в PDF
	for i := range payload.Pages {
		for _, ann := range payload.Annotations {
			if ann.Page != payload.Pages[i].Page {
				continue
			}
			switch ann.Type {
			case "underline":
				col := ann.Color
				if col == "" {
					col = "#ef4444"
				}
				payload.Pages[i].Strokes = append(payload.Pages[i].Strokes, StrokeData{
					Tool: "underline", Color: col, Width: 2, Opacity: 1.0,
					Points: [][2]float64{{ann.X1, ann.Y1}, {ann.X2, ann.Y2}},
				})
			case "highlight":
				payload.Pages[i].Strokes = append(payload.Pages[i].Strokes, StrokeData{
					Tool: "highlight", Color: "#fbbf24", Width: 0, Opacity: 0.3,
					Points: [][2]float64{{ann.X1, ann.Y1}, {ann.X2, ann.Y2}},
				})
			}
		}
	}

	// Всегда запекаем из ОРИГИНАЛА: редактор присылает полный набор штрихов
	// (он их подгружает при открытии), поэтому пересборка с нуля идемпотентна —
	// никакого накопления/дублирования поверх прошлого аннотированного файла.
	srcPath := filepath.Join(h.BaseDir, "uploads", "submissions", sub.FilePath)
	annotatedName := fmt.Sprintf("ann_%d_%d.pdf", submissionID, time.Now().UnixNano())
	dstPath := filepath.Join(h.BaseDir, "uploads", "submissions", annotatedName)

	if err := applyAnnotationsToPDF(srcPath, dstPath, payload.Pages, payload.Pins, payload.Annotations); err != nil {
		log.Printf("annotation error: %v", err)
		http.Error(w, "Ошибка создания PDF", http.StatusInternalServerError)
		return
	}

	pinsJSON := "[]"
	if len(payload.Pins) > 0 {
		if b, err := json.Marshal(payload.Pins); err == nil {
			pinsJSON = string(b)
		}
	}

	annotationsJSON := "[]"
	if len(payload.Annotations) > 0 {
		if b, err := json.Marshal(payload.Annotations); err == nil {
			annotationsJSON = string(b)
		}
	}

	if err := h.Repo.SaveAnnotatedFile(submissionID, annotatedName, payload.Comment, pinsJSON, annotationsJSON, strokesJSON); err != nil {
		http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
		return
	}

	// прошлый аннотированный файл больше не нужен — иначе при каждом
	// пересохранении на диске копятся осиротевшие ann_*.pdf
	if sub.AnnotatedFilePath != "" && sub.AnnotatedFilePath != annotatedName && sub.AnnotatedFilePath != sub.FilePath {
		os.Remove(filepath.Join(h.BaseDir, "uploads", "submissions", sub.AnnotatedFilePath))
	}

	respondJSON(w, map[string]string{"status": "ok"})
}

func (h *ClassroomHandler) GetSubmissionPins(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// пины видят только сам студент и преподаватель предмета
	if sub.StudentID != userID {
		assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
		if aerr != nil || assignment == nil {
			http.NotFound(w, r)
			return
		}
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	}

	pins, err := h.Repo.GetPins(submissionID)
	if err != nil || pins == "" {
		pins = "[]"
	}
	annotations, err := h.Repo.GetAnnotations(submissionID)
	if err != nil || annotations == "" {
		annotations = "[]"
	}
	strokes, err := h.Repo.GetStrokes(submissionID)
	if err != nil || strokes == "" {
		strokes = "[]"
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"pins":%s,"annotations":%s,"strokes":%s}`, pins, annotations, strokes)
}

func (h *ClassroomHandler) SubmissionView(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sub.StudentID != userID {
		assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
		if aerr != nil || assignment == nil {
			http.NotFound(w, r)
			return
		}
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	} else {
		// студент открыл работу — снимаем бейдж
		h.Repo.MarkSubmissionSeenByStudent(submissionID)
	}

	execTemplate(w, "submission-view.html", map[string]interface{}{
		"SubmissionID": submissionID,
		"AssignmentID": sub.AssignmentID,
	})
}

func (h *ClassroomHandler) GradeSubmission(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	assignment, err := h.Repo.GetAssignmentByID(sub.AssignmentID)
	if err != nil || assignment == nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	var req struct {
		Grade      int            `json:"grade"`
		TaskGrades map[string]int `json:"task_grades"` // task_number -> score, для per_task
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	finalGrade := req.Grade
	if assignment.GradingMode == "per_task" && len(req.TaskGrades) > 0 {
		grades := make(map[int]int)
		sum := 0
		for k, v := range req.TaskGrades {
			n, _ := strconv.Atoi(k)
			if v < 0 { v = 0 }
			grades[n] = v
			sum += v
		}
		if sum > assignment.MaxScore {
			sum = assignment.MaxScore
		}
		finalGrade = sum
		if err := h.Repo.SaveTaskGrades(submissionID, grades); err != nil {
			http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
			return
		}
	} else {
		if finalGrade < 0 || finalGrade > assignment.MaxScore {
			jsonError(w, fmt.Sprintf("Оценка должна быть от 0 до %d", assignment.MaxScore), http.StatusBadRequest)
			return
		}
	}

	if err := h.Repo.SaveGrade(submissionID, finalGrade); err != nil {
		http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{"grade": finalGrade})
}

func (h *ClassroomHandler) BadgesAPI(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserIDFromContext(r)
	if err != nil {
		respondJSON(w, map[string]int{"count": 0})
		return
	}
	userType, _ := GetUserTypeFromContext(r)

	var count int
	if userType == "teacher" {
		count = h.Repo.CountUnreviewedForTeacher(userID)
	} else {
		count = h.Repo.CountNewReviewsForStudent(userID)
	}

	respondJSON(w, map[string]int{"count": count})
}

func (h *ClassroomHandler) ServeSubmissionPDF(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	assignment, err := h.Repo.GetAssignmentByID(sub.AssignmentID)
	if err != nil || assignment == nil {
		http.NotFound(w, r)
		return
	}
	ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	fpath := filepath.Join(h.BaseDir, "uploads", "submissions", sub.FilePath)
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeFile(w, r, fpath)
}

func (h *ClassroomHandler) GetSubjectStudents(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	isTeacher, _ := h.Repo.IsTeacherOfSubject(subjectID, claims.UserID)
	if !isTeacher {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	students, err := h.Repo.GetStudentsBySubject(subjectID)
	if err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}
	respondJSON(w, students)
}

func (h *ClassroomHandler) RemoveStudentFromSubject(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	studentID, _ := strconv.Atoi(chi.URLParam(r, "studentId"))
	isTeacher, _ := h.Repo.IsTeacherOfSubject(subjectID, claims.UserID)
	if !isTeacher {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	if err := h.Repo.RemoveStudentFromSubject(subjectID, studentID); err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]bool{"success": true})
}

func (h *ClassroomHandler) GradesXLSX(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok || claims.UserType != "teacher" {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	assignmentID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	assignment, err := h.Repo.GetAssignmentByID(assignmentID)
	if err != nil {
		http.Error(w, "Задание не найдено", http.StatusNotFound)
		return
	}
	isTeacher, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, claims.UserID)
	if !isTeacher {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	submissions, _ := h.Repo.GetSubmissionsByAssignment(assignmentID)

	data := buildGradesXLSX(assignment, submissions)
	fname := fmt.Sprintf("grades_assignment_%d.xlsx", assignmentID)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fname+"\"")
	w.Write(data)
}

func (h *ClassroomHandler) WithdrawSubmission(w http.ResponseWriter, r *http.Request) {
	studentID, _ := GetUserIDFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil || sub.StudentID != studentID {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	if sub.Status == "reviewed" {
		http.Error(w, "Нельзя отозвать проверенную работу", http.StatusBadRequest)
		return
	}
	assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
	if aerr != nil || assignment == nil {
		http.NotFound(w, r)
		return
	}
	if assignment.Deadline != nil && time.Now().After(*assignment.Deadline) {
		http.Error(w, "Дедлайн истёк", http.StatusForbidden)
		return
	}
	if err := h.Repo.DeleteSubmission(submissionID, sub.FilePath, h.BaseDir); err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]bool{"success": true})
}

func (h *ClassroomHandler) UpdateSubjectInline(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	ok, _ := h.Repo.IsTeacherOfSubject(subjectID, userID)
	if !ok {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		http.Error(w, "Название не может быть пустым", http.StatusBadRequest)
		return
	}
	if err := h.Repo.UpdateSubject(subjectID, title, strings.TrimSpace(req.Description)); err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]bool{"success": true})
}


func buildGradesXLSX(assignment *storage.Assignment, submissions []storage.Submission) []byte {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Баллы"
	f.SetSheetName("Sheet1", sheet)

	// Заголовок
	headers := []string{"Студент", "Группа", "Статус", fmt.Sprintf("Балл (макс. %d)", assignment.MaxScore)}
	for i, hd := range headers {
		xlsxSet(f, sheet, i+1, 1, hd)
	}

	// Один студент = одна строка (берём первый встреченный сабмит)
	seen := map[int]bool{}
	rowNum := 1
	for _, s := range submissions {
		if seen[s.StudentID] {
			continue
		}
		seen[s.StudentID] = true
		rowNum++

		status := "Сдано"
		if s.Status == "reviewed" {
			status = "Проверено"
		}
		xlsxSet(f, sheet, 1, rowNum, s.StudentName)
		xlsxSet(f, sheet, 2, rowNum, s.StudentGroup)
		xlsxSet(f, sheet, 3, rowNum, status)
		if s.Grade != nil {
			xlsxSet(f, sheet, 4, rowNum, *s.Grade)
		} else {
			xlsxSet(f, sheet, 4, rowNum, "—")
		}
	}

	f.SetColWidth(sheet, "A", "A", 28)
	f.SetColWidth(sheet, "B", "C", 14)
	xlsxStyleHeader(f, sheet, len(headers))

	buf, _ := f.WriteToBuffer()
	return buf.Bytes()
}

func (h *ClassroomHandler) GetComments(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if userType == "teacher" {
		assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
		if aerr != nil || assignment == nil {
			http.NotFound(w, r)
			return
		}
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	} else if sub.StudentID != userID {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	comments, err := h.Repo.GetSubmissionComments(submissionID)
	if err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}
	if comments == nil {
		comments = []storage.SubmissionComment{}
	}

	type commentResp struct {
		ID        int    `json:"id"`
		UserName  string `json:"user_name"`
		UserType  string `json:"user_type"`
		Message   string `json:"message"`
		CreatedAt string `json:"created_at"`
	}
	resp := make([]commentResp, len(comments))
	for i, c := range comments {
		resp[i] = commentResp{
			ID:        c.ID,
			UserName:  c.UserName,
			UserType:  c.UserType,
			Message:   c.Message,
			CreatedAt: c.CreatedAt.Format("02.01.2006 в 15:04"),
		}
	}
	respondJSON(w, resp)
}

func (h *ClassroomHandler) PostComment(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	submissionID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	sub, err := h.Repo.GetSubmissionByID(submissionID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if userType == "teacher" {
		assignment, aerr := h.Repo.GetAssignmentByID(sub.AssignmentID)
		if aerr != nil || assignment == nil {
			http.NotFound(w, r)
			return
		}
		ok, _ := h.Repo.IsTeacherOfSubject(assignment.SubjectID, userID)
		if !ok {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
	} else if sub.StudentID != userID {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		http.Error(w, "Пустое сообщение", http.StatusBadRequest)
		return
	}
	if len(msg) > 2000 {
		msg = msg[:2000]
	}

	if err := h.Repo.AddSubmissionComment(submissionID, userID, userType, msg); err != nil {
		http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]bool{"ok": true})
}

// hasSubjectAccess — участник предмета: преподаватель предмета или записанный студент.
func (h *ClassroomHandler) hasSubjectAccess(subjectID, userID int, userType string) bool {
	if userType == "teacher" {
		ok, _ := h.Repo.IsTeacherOfSubject(subjectID, userID)
		return ok
	}
	ok, _ := h.Repo.IsStudentOfSubject(subjectID, userID)
	return ok
}

func (h *ClassroomHandler) GetDiscussion(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	if !h.hasSubjectAccess(subjectID, userID, userType) {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	posts, err := h.Repo.GetDiscussionPosts(subjectID)
	if err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}

	type postResp struct {
		ID        int    `json:"id"`
		UserID    int    `json:"user_id"`
		UserName  string `json:"user_name"`
		UserType  string `json:"user_type"`
		Message   string `json:"message"`
		CreatedAt string `json:"created_at"`
		CanDelete bool   `json:"can_delete"`
	}
	// Преподаватель предмета модерирует любые посты; автор может удалить свой.
	isTeacher := userType == "teacher"
	resp := make([]postResp, 0, len(posts))
	for _, p := range posts {
		resp = append(resp, postResp{
			ID:        p.ID,
			UserID:    p.UserID,
			UserName:  p.UserName,
			UserType:  p.UserType,
			Message:   p.Message,
			CreatedAt: p.CreatedAt.Format("02.01.2006 в 15:04"),
			CanDelete: isTeacher || p.UserID == userID,
		})
	}
	respondJSON(w, resp)
}

func (h *ClassroomHandler) PostDiscussion(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	subjectID, _ := strconv.Atoi(chi.URLParam(r, "id"))

	if !h.hasSubjectAccess(subjectID, userID, userType) {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		http.Error(w, "Пустое сообщение", http.StatusBadRequest)
		return
	}
	if len(msg) > 2000 {
		msg = msg[:2000]
	}

	if err := h.Repo.AddDiscussionPost(subjectID, userID, userType, msg); err != nil {
		http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]bool{"ok": true})
}

func (h *ClassroomHandler) DeleteDiscussionPost(w http.ResponseWriter, r *http.Request) {
	userID, _ := GetUserIDFromContext(r)
	userType, _ := GetUserTypeFromContext(r)
	postID, _ := strconv.Atoi(chi.URLParam(r, "postId"))

	post, err := h.Repo.GetDiscussionPost(postID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Удалить может автор поста или преподаватель этого предмета (модерация).
	allowed := post.UserID == userID
	if !allowed && userType == "teacher" {
		ok, _ := h.Repo.IsTeacherOfSubject(post.SubjectID, userID)
		allowed = ok
	}
	if !allowed {
		http.Error(w, "Нет доступа", http.StatusForbidden)
		return
	}

	if err := h.Repo.DeleteDiscussionPost(postID); err != nil {
		http.Error(w, "Ошибка", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]bool{"ok": true})
}
