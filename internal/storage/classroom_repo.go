package storage

import (
	"database/sql"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

type Subject struct {
	ID          int
	Title       string
	Description string
	InviteCode  string
	CreatedBy   int
	CreatedAt   time.Time
}

type SubjectWithRole struct {
	Subject
	IsOwner      bool
	TeacherCount int
	StudentCount int
	AssignCount  int
}

type AssignmentTask struct {
	ID           int
	AssignmentID int
	TaskNumber   int
	Label        string
	MaxScore     int
}

type SubmissionTaskGrade struct {
	TaskNumber int
	Score      int
	Label      string
	MaxScore   int
}

type Assignment struct {
	ID             int
	SubjectID      int
	Title          string
	Description    string
	ConditionFile  string
	Deadline       *time.Time
	StrictDeadline bool
	MaxScore       int
	GradingMode    string // "total" | "per_task"
	CreatedAt      time.Time
	AllowComments  bool
	PublishAt      *time.Time // время выдачи студентам; nil = сразу
	// для отображения
	SubjectTitle  string
	SubmitCount   int
	ReviewedCount int
	Tasks         []AssignmentTask
}

// IsScheduled — задание ещё не выдано студентам (время выдачи в будущем).
func (a Assignment) IsScheduled() bool {
	return a.PublishAt != nil && a.PublishAt.After(time.Now())
}

type SubmissionComment struct {
	ID           int
	SubmissionID int
	UserID       int
	UserName     string
	UserType     string
	Message      string
	CreatedAt    time.Time
}

type DiscussionPost struct {
	ID        int
	SubjectID int
	UserID    int
	UserName  string
	UserType  string
	Message   string
	CreatedAt time.Time
}

type Submission struct {
	ID                 int
	AssignmentID       int
	StudentID          int
	FilePath           string
	AnnotatedFilePath  string
	Status             string
	TeacherComment     string
	Grade              *int
	PinsJSON           string
	SubmittedAt        time.Time
	ReviewedAt         *time.Time
	// для отображения
	StudentName        string
	StudentGroup       string
	AssignmentTitle    string
	SubjectTitle       string
	IsLate             bool
}

type ClassroomRepo struct {
	DB *sql.DB
}

func NewClassroomRepo(db *sql.DB) *ClassroomRepo {
	return &ClassroomRepo{DB: db}
}

func (r *ClassroomRepo) CreateSubject(title, description, inviteCode string, teacherID int) (int, error) {
	res, err := r.DB.Exec(
		`INSERT INTO subjects (title, description, invite_code, created_by) VALUES (?,?,?,?)`,
		title, description, inviteCode, teacherID,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	// создатель предмета автоматически становится его преподавателем
	_, err = r.DB.Exec(`INSERT INTO subject_teachers (subject_id, teacher_id) VALUES (?,?)`, id, teacherID)
	return int(id), err
}

func (r *ClassroomRepo) GetSubjectsForTeacher(teacherID int) ([]SubjectWithRole, error) {
	rows, err := r.DB.Query(`
		SELECT s.id, s.title, s.description, s.invite_code, s.created_by, s.created_at,
		       (s.created_by = ?) as is_owner,
		       (SELECT COUNT(*) FROM subject_teachers WHERE subject_id = s.id) as teacher_count,
		       (SELECT COUNT(*) FROM subject_students WHERE subject_id = s.id) as student_count,
		       (SELECT COUNT(*) FROM assignments WHERE subject_id = s.id) as assign_count
		FROM subjects s
		JOIN subject_teachers st ON st.subject_id = s.id
		WHERE st.teacher_id = ?
		ORDER BY s.created_at DESC
	`, teacherID, teacherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SubjectWithRole
	for rows.Next() {
		var sw SubjectWithRole
		err := rows.Scan(&sw.ID, &sw.Title, &sw.Description, &sw.InviteCode,
			&sw.CreatedBy, &sw.CreatedAt, &sw.IsOwner,
			&sw.TeacherCount, &sw.StudentCount, &sw.AssignCount)
		if err != nil {
			return nil, err
		}
		result = append(result, sw)
	}
	return result, nil
}

func (r *ClassroomRepo) GetSubjectsForStudent(studentID int) ([]SubjectWithRole, error) {
	rows, err := r.DB.Query(`
		SELECT s.id, s.title, s.description, s.invite_code, s.created_by, s.created_at,
		       0 as is_owner,
		       (SELECT COUNT(*) FROM subject_teachers WHERE subject_id = s.id) as teacher_count,
		       (SELECT COUNT(*) FROM subject_students WHERE subject_id = s.id) as student_count,
		       (SELECT COUNT(*) FROM assignments WHERE subject_id = s.id) as assign_count
		FROM subjects s
		JOIN subject_students ss ON ss.subject_id = s.id
		WHERE ss.student_id = ?
		ORDER BY s.created_at DESC
	`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SubjectWithRole
	for rows.Next() {
		var sw SubjectWithRole
		err := rows.Scan(&sw.ID, &sw.Title, &sw.Description, &sw.InviteCode,
			&sw.CreatedBy, &sw.CreatedAt, &sw.IsOwner,
			&sw.TeacherCount, &sw.StudentCount, &sw.AssignCount)
		if err != nil {
			return nil, err
		}
		result = append(result, sw)
	}
	return result, nil
}

func (r *ClassroomRepo) GetSubjectByID(id int) (*Subject, error) {
	s := &Subject{}
	err := r.DB.QueryRow(`SELECT id, title, description, invite_code, created_by, created_at FROM subjects WHERE id = ?`, id).
		Scan(&s.ID, &s.Title, &s.Description, &s.InviteCode, &s.CreatedBy, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *ClassroomRepo) GetSubjectByInviteCode(code string) (*Subject, error) {
	s := &Subject{}
	err := r.DB.QueryRow(`SELECT id, title, description, invite_code, created_by, created_at FROM subjects WHERE invite_code = ?`, code).
		Scan(&s.ID, &s.Title, &s.Description, &s.InviteCode, &s.CreatedBy, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *ClassroomRepo) IsTeacherOfSubject(subjectID, teacherID int) (bool, error) {
	var count int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM subject_teachers WHERE subject_id=? AND teacher_id=?`, subjectID, teacherID).Scan(&count)
	return count > 0, err
}

func (r *ClassroomRepo) IsStudentOfSubject(subjectID, studentID int) (bool, error) {
	var count int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM subject_students WHERE subject_id=? AND student_id=?`, subjectID, studentID).Scan(&count)
	return count > 0, err
}

func (r *ClassroomRepo) JoinSubject(subjectID, studentID int) error {
	_, err := r.DB.Exec(`INSERT OR IGNORE INTO subject_students (subject_id, student_id) VALUES (?,?)`, subjectID, studentID)
	return err
}

func (r *ClassroomRepo) DeleteSubject(id int) error {
	_, err := r.DB.Exec(`DELETE FROM subjects WHERE id = ?`, id)
	return err
}

func (r *ClassroomRepo) UpdateSubject(id int, title, description string) error {
	_, err := r.DB.Exec(`UPDATE subjects SET title=?, description=? WHERE id=?`, title, description, id)
	return err
}

func (r *ClassroomRepo) CreateAssignment(subjectID int, title, description, conditionFile string, deadline *time.Time, maxScore int, strictDeadline bool, gradingMode string, allowComments bool, publishAt *time.Time) (int, error) {
	res, err := r.DB.Exec(
		`INSERT INTO assignments (subject_id, title, description, condition_file, deadline, max_score, strict_deadline, grading_mode, allow_comments, publish_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		subjectID, title, description, conditionFile, deadline, maxScore, strictDeadline, gradingMode, allowComments, publishAt,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func (r *ClassroomRepo) CreateAssignmentTasks(assignmentID int, tasks []AssignmentTask) error {
	for _, t := range tasks {
		_, err := r.DB.Exec(
			`INSERT INTO assignment_tasks (assignment_id, task_number, label, max_score) VALUES (?,?,?,?)`,
			assignmentID, t.TaskNumber, t.Label, t.MaxScore,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ClassroomRepo) GetTasksByAssignment(assignmentID int) ([]AssignmentTask, error) {
	rows, err := r.DB.Query(
		`SELECT id, assignment_id, task_number, label, max_score FROM assignment_tasks WHERE assignment_id=? ORDER BY task_number`,
		assignmentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []AssignmentTask
	for rows.Next() {
		var t AssignmentTask
		if err := rows.Scan(&t.ID, &t.AssignmentID, &t.TaskNumber, &t.Label, &t.MaxScore); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (r *ClassroomRepo) SaveTaskGrades(submissionID int, grades map[int]int) error {
	for taskNum, score := range grades {
		_, err := r.DB.Exec(
			`INSERT INTO submission_task_grades (submission_id, task_number, score) VALUES (?,?,?)
			 ON CONFLICT(submission_id, task_number) DO UPDATE SET score=excluded.score`,
			submissionID, taskNum, score,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ClassroomRepo) GetTaskGrades(submissionID int) ([]SubmissionTaskGrade, error) {
	rows, err := r.DB.Query(`
		SELECT stg.task_number, stg.score, COALESCE(at.label,''), COALESCE(at.max_score,0)
		FROM submission_task_grades stg
		LEFT JOIN assignment_tasks at ON at.task_number = stg.task_number
		  AND at.assignment_id = (SELECT assignment_id FROM submissions WHERE id = ?)
		WHERE stg.submission_id = ?
		ORDER BY stg.task_number
	`, submissionID, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grades []SubmissionTaskGrade
	for rows.Next() {
		var g SubmissionTaskGrade
		if err := rows.Scan(&g.TaskNumber, &g.Score, &g.Label, &g.MaxScore); err != nil {
			return nil, err
		}
		grades = append(grades, g)
	}
	return grades, nil
}

func (r *ClassroomRepo) DeleteSubmission(id int, filePath string, baseDir string) error {
	_, err := r.DB.Exec(`DELETE FROM submissions WHERE id=?`, id)
	if err != nil {
		return err
	}
	if filePath != "" {
		os.Remove(filepath.Join(baseDir, "uploads", "submissions", filePath))
	}
	return nil
}

func (r *ClassroomRepo) GetAssignmentsBySubject(subjectID int) ([]Assignment, error) {
	rows, err := r.DB.Query(`
		SELECT a.id, a.subject_id, a.title, COALESCE(a.description,''), COALESCE(a.condition_file,''),
		       a.deadline, COALESCE(a.max_score, 100), a.created_at,
		       (SELECT COUNT(*) FROM submissions WHERE assignment_id = a.id) as submit_count,
		       (SELECT COUNT(*) FROM submissions WHERE assignment_id = a.id AND status = 'reviewed') as reviewed_count,
		       COALESCE(a.strict_deadline, 0), COALESCE(a.grading_mode, 'total'), COALESCE(a.allow_comments, 1), a.publish_at
		FROM assignments a
		WHERE a.subject_id = ?
		ORDER BY a.created_at DESC
	`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssignments(rows)
}

func (r *ClassroomRepo) GetAssignmentByID(id int) (*Assignment, error) {
	rows, err := r.DB.Query(`
		SELECT a.id, a.subject_id, a.title, COALESCE(a.description,''), COALESCE(a.condition_file,''),
		       a.deadline, COALESCE(a.max_score, 100), a.created_at,
		       (SELECT COUNT(*) FROM submissions WHERE assignment_id = a.id) as submit_count,
		       (SELECT COUNT(*) FROM submissions WHERE assignment_id = a.id AND status = 'reviewed') as reviewed_count,
		       COALESCE(a.strict_deadline, 0), COALESCE(a.grading_mode, 'total'), COALESCE(a.allow_comments, 1), a.publish_at
		FROM assignments a WHERE a.id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanAssignments(rows)
	if err != nil {
		return nil, err
	}
	// «не найдено» — это ошибка, а не (nil, nil): иначе хендлеры, проверяющие
	// только err, разыменовывают nil на несуществующем ID задания
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return &list[0], nil
}

func scanAssignments(rows *sql.Rows) ([]Assignment, error) {
	var result []Assignment
	for rows.Next() {
		var a Assignment
		var deadline, publishAt sql.NullTime
		var strict, allowComments int
		err := rows.Scan(&a.ID, &a.SubjectID, &a.Title, &a.Description, &a.ConditionFile,
			&deadline, &a.MaxScore, &a.CreatedAt, &a.SubmitCount, &a.ReviewedCount, &strict, &a.GradingMode, &allowComments, &publishAt)
		if err != nil {
			return nil, err
		}
		if deadline.Valid {
			a.Deadline = &deadline.Time
		}
		if publishAt.Valid {
			a.PublishAt = &publishAt.Time
		}
		a.StrictDeadline = strict == 1
		a.AllowComments = allowComments == 1
		result = append(result, a)
	}
	return result, nil
}

func (r *ClassroomRepo) SaveVariants(assignmentID int, variants []string) error {
	r.DB.Exec(`DELETE FROM assignment_variants WHERE assignment_id=?`, assignmentID)
	for i, fp := range variants {
		if _, err := r.DB.Exec(`INSERT INTO assignment_variants (assignment_id, variant_index, file_path) VALUES (?,?,?)`, assignmentID, i+1, fp); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClassroomRepo) AssignVariantsToStudents(assignmentID int, variants []string) error {
	students, err := r.GetStudentsBySubjectForAssignment(assignmentID)
	if err != nil {
		return err
	}
	rand.Shuffle(len(students), func(i, j int) { students[i], students[j] = students[j], students[i] })
	r.DB.Exec(`DELETE FROM student_variant_assignments WHERE assignment_id=?`, assignmentID)
	for i, s := range students {
		variantIdx := i % len(variants)
		_, err := r.DB.Exec(`INSERT OR REPLACE INTO student_variant_assignments (assignment_id, student_id, variant_index, file_path) VALUES (?,?,?,?)`,
			assignmentID, s.ID, variantIdx+1, variants[variantIdx])
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ClassroomRepo) GetStudentsBySubjectForAssignment(assignmentID int) ([]SubjectStudent, error) {
	rows, err := r.DB.Query(`
		SELECT u.id, COALESCE(u.full_name, u.login), u.login, COALESCE(u.group_number,'')
		FROM subject_students ss
		JOIN users u ON u.id = ss.student_id
		JOIN assignments a ON a.subject_id = ss.subject_id
		WHERE a.id = ?
		ORDER BY ss.joined_at, ss.student_id
	`, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SubjectStudent
	for rows.Next() {
		var s SubjectStudent
		if err := rows.Scan(&s.ID, &s.FullName, &s.Login, &s.Group); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func (r *ClassroomRepo) GetStudentVariant(assignmentID, studentID int) (string, int, error) {
	var filePath string
	var variantIdx int
	err := r.DB.QueryRow(`SELECT file_path, variant_index FROM student_variant_assignments WHERE assignment_id=? AND student_id=?`, assignmentID, studentID).Scan(&filePath, &variantIdx)
	return filePath, variantIdx, err
}

func (r *ClassroomRepo) GetVariants(assignmentID int) ([]string, error) {
	rows, err := r.DB.Query(`SELECT file_path FROM assignment_variants WHERE assignment_id=? ORDER BY variant_index`, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var fp string
		rows.Scan(&fp)
		result = append(result, fp)
	}
	return result, nil
}

func (r *ClassroomRepo) UpdateAssignment(id int, title, description, conditionFile string, deadline *time.Time, maxScore int, strictDeadline bool, gradingMode string, allowComments bool, publishAt *time.Time) error {
	_, err := r.DB.Exec(
		`UPDATE assignments SET title=?, description=?, condition_file=?, deadline=?, max_score=?, strict_deadline=?, grading_mode=?, allow_comments=?, publish_at=? WHERE id=?`,
		title, description, conditionFile, deadline, maxScore, strictDeadline, gradingMode, allowComments, publishAt, id,
	)
	return err
}

func (r *ClassroomRepo) DeleteAssignment(id int) error {
	_, err := r.DB.Exec(`DELETE FROM assignments WHERE id = ?`, id)
	return err
}

func (r *ClassroomRepo) CreateSubmission(assignmentID, studentID int, filePath string) (int, error) {
	res, err := r.DB.Exec(
		`INSERT INTO submissions (assignment_id, student_id, file_path, submitted_at) VALUES (?,?,?,?)`,
		assignmentID, studentID, filePath, time.Now(),
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func (r *ClassroomRepo) GetSubmissionByID(id int) (*Submission, error) {
	s := &Submission{}
	var annotated, comment, pinsJSON sql.NullString
	var reviewedAt sql.NullTime
	var grade sql.NullInt64
	err := r.DB.QueryRow(`
		SELECT s.id, s.assignment_id, s.student_id, s.file_path,
		       COALESCE(s.annotated_file_path,''), s.status, COALESCE(s.teacher_comment,''),
		       s.grade, s.submitted_at, s.reviewed_at,
		       u.full_name, COALESCE(u.group_number,''),
		       a.title, subj.title, COALESCE(s.pins_json,'')
		FROM submissions s
		JOIN users u ON u.id = s.student_id
		JOIN assignments a ON a.id = s.assignment_id
		JOIN subjects subj ON subj.id = a.subject_id
		WHERE s.id = ?
	`, id).Scan(&s.ID, &s.AssignmentID, &s.StudentID, &s.FilePath,
		&annotated, &s.Status, &comment,
		&grade, &s.SubmittedAt, &reviewedAt,
		&s.StudentName, &s.StudentGroup,
		&s.AssignmentTitle, &s.SubjectTitle, &pinsJSON)
	if err != nil {
		return nil, err
	}
	s.AnnotatedFilePath = annotated.String
	s.TeacherComment = comment.String
	s.PinsJSON = pinsJSON.String
	if grade.Valid {
		v := int(grade.Int64)
		s.Grade = &v
	}
	if reviewedAt.Valid {
		s.ReviewedAt = &reviewedAt.Time
	}
	return s, nil
}

func (r *ClassroomRepo) GetSubmissionByStudentAndAssignment(assignmentID, studentID int) (*Submission, error) {
	var id int
	err := r.DB.QueryRow(`SELECT id FROM submissions WHERE assignment_id=? AND student_id=?`, assignmentID, studentID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetSubmissionByID(id)
}

func (r *ClassroomRepo) GetSubmissionsByAssignment(assignmentID int) ([]Submission, error) {
	rows, err := r.DB.Query(`
		SELECT s.id, s.assignment_id, s.student_id, s.file_path,
		       COALESCE(s.annotated_file_path,''), s.status, COALESCE(s.teacher_comment,''),
		       s.grade, s.submitted_at, s.reviewed_at,
		       u.full_name, COALESCE(u.group_number,''), '', ''
		FROM submissions s
		JOIN users u ON u.id = s.student_id
		WHERE s.assignment_id = ?
		ORDER BY s.submitted_at DESC
	`, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubmissions(rows)
}

func scanSubmissions(rows *sql.Rows) ([]Submission, error) {
	var result []Submission
	for rows.Next() {
		var s Submission
		var annotated, comment sql.NullString
		var grade sql.NullInt64
		var reviewedAt sql.NullTime
		err := rows.Scan(&s.ID, &s.AssignmentID, &s.StudentID, &s.FilePath,
			&annotated, &s.Status, &comment,
			&grade, &s.SubmittedAt, &reviewedAt,
			&s.StudentName, &s.StudentGroup,
			&s.AssignmentTitle, &s.SubjectTitle)
		if err != nil {
			return nil, err
		}
		s.AnnotatedFilePath = annotated.String
		s.TeacherComment = comment.String
		if grade.Valid {
			v := int(grade.Int64)
			s.Grade = &v
		}
		if reviewedAt.Valid {
			s.ReviewedAt = &reviewedAt.Time
		}
		result = append(result, s)
	}
	return result, nil
}

// GetNearestAssignment — берём ближайший будущий дедлайн; фильтруем в Go, а не в SQL,
// потому что формат дат в SQLite ненадёжно сравнивается на разных платформах.
func (r *ClassroomRepo) GetNearestAssignment(subjectID int) (*Assignment, error) {
	rows, err := r.DB.Query(`
		SELECT a.id, a.subject_id, a.title, COALESCE(a.description,''), COALESCE(a.condition_file,''),
		       a.deadline, COALESCE(a.max_score, 100), a.created_at, 0, 0, COALESCE(a.strict_deadline,0), COALESCE(a.grading_mode,'total'), COALESCE(a.allow_comments,1), a.publish_at
		FROM assignments a
		WHERE a.subject_id = ?
		ORDER BY a.deadline ASC
	`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanAssignments(rows)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	// ищем первый дедлайн в будущем; запланированные (ещё не выданные) пропускаем,
	// чтобы контрольная не светилась на карточке предмета до времени выдачи
	for i := range list {
		if list[i].IsScheduled() {
			continue
		}
		if list[i].Deadline != nil && list[i].Deadline.After(now) {
			return &list[i], nil
		}
	}
	// нет будущих дедлайнов — показываем последнее по дате создания (тоже без запланированных)
	var best *Assignment
	for i := range list {
		if list[i].IsScheduled() {
			continue
		}
		if best == nil || list[i].CreatedAt.After(best.CreatedAt) {
			best = &list[i]
		}
	}
	return best, nil
}

func (r *ClassroomRepo) GetStudentFilesForSubject(subjectID, studentID int) ([]Submission, error) {
	rows, err := r.DB.Query(`
		SELECT s.id, s.assignment_id, s.student_id, s.file_path,
		       COALESCE(s.annotated_file_path,''), s.status, COALESCE(s.teacher_comment,''),
		       s.grade, s.submitted_at, s.reviewed_at,
		       '', '', a.title, ''
		FROM submissions s
		JOIN assignments a ON a.id = s.assignment_id
		WHERE a.subject_id = ? AND s.student_id = ?
		ORDER BY s.submitted_at DESC
	`, subjectID, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubmissions(rows)
}

func (r *ClassroomRepo) SaveAnnotatedFile(submissionID int, annotatedPath, comment, pinsJSON, annotationsJSON, strokesJSON string) error {
	_, err := r.DB.Exec(`
		UPDATE submissions SET annotated_file_path=?, teacher_comment=?, pins_json=?, annotations_json=?, strokes_json=?, status='reviewed', reviewed_at=?
		WHERE id=?
	`, annotatedPath, comment, pinsJSON, annotationsJSON, strokesJSON, time.Now(), submissionID)
	return err
}

func (r *ClassroomRepo) GetStrokes(submissionID int) (string, error) {
	var strokes sql.NullString
	err := r.DB.QueryRow(`SELECT COALESCE(strokes_json,'') FROM submissions WHERE id=?`, submissionID).Scan(&strokes)
	return strokes.String, err
}

func (r *ClassroomRepo) GetPins(submissionID int) (string, error) {
	var pins sql.NullString
	err := r.DB.QueryRow(`SELECT COALESCE(pins_json,'') FROM submissions WHERE id=?`, submissionID).Scan(&pins)
	return pins.String, err
}

func (r *ClassroomRepo) GetAnnotations(submissionID int) (string, error) {
	var anns sql.NullString
	err := r.DB.QueryRow(`SELECT COALESCE(annotations_json,'') FROM submissions WHERE id=?`, submissionID).Scan(&anns)
	return anns.String, err
}

func (r *ClassroomRepo) SaveGrade(submissionID, grade int) error {
	_, err := r.DB.Exec(`UPDATE submissions SET grade=? WHERE id=?`, grade, submissionID)
	return err
}

func (r *ClassroomRepo) CountUnreviewedForTeacher(teacherID int) int {
	var count int
	r.DB.QueryRow(`
		SELECT COUNT(*) FROM submissions s
		JOIN assignments a ON a.id = s.assignment_id
		JOIN subject_teachers st ON st.subject_id = a.subject_id
		WHERE st.teacher_id = ? AND s.status = 'submitted'
	`, teacherID).Scan(&count)
	return count
}

func (r *ClassroomRepo) CountNewReviewsForStudent(studentID int) int {
	var count int
	r.DB.QueryRow(`
		SELECT COUNT(*) FROM submissions
		WHERE student_id = ? AND status = 'reviewed' AND COALESCE(student_seen, 0) = 0
	`, studentID).Scan(&count)
	return count
}

func (r *ClassroomRepo) MarkSubmissionSeenByStudent(submissionID int) {
	r.DB.Exec(`UPDATE submissions SET student_seen=1 WHERE id=?`, submissionID)
}

type SubjectStudent struct {
	ID       int
	FullName string
	Login    string
	Group    string
}

func (r *ClassroomRepo) GetStudentsBySubject(subjectID int) ([]SubjectStudent, error) {
	rows, err := r.DB.Query(`
		SELECT u.id, COALESCE(u.full_name, u.login), u.login, COALESCE(u.group_number,'')
		FROM subject_students ss
		JOIN users u ON u.id = ss.student_id
		WHERE ss.subject_id = ?
		ORDER BY u.full_name, u.login
	`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SubjectStudent
	for rows.Next() {
		var s SubjectStudent
		if err := rows.Scan(&s.ID, &s.FullName, &s.Login, &s.Group); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func (r *ClassroomRepo) RemoveStudentFromSubject(subjectID, studentID int) error {
	_, err := r.DB.Exec(`DELETE FROM subject_students WHERE subject_id=? AND student_id=?`, subjectID, studentID)
	return err
}

// AutoAssignVariant — назначает вариант студенту если он ещё не получил его.
// Используется при входе на страницу задания, если студент записался после создания.
func (r *ClassroomRepo) AutoAssignVariant(assignmentID, studentID int) (string, int, error) {
	variants, err := r.GetVariants(assignmentID)
	if err != nil || len(variants) == 0 {
		return "", 0, nil
	}
	students, err := r.GetStudentsBySubjectForAssignment(assignmentID)
	if err != nil {
		return "", 0, err
	}
	pos := 0
	for i, s := range students {
		if s.ID == studentID {
			pos = i
			break
		}
	}
	variantIdx := pos % len(variants)
	filePath := variants[variantIdx]
	_, err = r.DB.Exec(
		`INSERT OR IGNORE INTO student_variant_assignments (assignment_id, student_id, variant_index, file_path) VALUES (?,?,?,?)`,
		assignmentID, studentID, variantIdx+1, filePath,
	)
	if err != nil {
		return "", 0, err
	}
	return filePath, variantIdx + 1, nil
}

func (r *ClassroomRepo) GetSubmissionComments(submissionID int) ([]SubmissionComment, error) {
	rows, err := r.DB.Query(`
		SELECT sc.id, sc.submission_id, sc.user_id,
		       COALESCE(u.full_name, u.login), sc.user_type, sc.message, sc.created_at
		FROM submission_comments sc
		JOIN users u ON u.id = sc.user_id
		WHERE sc.submission_id = ?
		ORDER BY sc.created_at ASC
	`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SubmissionComment
	for rows.Next() {
		var c SubmissionComment
		if err := rows.Scan(&c.ID, &c.SubmissionID, &c.UserID, &c.UserName, &c.UserType, &c.Message, &c.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *ClassroomRepo) AddSubmissionComment(submissionID, userID int, userType, message string) error {
	_, err := r.DB.Exec(
		`INSERT INTO submission_comments (submission_id, user_id, user_type, message) VALUES (?,?,?,?)`,
		submissionID, userID, userType, message,
	)
	return err
}

func (r *ClassroomRepo) GetDiscussionPosts(subjectID int) ([]DiscussionPost, error) {
	rows, err := r.DB.Query(`
		SELECT sd.id, sd.subject_id, sd.user_id,
		       COALESCE(u.full_name, u.login), sd.user_type, sd.message, sd.created_at
		FROM subject_discussions sd
		JOIN users u ON u.id = sd.user_id
		WHERE sd.subject_id = ?
		ORDER BY sd.created_at ASC
	`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DiscussionPost
	for rows.Next() {
		var p DiscussionPost
		if err := rows.Scan(&p.ID, &p.SubjectID, &p.UserID, &p.UserName, &p.UserType, &p.Message, &p.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (r *ClassroomRepo) AddDiscussionPost(subjectID, userID int, userType, message string) error {
	_, err := r.DB.Exec(
		`INSERT INTO subject_discussions (subject_id, user_id, user_type, message) VALUES (?,?,?,?)`,
		subjectID, userID, userType, message,
	)
	return err
}

// GetDiscussionPost возвращает один пост — нужен для проверки прав при удалении.
func (r *ClassroomRepo) GetDiscussionPost(postID int) (DiscussionPost, error) {
	var p DiscussionPost
	err := r.DB.QueryRow(
		`SELECT id, subject_id, user_id, user_type, message, created_at FROM subject_discussions WHERE id = ?`,
		postID,
	).Scan(&p.ID, &p.SubjectID, &p.UserID, &p.UserType, &p.Message, &p.CreatedAt)
	return p, err
}

func (r *ClassroomRepo) DeleteDiscussionPost(postID int) error {
	_, err := r.DB.Exec(`DELETE FROM subject_discussions WHERE id = ?`, postID)
	return err
}
