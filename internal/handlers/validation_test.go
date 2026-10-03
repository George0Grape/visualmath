package handlers

import "testing"

func validateReq(login, email, fullName string) error {
	return validate.Struct(&RegisterRequest{
		Login:    login,
		Password: "pass123",
		Email:    email,
		FullName: fullName,
	})
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestValidateLogin(t *testing.T) {
	tests := []struct {
		name    string
		login   string
		wantErr bool
	}{
		{"too short 1 char", "a", true},
		{"too short 2 chars", "ab", true},
		{"min length 3", "abc", false},
		{"max length 30", "abcdefghij1234567890abcdefghij", false},
		{"too long 31", "abcdefghij1234567890abcdefghij1", true},
		{"latin letters", "johnDoe", false},
		{"cyrillic letters", "Иван", false},
		{"digits", "user123", false},
		{"dot", "user.name", false},
		{"dash", "user-name", false},
		{"underscore", "user_name", false},
		{"space forbidden", "user name", true},
		{"at-sign forbidden", "user@mail", true},
		{"slash forbidden", "user/name", true},
		{"null byte", "abc\x00def", true},
		{"newline forbidden", "abc\ndef", true},
		{"sql single quote", "a'; DROP TABLE users;--", true},
		{"sql union", "user UNION SELECT", true},
		{"xss script tag", "<script>alert(1)</script>", true},
		{"xss angle bracket", "user<b>", true},
		{"xss ampersand", "user&name", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReq(tt.login, "u@example.com", "")
			if tt.wantErr && err == nil {
				t.Errorf("login=%q: expected error, got nil", tt.login)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("login=%q: unexpected error: %v", tt.login, err)
			}
		})
	}
}

// ── Email ─────────────────────────────────────────────────────────────────────

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid simple", "user@example.com", false},
		{"valid subdomain", "user@mail.example.com", false},
		{"valid plus", "user+tag@example.com", false},
		{"valid dots", "first.last@example.org", false},
		{"no at", "userexample.com", true},
		{"no domain", "user@", true},
		{"no tld", "user@example", true},
		{"empty", "", true},
		{"double at", "user@@example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReq("validlogin", tt.email, "")
			if tt.wantErr && err == nil {
				t.Errorf("email=%q: expected error, got nil", tt.email)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("email=%q: unexpected error: %v", tt.email, err)
			}
		})
	}
}

// ── FullName ──────────────────────────────────────────────────────────────────

func TestValidateFullName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid ru", "Иванов Иван", false},
		{"valid en", "John Smith", false},
		{"min 2 chars", "Ян", false},
		{"single char", "Я", true},
		{"empty is ok (omitempty)", "", false},
		{"too long 101", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaа", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReq("validlogin", "u@example.com", tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("fullName=%q: expected error, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("fullName=%q: unexpected error: %v", tt.input, err)
			}
		})
	}
}
