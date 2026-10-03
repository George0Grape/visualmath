package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type LectureRepo struct {
	DB *sql.DB
}

type LectureSummary struct {
	ID           int
	Title        string
	Course       string
	Description  string
	Author       string
	AuthorID     int
	CreatedAt    string
	Published    bool
	HideTests    bool
	AllowBack    bool
	ModulesCount int
	CommentCount int
}

type LectureModule struct {
	ID          int
	Order       int
	Title       string
	Type        string
	Description string
	Content     interface{}
}

type LectureDetail struct {
	ID          int
	Title       string
	Course      string
	Description string
	AuthorName  string
	AuthorID    int
	Published   bool
	HideTests   bool
	AllowBack   bool
	CreatedAt   string
	Modules     []LectureModule
}

type LectureComment struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	UserName  string `json:"user_name"`
	UserType  string `json:"user_type"`
	AvatarURL string `json:"avatar_url"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type AvailableModule struct {
	ID          int
	Title       string
	Course      string
	Type        string
	Description string
	Author      string
	CreatedAt   string
}

func (r *LectureRepo) ListByAuthor(authorID int) ([]LectureSummary, error) {
	rows, err := r.DB.Query(`
		SELECT l.id, l.title, l.course, l.description,
		       u.full_name, l.author_id, l.created_at, l.published, l.hide_tests, l.allow_back,
		       COUNT(lm.module_id) as modules_count
		FROM lectures l
		LEFT JOIN users u ON l.author_id = u.id
		LEFT JOIN lecture_modules lm ON l.id = lm.lecture_id
		WHERE l.author_id = ?
		GROUP BY l.id
		ORDER BY l.created_at DESC
	`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lectures []LectureSummary
	for rows.Next() {
		var l LectureSummary
		if err := rows.Scan(&l.ID, &l.Title, &l.Course, &l.Description, &l.Author,
			&l.AuthorID, &l.CreatedAt, &l.Published, &l.HideTests, &l.AllowBack, &l.ModulesCount); err != nil {
			return nil, err
		}
		lectures = append(lectures, l)
	}
	return lectures, rows.Err()
}

func (r *LectureRepo) ListPublished(course, authorName string, year, authorID int) ([]LectureSummary, error) {
	query := `
		SELECT l.id, l.title, l.course, COALESCE(l.description,''),
		       u.full_name, l.author_id, l.created_at, l.published, l.hide_tests, l.allow_back,
		       COUNT(lm.module_id) as modules_count,
		       (SELECT COUNT(*) FROM lecture_comments lc WHERE lc.lecture_id = l.id) as comment_count
		FROM lectures l
		LEFT JOIN users u ON l.author_id = u.id
		LEFT JOIN lecture_modules lm ON l.id = lm.lecture_id
		WHERE l.published = 1
	`
	args := []interface{}{}
	if course != "" {
		query += " AND l.course = ?"
		args = append(args, course)
	}
	if authorName != "" {
		query += " AND u.full_name LIKE ?"
		args = append(args, "%"+authorName+"%")
	}
	if year > 0 {
		query += " AND strftime('%Y', l.created_at) = ?"
		args = append(args, fmt.Sprintf("%d", year))
	}
	if authorID > 0 {
		query += " AND l.author_id = ?"
		args = append(args, authorID)
	}
	query += " GROUP BY l.id ORDER BY l.created_at DESC"

	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lectures []LectureSummary
	for rows.Next() {
		var l LectureSummary
		if err := rows.Scan(&l.ID, &l.Title, &l.Course, &l.Description, &l.Author,
			&l.AuthorID, &l.CreatedAt, &l.Published, &l.HideTests, &l.AllowBack, &l.ModulesCount, &l.CommentCount); err != nil {
			return nil, err
		}
		lectures = append(lectures, l)
	}
	return lectures, rows.Err()
}

func (r *LectureRepo) DistinctCourses() ([]string, error) {
	rows, err := r.DB.Query(`SELECT DISTINCT course FROM lectures WHERE published=1 ORDER BY course`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var courses []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

func (r *LectureRepo) DistinctYears() ([]int, error) {
	rows, err := r.DB.Query(`SELECT DISTINCT CAST(strftime('%Y', created_at) AS INTEGER) FROM lectures WHERE published=1 ORDER BY 1 DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var years []int
	for rows.Next() {
		var y int
		rows.Scan(&y)
		years = append(years, y)
	}
	return years, rows.Err()
}

func (r *LectureRepo) GetByID(id int) (*LectureDetail, error) {
	var l LectureDetail
	err := r.DB.QueryRow(`
		SELECT l.id, l.title, l.course, COALESCE(l.description,''),
		       u.full_name, l.author_id, l.published, l.hide_tests, l.allow_back, l.created_at
		FROM lectures l
		LEFT JOIN users u ON l.author_id = u.id
		WHERE l.id = ?
	`, id).Scan(&l.ID, &l.Title, &l.Course, &l.Description,
		&l.AuthorName, &l.AuthorID, &l.Published, &l.HideTests, &l.AllowBack, &l.CreatedAt)
	if err != nil {
		return nil, err
	}

	rows, err := r.DB.Query(`
		SELECT m.id, m.title, m.module_type, m.content, m.description, lm.module_order
		FROM lecture_modules lm
		JOIN modules m ON lm.module_id = m.id
		WHERE lm.lecture_id = ?
		ORDER BY lm.module_order
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var mod LectureModule
		var content string
		if err := rows.Scan(&mod.ID, &mod.Title, &mod.Type, &content, &mod.Description, &mod.Order); err != nil {
			return nil, err
		}
		if content != "" {
			if err := json.Unmarshal([]byte(content), &mod.Content); err != nil {
				mod.Content = content
			}
		}
		l.Modules = append(l.Modules, mod)
	}
	return &l, rows.Err()
}

func (r *LectureRepo) Create(title, course, description string, authorID int, published, hideTests, allowBack bool, moduleIDs []int) (int64, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	result, err := tx.Exec(`
		INSERT INTO lectures (title, course, description, author_id, published, hide_tests, allow_back)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, title, course, description, authorID, published, hideTests, allowBack)
	if err != nil {
		return 0, fmt.Errorf("failed to save lecture: %w", err)
	}

	lectureID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get lecture ID: %w", err)
	}

	for order, moduleID := range moduleIDs {
		var exists bool
		err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM modules WHERE id = ?)", moduleID).Scan(&exists)
		if err != nil || !exists {
			return 0, fmt.Errorf("module with ID %d does not exist", moduleID)
		}
		_, err = tx.Exec(`
			INSERT INTO lecture_modules (lecture_id, module_id, module_order)
			VALUES (?, ?, ?)
		`, lectureID, moduleID, order)
		if err != nil {
			return 0, fmt.Errorf("failed to save module %d: %w", moduleID, err)
		}
	}

	return lectureID, tx.Commit()
}

func (r *LectureRepo) Update(id int, title, course, description string, published, hideTests, allowBack bool, moduleIDs []int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	_, err = tx.Exec(`
		UPDATE lectures SET title=?, course=?, description=?, published=?, hide_tests=?, allow_back=?
		WHERE id=?
	`, title, course, description, published, hideTests, allowBack, id)
	if err != nil {
		return fmt.Errorf("failed to update lecture: %w", err)
	}

	if len(moduleIDs) > 0 {
		_, err = tx.Exec("DELETE FROM lecture_modules WHERE lecture_id = ?", id)
		if err != nil {
			return fmt.Errorf("failed to delete old modules: %w", err)
		}
		for order, moduleID := range moduleIDs {
			var exists bool
			err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM modules WHERE id = ?)", moduleID).Scan(&exists)
			if err != nil || !exists {
				return fmt.Errorf("module with ID %d does not exist", moduleID)
			}
			_, err = tx.Exec(`
				INSERT INTO lecture_modules (lecture_id, module_id, module_order)
				VALUES (?, ?, ?)
			`, id, moduleID, order)
			if err != nil {
				return fmt.Errorf("failed to save module %d: %w", moduleID, err)
			}
		}
	}

	return tx.Commit()
}

func (r *LectureRepo) Delete(id int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	_, err = tx.Exec("DELETE FROM lecture_modules WHERE lecture_id = ?", id)
	if err != nil {
		return err
	}

	result, err := tx.Exec("DELETE FROM lectures WHERE id = ?", id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return tx.Commit()
}

func (r *LectureRepo) GetAvailableModules(authorID int) ([]AvailableModule, error) {
	rows, err := r.DB.Query(`
		SELECT m.id, m.title, m.course, m.module_type, m.description,
		       COALESCE(u.full_name, 'Неизвестный автор') as author, m.created_at
		FROM modules m
		LEFT JOIN users u ON m.author_id = u.id
		ORDER BY m.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []AvailableModule
	for rows.Next() {
		var m AvailableModule
		if err := rows.Scan(&m.ID, &m.Title, &m.Course, &m.Type, &m.Description, &m.Author, &m.CreatedAt); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, rows.Err()
}

func (r *LectureRepo) Exists(id int) (bool, error) {
	var exists bool
	err := r.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM lectures WHERE id = ?)", id).Scan(&exists)
	return exists, err
}

func (r *LectureRepo) GetAllowBack(id int) (bool, error) {
	var allowBack bool
	err := r.DB.QueryRow("SELECT allow_back FROM lectures WHERE id = ?", id).Scan(&allowBack)
	return allowBack, err
}

func (r *LectureRepo) GetNextModuleID(lectureID, moduleID int) (int, error) {
	var nextID int
	err := r.DB.QueryRow(`
		SELECT lm2.module_id
		FROM lecture_modules lm1
		LEFT JOIN lecture_modules lm2 ON lm1.lecture_id = lm2.lecture_id
			AND lm2.module_order = lm1.module_order + 1
		WHERE lm1.lecture_id = ? AND lm1.module_id = ?
	`, lectureID, moduleID).Scan(&nextID)
	return nextID, err
}

func (r *LectureRepo) CountModules(lectureID int) (int, error) {
	var count int
	err := r.DB.QueryRow("SELECT COUNT(*) FROM lecture_modules WHERE lecture_id = ?", lectureID).Scan(&count)
	return count, err
}

func (r *LectureRepo) GetComments(lectureID int) ([]LectureComment, error) {
	rows, err := r.DB.Query(`
		SELECT lc.id, lc.user_id, u.full_name, COALESCE(u.user_type,''), COALESCE(u.avatar_url,''), lc.text,
		       strftime('%d.%m.%Y %H:%M', lc.created_at)
		FROM lecture_comments lc
		JOIN users u ON lc.user_id = u.id
		WHERE lc.lecture_id = ?
		ORDER BY lc.created_at ASC
	`, lectureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []LectureComment
	for rows.Next() {
		var c LectureComment
		if err := rows.Scan(&c.ID, &c.UserID, &c.UserName, &c.UserType, &c.AvatarURL, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (r *LectureRepo) AddComment(lectureID, userID int, text string) (int64, error) {
	res, err := r.DB.Exec(
		`INSERT INTO lecture_comments (lecture_id, user_id, text) VALUES (?, ?, ?)`,
		lectureID, userID, text,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
