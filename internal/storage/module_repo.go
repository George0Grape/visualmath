package storage

import "database/sql"

type ModuleRepo struct {
	DB *sql.DB
}

type Module struct {
	ID             int
	Title          string
	Course         string
	Description    string
	Type           string
	ContentJSON    string
	Author         string
	CreatedAt      string
	QuestionsCount int
	QuestionsMode  string // "per_student" | "same_for_all"
}

type ModuleSummary struct {
	ID          int
	AuthorID    int
	Title       string
	Course      string
	Description string
	Type        string
	Author      string
	CreatedAt   string
}

func (r *ModuleRepo) Create(title, course, description, moduleType, contentJSON string, authorID, questionsCount int, questionsMode string) (int64, error) {
	if questionsMode == "" {
		questionsMode = "per_student"
	}
	result, err := r.DB.Exec(`
		INSERT INTO modules (title, course, description, module_type, content, author_id, questions_count, questions_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, title, course, description, moduleType, contentJSON, authorID, questionsCount, questionsMode)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (r *ModuleRepo) GetByID(id string) (*Module, error) {
	var m Module
	err := r.DB.QueryRow(`
		SELECT m.id, m.title, m.course, COALESCE(m.description,''), m.module_type,
		       COALESCE(m.content,''), COALESCE(u.full_name,'') as author, m.created_at,
		       COALESCE(m.questions_count, 0), COALESCE(m.questions_mode, 'per_student')
		FROM modules m
		LEFT JOIN users u ON m.author_id = u.id
		WHERE m.id = ?
	`, id).Scan(&m.ID, &m.Title, &m.Course, &m.Description, &m.Type, &m.ContentJSON, &m.Author, &m.CreatedAt, &m.QuestionsCount, &m.QuestionsMode)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *ModuleRepo) Update(id, title, course, description, moduleType, contentJSON string, questionsCount int, questionsMode string) error {
	if questionsMode == "" {
		questionsMode = "per_student"
	}
	_, err := r.DB.Exec(`
		UPDATE modules SET title=?, course=?, description=?, module_type=?, content=?, questions_count=?, questions_mode=?
		WHERE id=?
	`, title, course, description, moduleType, contentJSON, questionsCount, questionsMode, id)
	return err
}

// GetSharedAssignment возвращает шаблонный набор вопросов для режима "одинаковые для всех".
// Ищет любую запись для данной сессии+модуля, т.е. что получил первый студент.
func (r *ModuleRepo) GetSharedAssignment(sessionID string, moduleID int) (string, error) {
	var questionsJSON string
	err := r.DB.QueryRow(
		`SELECT questions_json FROM student_module_assignments WHERE session_id=? AND module_id=? LIMIT 1`,
		sessionID, moduleID,
	).Scan(&questionsJSON)
	return questionsJSON, err
}

func (r *ModuleRepo) GetAssignment(sessionID string, studentID, moduleID int) (string, error) {
	var questionsJSON string
	err := r.DB.QueryRow(
		`SELECT questions_json FROM student_module_assignments WHERE session_id=? AND student_id=? AND module_id=?`,
		sessionID, studentID, moduleID,
	).Scan(&questionsJSON)
	return questionsJSON, err
}

func (r *ModuleRepo) SaveAssignment(sessionID string, studentID, moduleID int, questionsJSON string) error {
	_, err := r.DB.Exec(
		`INSERT INTO student_module_assignments (session_id, student_id, module_id, questions_json) VALUES (?, ?, ?, ?)`,
		sessionID, studentID, moduleID, questionsJSON,
	)
	return err
}

func (r *ModuleRepo) Delete(id string) error {
	r.DB.Exec("DELETE FROM lecture_modules WHERE module_id=?", id)
	_, err := r.DB.Exec("DELETE FROM modules WHERE id=?", id)
	return err
}

func (r *ModuleRepo) ListByAuthor(authorID int) ([]ModuleSummary, error) {
	rows, err := r.DB.Query(`
		SELECT m.id, m.author_id, m.title, m.course, COALESCE(m.description,''), m.module_type,
		       COALESCE(u.full_name,'') as author, m.created_at
		FROM modules m
		LEFT JOIN users u ON m.author_id = u.id
		WHERE m.author_id = ?
		ORDER BY m.created_at DESC
	`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []ModuleSummary
	for rows.Next() {
		var m ModuleSummary
		if err := rows.Scan(&m.ID, &m.AuthorID, &m.Title, &m.Course, &m.Description, &m.Type, &m.Author, &m.CreatedAt); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, rows.Err()
}

func (r *ModuleRepo) ListAll() ([]ModuleSummary, error) {
	rows, err := r.DB.Query(`
		SELECT m.id, m.author_id, m.title, m.course, COALESCE(m.description,''), m.module_type,
		       COALESCE(u.full_name,'Неизвестный автор') as author, m.created_at
		FROM modules m
		LEFT JOIN users u ON m.author_id = u.id
		ORDER BY m.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []ModuleSummary
	for rows.Next() {
		var m ModuleSummary
		if err := rows.Scan(&m.ID, &m.AuthorID, &m.Title, &m.Course, &m.Description, &m.Type, &m.Author, &m.CreatedAt); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, rows.Err()
}
