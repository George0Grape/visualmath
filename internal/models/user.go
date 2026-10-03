package models

import "time"

type User struct {
    ID            int       `json:"id"`
    Login         string    `json:"login"`
    PasswordHash  string    `json:"-"`
    FullName      string    `json:"full_name"`
    UserType      string    `json:"user_type"`
    GroupNumber   string    `json:"group_number"`
    Email         string    `json:"email"`
    EmailVerified bool      `json:"email_verified"`
    CreatedAt     time.Time `json:"created_at"`
}