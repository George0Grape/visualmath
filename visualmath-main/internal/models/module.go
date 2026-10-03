package models

import (
    "time"
    "encoding/json"
)

type Module struct {
    ID         int       `json:"id"`
    Title      string    `json:"title"`
    CourseID   int       `json:"course_id"`
    CourseName string    `json:"course_name"`
    AuthorID   int       `json:"author_id"`
    AuthorName string    `json:"author_name"`
    ModuleType string    `json:"module_type"` // text, visual, question, test
    Content    json.RawMessage `json:"content"`
    CreatedAt  time.Time `json:"created_at"`
    Published  bool      `json:"published" db:"published"`
}

type TextModuleContent struct {
    Text   string   `json:"text"`
    Images []string `json:"images"`
}

type Question struct {
    Question string   `json:"question"`
    Answers  []string `json:"answers"`
    Correct  int      `json:"correct"`
}

type TestConfig struct {
    TimeLimit      int  `json:"time_limit"`
    QuestionsCount int  `json:"questions_count"`
    PassingScore   int  `json:"passing_score"`
    ShuffleQuestions bool `json:"shuffle_questions"`
    ShowResults     bool `json:"show_results"`
}