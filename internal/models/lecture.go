package models

import "time"

type Lecture struct {
    ID          int       `json:"id"`
    Title       string    `json:"title"`
    CourseID    int       `json:"course_id"`
    CourseName  string    `json:"course_name"`
    AuthorID    int       `json:"author_id"`
    AuthorName  string    `json:"author_name"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
}