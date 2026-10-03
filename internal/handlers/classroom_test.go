package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/go-chi/chi/v5"

	"visualmath/internal/auth"
	"visualmath/internal/storage"
)

// setupClassroomDB поднимает минимальную схему classroom в памяти — ровно те
// таблицы и колонки, что читают ClassroomRepo и тестируемые хендлеры.
func setupClassroomDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			full_name TEXT NOT NULL DEFAULT '',
			group_number TEXT DEFAULT ''
		);
		CREATE TABLE subjects (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			description TEXT DEFAULT '',
			invite_code TEXT DEFAULT '',
			created_by INTEGER
		);
		CREATE TABLE subject_teachers (subject_id INTEGER, teacher_id INTEGER);
		CREATE TABLE subject_students (subject_id INTEGER, student_id INTEGER);
		CREATE TABLE assignments (
			id INTEGER PRIMARY KEY,
			subject_id INTEGER NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			description TEXT DEFAULT '',
			condition_file TEXT DEFAULT '',
			deadline DATETIME,
			max_score INTEGER DEFAULT 100,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			strict_deadline INTEGER DEFAULT 0,
			grading_mode TEXT DEFAULT 'total',
			allow_comments INTEGER DEFAULT 1,
			publish_at DATETIME
		);
		CREATE TABLE submissions (
			id INTEGER PRIMARY KEY,
			assignment_id INTEGER NOT NULL,
			student_id INTEGER NOT NULL,
			file_path TEXT DEFAULT '',
			annotated_file_path TEXT DEFAULT '',
			status TEXT DEFAULT 'submitted',
			teacher_comment TEXT DEFAULT '',
			grade INTEGER,
			submitted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			reviewed_at DATETIME,
			pins_json TEXT DEFAULT '',
			annotations_json TEXT DEFAULT '',
			strokes_json TEXT DEFAULT ''
		);
		CREATE TABLE submission_task_grades (
			submission_id INTEGER,
			task_number INTEGER,
			score INTEGER,
			PRIMARY KEY (submission_id, task_number)
		);
		CREATE TABLE assignment_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			assignment_id INTEGER,
			task_number INTEGER,
			label TEXT DEFAULT '',
			max_score INTEGER DEFAULT 0
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// seedGrading готовит сценарий: предмет 1 (препод 10), студент 20, задание 1,
// сдача 1. Параметры задания/сдачи задаются аргументами.
func seedGrading(t *testing.T, db *sql.DB, maxScore int, gradingMode string, strict bool, deadline *time.Time, subStatus string) {
	t.Helper()
	exec := func(q string, args ...interface{}) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, full_name, group_number) VALUES (20, 'Студент Тест', 'БК-1')`)
	exec(`INSERT INTO subjects (id, title, created_by) VALUES (1, 'Матанализ', 10)`)
	exec(`INSERT INTO subject_teachers (subject_id, teacher_id) VALUES (1, 10)`)
	exec(`INSERT INTO subject_students (subject_id, student_id) VALUES (1, 20)`)
	strictI := 0
	if strict {
		strictI = 1
	}
	exec(`INSERT INTO assignments (id, subject_id, title, deadline, max_score, strict_deadline, grading_mode)
	      VALUES (1, 1, 'ДЗ-1', ?, ?, ?, ?)`, deadline, maxScore, strictI, gradingMode)
	exec(`INSERT INTO submissions (id, assignment_id, student_id, status) VALUES (1, 1, 20, ?)`, subStatus)
}

// classroomReq строит запрос с chi-параметром id, claims в контексте и JSON-телом.
func classroomReq(method string, id int, claims *auth.UserClaims, body interface{}) *http.Request {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "/", r)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(id))
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if claims != nil {
		ctx = context.WithValue(ctx, auth.UserContextKey, *claims)
	}
	return req.WithContext(ctx)
}

func gradeOf(t *testing.T, db *sql.DB, submissionID int) (int, bool) {
	t.Helper()
	var g sql.NullInt64
	if err := db.QueryRow(`SELECT grade FROM submissions WHERE id=?`, submissionID).Scan(&g); err != nil {
		t.Fatalf("read grade: %v", err)
	}
	return int(g.Int64), g.Valid
}

// Препод-владелец предмета выставляет валидную оценку — сохраняется.
func TestGradeSubmission_Success(t *testing.T) {
	db := setupClassroomDB(t)
	seedGrading(t, db, 100, "total", false, nil, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}

	w := httptest.NewRecorder()
	h.GradeSubmission(w, classroomReq("POST", 1, teacher(10), map[string]int{"grade": 80}))

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if g, ok := gradeOf(t, db, 1); !ok || g != 80 {
		t.Errorf("ожидали grade=80, got %d (valid=%v)", g, ok)
	}
}

// Оценка больше max_score отклоняется (400), в БД не пишется.
func TestGradeSubmission_ExceedsMaxRejected(t *testing.T) {
	db := setupClassroomDB(t)
	seedGrading(t, db, 100, "total", false, nil, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}

	w := httptest.NewRecorder()
	h.GradeSubmission(w, classroomReq("POST", 1, teacher(10), map[string]int{"grade": 150}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("ожидали 400, got %d", w.Code)
	}
	if _, ok := gradeOf(t, db, 1); ok {
		t.Errorf("оценка не должна была сохраниться")
	}
}

// Не-преподаватель этого предмета не может выставить оценку (403).
func TestGradeSubmission_NotTeacherForbidden(t *testing.T) {
	db := setupClassroomDB(t)
	seedGrading(t, db, 100, "total", false, nil, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}

	w := httptest.NewRecorder()
	h.GradeSubmission(w, classroomReq("POST", 1, teacher(999), map[string]int{"grade": 50}))

	if w.Code != http.StatusForbidden {
		t.Fatalf("ожидали 403, got %d", w.Code)
	}
}

// per_task: сумма баллов по заданиям складывается и режется по max_score,
// отрицательные баллы обнуляются.
func TestGradeSubmission_PerTaskSumsAndCaps(t *testing.T) {
	db := setupClassroomDB(t)
	seedGrading(t, db, 100, "per_task", false, nil, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}

	w := httptest.NewRecorder()
	body := map[string]map[string]int{"task_grades": {"1": 60, "2": 70, "3": -10}}
	h.GradeSubmission(w, classroomReq("POST", 1, teacher(10), body))

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	// 60 + 70 + 0 = 130 → режется до max_score 100
	var resp struct {
		Grade int `json:"grade"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Grade != 100 {
		t.Errorf("ожидали итог 100 (срез по max), got %d", resp.Grade)
	}
	if g, ok := gradeOf(t, db, 1); !ok || g != 100 {
		t.Errorf("ожидали grade=100 в БД, got %d (valid=%v)", g, ok)
	}
	// отрицательный балл обнулён
	var task3 int
	db.QueryRow(`SELECT score FROM submission_task_grades WHERE submission_id=1 AND task_number=3`).Scan(&task3)
	if task3 != 0 {
		t.Errorf("отрицательный балл должен быть обнулён, got %d", task3)
	}
}

// Студент-владелец до дедлайна может отозвать неотправленную на проверку работу.
func TestWithdrawSubmission_Success(t *testing.T) {
	db := setupClassroomDB(t)
	future := time.Now().Add(24 * time.Hour)
	seedGrading(t, db, 100, "total", true, &future, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db), BaseDir: t.TempDir()}

	w := httptest.NewRecorder()
	h.WithdrawSubmission(w, classroomReq("POST", 1, student(20), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM submissions WHERE id=1`).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("сдача должна быть удалена, осталось %d", cnt)
	}
}

// Проверенную работу отозвать нельзя (400).
func TestWithdrawSubmission_ReviewedRejected(t *testing.T) {
	db := setupClassroomDB(t)
	future := time.Now().Add(24 * time.Hour)
	seedGrading(t, db, 100, "total", false, &future, "reviewed")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db), BaseDir: t.TempDir()}

	w := httptest.NewRecorder()
	h.WithdrawSubmission(w, classroomReq("POST", 1, student(20), nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("ожидали 400, got %d", w.Code)
	}
}

// После дедлайна отзыв запрещён (403), сдача остаётся.
func TestWithdrawSubmission_AfterDeadlineForbidden(t *testing.T) {
	db := setupClassroomDB(t)
	past := time.Now().Add(-1 * time.Hour)
	seedGrading(t, db, 100, "total", true, &past, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db), BaseDir: t.TempDir()}

	w := httptest.NewRecorder()
	h.WithdrawSubmission(w, classroomReq("POST", 1, student(20), nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("ожидали 403, got %d", w.Code)
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM submissions WHERE id=1`).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("сдача после дедлайна не должна удаляться")
	}
}

// Чужую сдачу отозвать нельзя (403).
func TestWithdrawSubmission_NotOwnerForbidden(t *testing.T) {
	db := setupClassroomDB(t)
	future := time.Now().Add(24 * time.Hour)
	seedGrading(t, db, 100, "total", false, &future, "submitted")
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db), BaseDir: t.TempDir()}

	w := httptest.NewRecorder()
	h.WithdrawSubmission(w, classroomReq("POST", 1, student(777), nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("ожидали 403, got %d", w.Code)
	}
}

// Страница аннотирования per_task-задания должна отрендериться без ошибки шаблона
// и содержать панель пооценочного выставления с предзаполнением сохранённых баллов.
func TestAnnotatePage_RendersPerTaskGrading(t *testing.T) {
	if err := InitTemplates("../.."); err != nil {
		t.Fatalf("InitTemplates: %v", err)
	}
	db := setupClassroomDB(t)
	mustExec := func(q string, a ...interface{}) {
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	mustExec(`INSERT INTO users (id, full_name) VALUES (20, 'Студент')`)
	mustExec(`INSERT INTO subjects (id, title, created_by) VALUES (1, 'Матан', 10)`)
	mustExec(`INSERT INTO subject_teachers (subject_id, teacher_id) VALUES (1, 10)`)
	mustExec(`INSERT INTO assignments (id, subject_id, title, grading_mode, max_score) VALUES (1, 1, 'КР', 'per_task', 100)`)
	mustExec(`INSERT INTO assignment_tasks (assignment_id, task_number, label, max_score) VALUES (1, 1, '№1', 40), (1, 2, '№2', 60)`)
	mustExec(`INSERT INTO submissions (id, assignment_id, student_id, status) VALUES (1, 1, 20, 'submitted')`)
	mustExec(`INSERT INTO submission_task_grades (submission_id, task_number, score) VALUES (1, 1, 35)`)

	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}
	w := httptest.NewRecorder()
	h.AnnotatePage(w, classroomReq("GET", 1, teacher(10), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "Ошибка шаблона") {
		t.Fatalf("ошибка исполнения шаблона: %s", body)
	}
	for _, want := range []string{"Сохранить оценку", `class="tg-input"`, `id="tgTotal"`} {
		if !strings.Contains(body, want) {
			t.Errorf("в HTML нет %q", want)
		}
	}
	// предзаполнение балла задачи 1 пришло в JS-объект TASK_SCORES
	if !strings.Contains(body, `"1":35`) {
		t.Errorf("TASK_SCORES не содержит выставленный балл 35")
	}
}

// Total-режим: одно поле оценки с предзаполнением уже выставленного балла.
func TestAnnotatePage_RendersTotalGrading(t *testing.T) {
	if err := InitTemplates("../.."); err != nil {
		t.Fatalf("InitTemplates: %v", err)
	}
	db := setupClassroomDB(t)
	mustExec := func(q string, a ...interface{}) {
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	mustExec(`INSERT INTO users (id, full_name) VALUES (20, 'Студент')`)
	mustExec(`INSERT INTO subjects (id, title, created_by) VALUES (1, 'Матан', 10)`)
	mustExec(`INSERT INTO subject_teachers (subject_id, teacher_id) VALUES (1, 10)`)
	mustExec(`INSERT INTO assignments (id, subject_id, title, grading_mode, max_score) VALUES (1, 1, 'ДЗ', 'total', 100)`)
	mustExec(`INSERT INTO submissions (id, assignment_id, student_id, status, grade) VALUES (1, 1, 20, 'reviewed', 87)`)

	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}
	w := httptest.NewRecorder()
	h.AnnotatePage(w, classroomReq("GET", 1, teacher(10), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "Ошибка шаблона") {
		t.Fatalf("ошибка исполнения шаблона: %s", body)
	}
	if !strings.Contains(body, `id="totalGrade"`) {
		t.Errorf("нет поля общей оценки")
	}
	if !strings.Contains(body, `value="87"`) {
		t.Errorf("общая оценка 87 не предзаполнена")
	}
}

// Несуществующее задание по прямому URL — 404, а не паника: раньше
// GetAssignmentByID возвращал (nil, nil) и AssignmentPage разыменовывал nil.
func TestAssignmentPage_NotFound404(t *testing.T) {
	if err := InitTemplates("../.."); err != nil {
		t.Fatalf("InitTemplates: %v", err)
	}
	db := setupClassroomDB(t)
	h := &ClassroomHandler{Repo: storage.NewClassroomRepo(db)}

	w := httptest.NewRecorder()
	h.AssignmentPage(w, classroomReq("GET", 999999, teacher(10), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("ожидали 404, got %d", w.Code)
	}
}

// strokesToPNG с враждебными данными (гигантские координаты и толщина) должен
// завершаться быстро, а не крутить миллиарды итераций в stampLine.
func TestStrokesToPNG_HostileInputBounded(t *testing.T) {
	strokes := []StrokeData{
		{Tool: "pen", Color: "#ff0000", Width: 1e9, Opacity: 5,
			Points: [][2]float64{{0, 0}, {1e12, 1e12}}},
		{Tool: "underline", Color: "#00ff00", Width: -50,
			Points: [][2]float64{{-1e9, 100}, {1e9, 100}}},
	}
	done := make(chan struct{})
	go func() {
		if _, err := strokesToPNG(strokes, 595, 842); err != nil {
			t.Errorf("strokesToPNG: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("strokesToPNG завис на враждебном входе (нет капа итераций)")
	}
}

// GetSubmissionPins должен отдавать pins, annotations И strokes — именно их
// редактор аннотирования подгружает при открытии, чтобы не начинать с пустого
// листа и не затирать разметку при повторном сохранении.
func TestGetSubmissionPins_IncludesStrokes(t *testing.T) {
	db := setupClassroomDB(t)
	seedGrading(t, db, 100, "total", false, nil, "submitted")
	r := storage.NewClassroomRepo(db)
	err := r.SaveAnnotatedFile(1, "ann.pdf", "ок",
		`[{"page":1,"x":1,"y":2,"comment":"a"}]`,
		`[{"type":"underline","page":1}]`,
		`[{"page":1,"strokes":[{"tool":"pen"}]}]`)
	if err != nil {
		t.Fatalf("SaveAnnotatedFile: %v", err)
	}
	h := &ClassroomHandler{Repo: r}

	w := httptest.NewRecorder()
	h.GetSubmissionPins(w, classroomReq("GET", 1, teacher(10), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Pins        []map[string]interface{} `json:"pins"`
		Annotations []map[string]interface{} `json:"annotations"`
		Strokes     []map[string]interface{} `json:"strokes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v body=%s", err, w.Body.String())
	}
	if len(resp.Pins) != 1 {
		t.Errorf("pins=%d, ожидали 1", len(resp.Pins))
	}
	if len(resp.Annotations) != 1 {
		t.Errorf("annotations=%d, ожидали 1", len(resp.Annotations))
	}
	if len(resp.Strokes) != 1 {
		t.Errorf("strokes=%d, ожидали 1 — редактор не получит мазки для подгрузки", len(resp.Strokes))
	}
}
