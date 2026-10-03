package handlers

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
	"visualmath/internal/storage"
)

func xlsxPtr(v int) *int { return &v }

// Проверяем, что excelize-ведомость открывается обратно и имеет правильную структуру:
// задания развёрнуты (старое слева), итог суммируется, шапка на месте.
func TestBuildGradebookXLSX_Valid(t *testing.T) {
	assignments := []storage.Assignment{ // DESC, как отдаёт репозиторий
		{ID: 2, Title: "ДЗ 2", MaxScore: 10},
		{ID: 1, Title: "ДЗ 1", MaxScore: 5},
	}
	students := []storage.SubjectStudent{{ID: 100, FullName: "Иванов Иван", Group: "БКНАД242"}}
	grades := []storage.GradeCell{
		{StudentID: 100, AssignmentID: 1, Grade: xlsxPtr(4)},
		{StudentID: 100, AssignmentID: 2, Grade: xlsxPtr(8)},
	}

	f, err := excelize.OpenReader(bytes.NewReader(buildGradebookXLSX(assignments, students, grades)))
	if err != nil {
		t.Fatalf("excelize не смог открыть сгенерированный файл: %v", err)
	}
	defer f.Close()
	const sheet = "Ведомость"

	checks := map[string]string{
		"A1": "Студент",
		"C1": "ДЗ 1 (макс. 5)", // старое задание слева
		"D1": "ДЗ 2 (макс. 10)",
		"E1": "Итого",
		"A2": "Иванов Иван",
		"E2": "12", // 4 + 8
	}
	for cell, want := range checks {
		if got, _ := f.GetCellValue(sheet, cell); got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}
}

func TestBuildGradesXLSX_Valid(t *testing.T) {
	a := &storage.Assignment{ID: 1, Title: "КР 1", MaxScore: 20}
	subs := []storage.Submission{
		{StudentID: 1, StudentName: "Петров", StudentGroup: "Г1", Status: "reviewed", Grade: xlsxPtr(15)},
		{StudentID: 1, StudentName: "Петров", StudentGroup: "Г1", Status: "submitted"}, // дубль — игнор
		{StudentID: 2, StudentName: "Сидоров", StudentGroup: "Г2", Status: "submitted"},
	}

	f, err := excelize.OpenReader(bytes.NewReader(buildGradesXLSX(a, subs)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	const sheet = "Баллы"

	checks := map[string]string{
		"D1": "Балл (макс. 20)",
		"A2": "Петров",
		"D2": "15",
		"D3": "—", // Сидоров без оценки
	}
	for cell, want := range checks {
		if got, _ := f.GetCellValue(sheet, cell); got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}
	// Дубль Петрова не должен создать лишнюю строку: шапка + 2 студента = 3
	if rows, _ := f.GetRows(sheet); len(rows) != 3 {
		t.Errorf("ожидалось 3 строки, got %d", len(rows))
	}
}
