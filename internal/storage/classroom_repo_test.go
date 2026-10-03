package storage

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupAssignmentsDB поднимает минимум таблиц, которые читают выборки заданий:
// колонки должны совпадать с тем, что сканит scanAssignments (иначе Scan падает).
func setupAssignmentsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, full_name TEXT, login TEXT, group_number TEXT);
		CREATE TABLE subject_students (
			subject_id INTEGER, student_id INTEGER,
			joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (subject_id, student_id)
		);
		CREATE TABLE submissions (id INTEGER PRIMARY KEY, assignment_id INTEGER, status TEXT);
		CREATE TABLE assignments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
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
	`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// Все три выборки заданий (список, по id, ближайшее) должны сканиться без ошибки —
// т.е. число колонок в SELECT совпадает с scanAssignments. Ловит рассинхрон,
// который дважды возникал при добавлении новых колонок (allow_comments, publish_at).
func TestAssignmentSelects_ColumnCountConsistent(t *testing.T) {
	db := setupAssignmentsDB(t)
	r := NewClassroomRepo(db)
	future := time.Now().Add(48 * time.Hour)
	if _, err := r.CreateAssignment(1, "Обычное ДЗ", "", "", &future, 100, false, "total", true, nil); err != nil {
		t.Fatalf("create normal: %v", err)
	}

	list, err := r.GetAssignmentsBySubject(1)
	if err != nil {
		t.Fatalf("GetAssignmentsBySubject: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ожидали 1 задание, got %d", len(list))
	}
	if _, err := r.GetAssignmentByID(list[0].ID); err != nil {
		t.Fatalf("GetAssignmentByID: %v", err)
	}
	if _, err := r.GetNearestAssignment(1); err != nil {
		t.Fatalf("GetNearestAssignment: %v", err)
	}
}

// Несуществующий ID — это ошибка (sql.ErrNoRows), а не (nil, nil): хендлеры
// проверяют только err и разыменовывали бы nil.
func TestGetAssignmentByID_NotFoundIsError(t *testing.T) {
	db := setupAssignmentsDB(t)
	r := NewClassroomRepo(db)
	a, err := r.GetAssignmentByID(999999)
	if err == nil {
		t.Fatalf("ожидали ошибку для несуществующего ID, got assignment=%v", a)
	}
	if a != nil {
		t.Errorf("assignment должен быть nil, got %+v", a)
	}
}

// Запланированное задание (publish_at в будущем) не должно всплывать как «ближайшее»
// на карточке предмета — иначе контрольная светится студенту до времени выдачи.
func TestGetNearestAssignment_SkipsScheduled(t *testing.T) {
	db := setupAssignmentsDB(t)
	r := NewClassroomRepo(db)
	future := time.Now().Add(72 * time.Hour)

	// запланированное с более ранним дедлайном — не должно перебить обычное
	nearDeadline := time.Now().Add(24 * time.Hour)
	if _, err := r.CreateAssignment(1, "Контрольная", "", "", &nearDeadline, 100, false, "total", true, &future); err != nil {
		t.Fatalf("create scheduled: %v", err)
	}
	farDeadline := time.Now().Add(48 * time.Hour)
	if _, err := r.CreateAssignment(1, "Обычное ДЗ", "", "", &farDeadline, 100, false, "total", true, nil); err != nil {
		t.Fatalf("create normal: %v", err)
	}

	near, err := r.GetNearestAssignment(1)
	if err != nil {
		t.Fatalf("GetNearestAssignment: %v", err)
	}
	if near == nil {
		t.Fatal("ожидали обычное задание, got nil")
	}
	if near.Title != "Обычное ДЗ" {
		t.Errorf("ближайшим оказалось запланированное: %q", near.Title)
	}
}

// Аннотации препода должны round-trip'иться: сохранили мазки/пины/аннотации/
// комментарий — читаем ровно то же. Это основа фикса: без хранения штрихов
// редактор открывался пустым и повторное сохранение затирало разметку.
func TestSaveAndGetAnnotations_RoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE submissions (
		id INTEGER PRIMARY KEY, assignment_id INTEGER, student_id INTEGER,
		file_path TEXT, annotated_file_path TEXT, status TEXT DEFAULT 'submitted',
		teacher_comment TEXT, grade INTEGER, submitted_at DATETIME, reviewed_at DATETIME,
		pins_json TEXT, annotations_json TEXT, strokes_json TEXT
	)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	db.Exec(`INSERT INTO submissions (id, assignment_id, student_id) VALUES (1, 1, 20)`)

	r := NewClassroomRepo(db)
	pins := `[{"page":1,"x":100,"y":200,"comment":"ошибка"}]`
	anns := `[{"type":"underline","page":1,"x1":10,"y1":20,"x2":60,"y2":20,"comment":"см."}]`
	strokes := `[{"page":1,"page_width":595,"page_height":842,"strokes":[{"tool":"pen","color":"#ef4444"}]}]`

	if err := r.SaveAnnotatedFile(1, "ann.pdf", "Хорошая работа", pins, anns, strokes); err != nil {
		t.Fatalf("save: %v", err)
	}

	if got, _ := r.GetPins(1); got != pins {
		t.Errorf("pins round-trip: %s", got)
	}
	if got, _ := r.GetAnnotations(1); got != anns {
		t.Errorf("annotations round-trip: %s", got)
	}
	if got, _ := r.GetStrokes(1); got != strokes {
		t.Errorf("strokes round-trip (без них редактор пуст): %s", got)
	}
	var comment, status string
	db.QueryRow(`SELECT teacher_comment, status FROM submissions WHERE id=1`).Scan(&comment, &status)
	if comment != "Хорошая работа" {
		t.Errorf("comment round-trip: %q", comment)
	}
	if status != "reviewed" {
		t.Errorf("статус должен стать reviewed, got %q", status)
	}
}

// Список студентов для раздачи вариантов сортируется по joined_at — колонка
// должна существовать (был баг с несуществующей ss.id, ронявший раздачу).
func TestGetStudentsBySubjectForAssignment_OrdersByJoined(t *testing.T) {
	db := setupAssignmentsDB(t)
	r := NewClassroomRepo(db)
	future := time.Now().Add(24 * time.Hour)
	id, err := r.CreateAssignment(5, "ДЗ", "", "", &future, 100, false, "total", true, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	db.Exec(`INSERT INTO users (id, full_name, login) VALUES (20,'Борис','b'),(21,'Анна','a')`)
	db.Exec(`INSERT INTO subject_students (subject_id, student_id, joined_at) VALUES (5,21,'2026-01-02'),(5,20,'2026-01-01')`)

	students, err := r.GetStudentsBySubjectForAssignment(id)
	if err != nil {
		t.Fatalf("GetStudentsBySubjectForAssignment: %v", err)
	}
	if len(students) != 2 {
		t.Fatalf("ожидали 2 студентов, got %d", len(students))
	}
	// раньше присоединился студент 20 → он первый
	if students[0].ID != 20 {
		t.Errorf("порядок по joined_at нарушен: первый %d, ожидали 20", students[0].ID)
	}
}
