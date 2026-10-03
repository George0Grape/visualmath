package handlers

import "testing"

// helper: один вопрос в формате массива, как хранится в content модуля.
func q(question string, correct interface{}) map[string]interface{} {
	m := map[string]interface{}{
		"question": question,
		"answers":  []interface{}{"а", "б", "в"},
	}
	if correct != nil {
		m["correct"] = correct
	}
	return m
}

// Главная гарантия безопасности: поле correct НИКОГДА не уходит студенту.
func TestStripCorrectAnswers_RemovesCorrect(t *testing.T) {
	content := []interface{}{
		q("Сколько будет 2+2?", []interface{}{1.0}),
		q("Выберите чётные", []interface{}{0.0, 2.0}),
	}
	out, ok := stripCorrectAnswers(content, "test").([]interface{})
	if !ok || len(out) != 2 {
		t.Fatalf("ожидали 2 вопроса, got %#v", out)
	}
	for i, item := range out {
		qm := item.(map[string]interface{})
		if _, leaked := qm["correct"]; leaked {
			t.Errorf("вопрос %d: поле correct утекло студенту", i)
		}
		// прочие поля сохранены
		if qm["question"] == nil || qm["answers"] == nil {
			t.Errorf("вопрос %d: потеряны question/answers: %#v", i, qm)
		}
	}
}

// test-модуль: is_multi = (правильных ответов > 1).
func TestStripCorrectAnswers_TestMultiFlag(t *testing.T) {
	cases := []struct {
		name    string
		correct interface{}
		want    bool
	}{
		{"один правильный", []interface{}{1.0}, false},
		{"несколько правильных", []interface{}{0.0, 2.0}, true},
		{"пустой массив", []interface{}{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := stripCorrectAnswers([]interface{}{q("Q", c.correct)}, "test").([]interface{})
			qm := out[0].(map[string]interface{})
			if qm["is_multi"] != c.want {
				t.Errorf("is_multi = %v, want %v", qm["is_multi"], c.want)
			}
		})
	}
}

// question-модуль всегда одиночный выбор, даже если в данных несколько correct.
func TestStripCorrectAnswers_QuestionAlwaysSingle(t *testing.T) {
	out := stripCorrectAnswers([]interface{}{q("Q", []interface{}{0.0, 1.0})}, "question").([]interface{})
	qm := out[0].(map[string]interface{})
	if qm["is_multi"] != false {
		t.Errorf("question-модуль должен быть is_multi=false, got %v", qm["is_multi"])
	}
	if _, leaked := qm["correct"]; leaked {
		t.Error("correct утекло в question-модуле")
	}
}

// Контент не в формате массива возвращается как есть (без паники).
func TestStripCorrectAnswers_NonArrayPassthrough(t *testing.T) {
	in := map[string]interface{}{"text": "просто текст"}
	out := stripCorrectAnswers(in, "text")
	m, ok := out.(map[string]interface{})
	if !ok || m["text"] != "просто текст" {
		t.Errorf("неархивный контент должен вернуться без изменений, got %#v", out)
	}
}
