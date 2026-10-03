package sync

import (
	"math"
	"testing"
)

// ─── parseIdxSet ────────────────────────────────────────────────────────────
// Парсит строку "0,2" в map[int]bool{0:true, 2:true}.
// Тестируем: нормальный вход, пустая строка, мусор, пробелы.

func TestParseIdxSet(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[int]bool
	}{
		{"single", "0", map[int]bool{0: true}},
		{"multiple", "0,2,5", map[int]bool{0: true, 2: true, 5: true}},
		{"with spaces", " 1 , 3 ", map[int]bool{1: true, 3: true}},
		{"empty string", "", map[int]bool{}},
		{"garbage ignored", "abc,1,xyz", map[int]bool{1: true}},
		{"duplicate", "2,2,2", map[int]bool{2: true}},
		{"trailing comma", "1,2,", map[int]bool{1: true, 2: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIdxSet(tt.in)
			if len(got) != len(tt.want) {
				t.Errorf("parseIdxSet(%q) = %v, want %v", tt.in, got, tt.want)
				return
			}
			for k := range tt.want {
				if !got[k] {
					t.Errorf("parseIdxSet(%q) missing key %d", tt.in, k)
				}
			}
		})
	}
}

// ─── correctIdxsToStr ───────────────────────────────────────────────────────
// Обратная операция: []int{0,2} → "0,2"

func TestCorrectIdxsToStr(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want string
	}{
		{"single", []int{0}, "0"},
		{"multiple", []int{0, 2, 5}, "0,2,5"},
		{"empty", []int{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := correctIdxsToStr(tt.in)
			if got != tt.want {
				t.Errorf("correctIdxsToStr(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ─── answersMatch ───────────────────────────────────────────────────────────
// Ключевая функция: сверяет ответ студента с правильными.
// "0,2" совпадает с "0,2" (порядок неважен), "0" не совпадает с "0,2".

func TestAnswersMatch(t *testing.T) {
	tests := []struct {
		name       string
		studentAns string
		correctIdx string
		want       bool
	}{
		{"exact match single", "0", "0", true},
		{"exact match multi", "0,2", "0,2", true},
		{"different order", "2,0", "0,2", true},
		{"missing one", "0", "0,2", false},
		{"extra answer", "0,1,2", "0,2", false},
		{"completely wrong", "3", "0", false},
		{"empty student", "", "0", false},
		{"both empty", "", "", false},
		{"with spaces", " 0 , 2 ", "0,2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := answersMatch(tt.studentAns, tt.correctIdx)
			if got != tt.want {
				t.Errorf("answersMatch(%q, %q) = %v, want %v",
					tt.studentAns, tt.correctIdx, got, tt.want)
			}
		})
	}
}

func TestIsSameStudent(t *testing.T) {
	tests := []struct {
		name string
		a, b *StudentConn
		want bool
	}{
		{
			"same ID",
			&StudentConn{ID: 42, Name: "Alice"},
			&StudentConn{ID: 42, Name: "Bob"}, 
			true,
		},
		{
			"different ID",
			&StudentConn{ID: 42, Name: "Alice"},
			&StudentConn{ID: 99, Name: "Alice"},
			false,
		},
		{
			"same name, no ID (guests)",
			&StudentConn{ID: 0, Name: "Гость"},
			&StudentConn{ID: 0, Name: "Гость"},
			true,
		},
		{
			"different name, no ID",
			&StudentConn{ID: 0, Name: "Alice"},
			&StudentConn{ID: 0, Name: "Bob"},
			false,
		},
		{
			"one has ID, other doesn't",
			&StudentConn{ID: 42, Name: "Alice"},
			&StudentConn{ID: 0, Name: "Alice"},
			false, 
		},
		{
			"both empty",
			&StudentConn{ID: 0, Name: ""},
			&StudentConn{ID: 0, Name: ""},
			false, 
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSameStudent(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("isSameStudent(%+v, %+v) = %v, want %v",
					tt.a, tt.b, got, tt.want)
			}
		})
	}
}


func TestExtractCorrectIdx(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want int
	}{
		{"float64", float64(3), 3},
		{"array with one", []interface{}{float64(2)}, 2},
		{"array with many", []interface{}{float64(5), float64(1)}, 5}, // берёт первый
		{"empty array", []interface{}{}, 0},
		{"nil", nil, 0},
		{"string (unexpected)", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCorrectIdx(tt.val)
			if got != tt.want {
				t.Errorf("extractCorrectIdx(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestBuildModuleResult(t *testing.T) {
	questions := []moduleQuestion{
		{
			Question:    "2+2=?",
			Answers:     []string{"3", "4", "5", "6"},
			CorrectIdxs: []int{1},
		},
		{
			Question:    "Выберите чётные",
			Answers:     []string{"1", "2", "3", "4"},
			CorrectIdxs: []int{1, 3},
		},
	}

	t.Run("all correct", func(t *testing.T) {
		sa := &StudentAnswers{
			Name: "Студент",
			Answers: map[int]map[int]string{
				10: {0: "1", 1: "1,3"}, 
			},
		}
		result := buildModuleResult(10, questions, sa)

		total := result["total_points"].(float64)
		if total != 2.0 {
			t.Errorf("all correct: total_points = %v, want 2.0", total)
		}
	})

	t.Run("all wrong", func(t *testing.T) {
		sa := &StudentAnswers{
			Name: "Студент",
			Answers: map[int]map[int]string{
				10: {0: "0", 1: "0"}, 
			},
		}
		result := buildModuleResult(10, questions, sa)

		total := result["total_points"].(float64)
		if math.Abs(total-(-0.5)) > 0.01 {
			t.Errorf("all wrong: total_points = %v, want -0.5", total)
		}
	})

	t.Run("no answers (nil StudentAnswers)", func(t *testing.T) {
		result := buildModuleResult(10, questions, nil)

		total := result["total_points"].(float64)
		if total != 0.0 {
			t.Errorf("no answers: total_points = %v, want 0.0", total)
		}
	})

	t.Run("partial answers", func(t *testing.T) {
		sa := &StudentAnswers{
			Name: "Студент",
			Answers: map[int]map[int]string{
				10: {0: "1"}, 
			},
		}
		result := buildModuleResult(10, questions, sa)

		total := result["total_points"].(float64)
		if total != 1.0 {
			t.Errorf("partial: total_points = %v, want 1.0", total)
		}

		maxPts := result["max_points"].(float64)
		if maxPts != 2.0 {
			t.Errorf("partial: max_points = %v, want 2.0", maxPts)
		}
	})
}

// ─── answerToLabel ──────────────────────────────────────────────────────────
// Преобразует "0,2" → "а, в" для отображения в таблице результатов.

func TestAnswerToLabel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"first option", "0", "а"},
		{"second option", "1", "б"},
		{"third option", "2", "в"},
		{"fourth option", "3", "г"},
		{"multi select", "0,2", "а, в"},
		{"multi reversed", "2,0", "в, а"},
		{"with spaces", " 0 , 1 ", "а, б"},
		{"large index", "7", "з"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := answerToLabel(tt.in)
			if got != tt.want {
				t.Errorf("answerToLabel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

