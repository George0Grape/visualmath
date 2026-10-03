package storage

import (
	"database/sql"
	"time"
)

// GradeCell — «сырая» оценка одного студента по одному заданию.
// Используется и для аналитики, и для сводной ведомости (матрицы).
type GradeCell struct {
	StudentID    int
	AssignmentID int
	Grade        *int
	Status       string
	SubmittedAt  time.Time
}

// GetGradesForSubject — все сдачи по всем заданиям предмета одним запросом.
func (r *ClassroomRepo) GetGradesForSubject(subjectID int) ([]GradeCell, error) {
	rows, err := r.DB.Query(`
		SELECT s.student_id, s.assignment_id, s.grade, s.status, s.submitted_at
		FROM submissions s
		JOIN assignments a ON a.id = s.assignment_id
		WHERE a.subject_id = ?
	`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GradeCell
	for rows.Next() {
		var c GradeCell
		var grade sql.NullInt64
		if err := rows.Scan(&c.StudentID, &c.AssignmentID, &grade, &c.Status, &c.SubmittedAt); err != nil {
			return nil, err
		}
		if grade.Valid {
			v := int(grade.Int64)
			c.Grade = &v
		}
		result = append(result, c)
	}
	return result, nil
}

// AssignmentStat — агрегаты по одному заданию для страницы аналитики.
type AssignmentStat struct {
	AssignmentID  int
	Title         string
	MaxScore      int
	StudentsTotal int
	Submitted     int     // сколько студентов сдали
	Reviewed      int     // сколько проверено
	GradedCount   int     // у скольких выставлен балл
	AvgGrade      float64 // средний балл среди выставленных
	SubmitRate    int     // % сдавших от всех студентов предмета
}

// SubjectAnalytics — сводка по предмету.
type SubjectAnalytics struct {
	StudentsTotal    int
	AssignmentsTotal int
	OverallAvg       float64 // средний балл по всем проверенным работам предмета
	TotalReviewed    int
	TotalPending     int // сдано, но ещё не проверено
	Assignments      []AssignmentStat
}

// GetSubjectAnalytics считает агрегаты в Go (не в SQL) — сдач на предмет немного,
// зато логика прозрачна и не зависит от формата дат в SQLite.
func (r *ClassroomRepo) GetSubjectAnalytics(subjectID int) (*SubjectAnalytics, error) {
	assignments, err := r.GetAssignmentsBySubject(subjectID)
	if err != nil {
		return nil, err
	}
	students, err := r.GetStudentsBySubject(subjectID)
	if err != nil {
		return nil, err
	}
	grades, err := r.GetGradesForSubject(subjectID)
	if err != nil {
		return nil, err
	}

	studentsTotal := len(students)

	byAssign := map[int][]GradeCell{}
	for _, g := range grades {
		byAssign[g.AssignmentID] = append(byAssign[g.AssignmentID], g)
	}

	an := &SubjectAnalytics{
		StudentsTotal:    studentsTotal,
		AssignmentsTotal: len(assignments),
	}
	var totalSum float64
	var totalGraded int

	for _, a := range assignments {
		stat := AssignmentStat{
			AssignmentID:  a.ID,
			Title:         a.Title,
			MaxScore:      a.MaxScore,
			StudentsTotal: studentsTotal,
		}
		var sum float64
		seen := map[int]bool{}
		for _, c := range byAssign[a.ID] {
			if seen[c.StudentID] {
				continue // у студента максимум одна сдача, но подстрахуемся
			}
			seen[c.StudentID] = true
			stat.Submitted++
			if c.Status == "reviewed" {
				stat.Reviewed++
			} else {
				an.TotalPending++
			}
			if c.Grade != nil {
				sum += float64(*c.Grade)
				stat.GradedCount++
			}
		}
		if stat.GradedCount > 0 {
			stat.AvgGrade = sum / float64(stat.GradedCount)
		}
		if studentsTotal > 0 {
			stat.SubmitRate = int(float64(stat.Submitted)/float64(studentsTotal)*100 + 0.5)
		}
		an.TotalReviewed += stat.Reviewed
		totalSum += sum
		totalGraded += stat.GradedCount
		an.Assignments = append(an.Assignments, stat)
	}
	if totalGraded > 0 {
		an.OverallAvg = totalSum / float64(totalGraded)
	}
	return an, nil
}
