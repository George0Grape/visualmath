package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"visualmath/internal/auth"
)

// withUserContext добавляет auth.UserClaims в контекст запроса (имитирует AuthMiddleware).
func withUserContext(r *http.Request, userID int, login, userType string) *http.Request {
	claims := auth.UserClaims{
		UserID:   userID,
		Login:    login,
		UserType: userType,
		FullName: "Test User",
	}
	ctx := context.WithValue(r.Context(), auth.UserContextKey, claims)
	return r.WithContext(ctx)
}

// ── helpers ───────────────────────────────────────────────────────────────────

// makeJPEG создаёт минимальный валидный JPEG 10×10.
func makeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("makeJPEG: %v", err)
	}
	return buf.Bytes()
}

// makePNG создаёт минимальный валидный PNG 10×10.
func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("makePNG: %v", err)
	}
	return buf.Bytes()
}

// fakeImageBytes возвращает произвольные байты, которые не являются валидным изображением.
func fakeImageBytes() []byte {
	// Случайные байты без магических сигнатур изображений
	return bytes.Repeat([]byte{0x42, 0x41, 0x44, 0x46, 0x49, 0x4C, 0x45}, 50)
}

// buildMultipartRequest формирует multipart/form-data запрос с файлом.
func buildMultipartRequest(t *testing.T, fieldName, filename, contentType string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatalf("copy data: %v", err)
	}
	w.Close()

	req := httptest.NewRequest("POST", "/api/profile/avatar", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	// Переопределяем Content-Type файла (если нужно)
	_ = contentType
	return req
}

// buildMultipartWithContentType формирует multipart с явным Content-Type части.
func buildMultipartWithContentType(t *testing.T, filename, partContentType string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="avatar"; filename="` + filename + `"`}
	h["Content-Type"] = []string{partContentType}

	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatalf("createPart: %v", err)
	}
	part.Write(data)
	mw.Close()

	req := httptest.NewRequest("POST", "/api/profile/avatar", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// ── Avatar Upload Security Tests ──────────────────────────────────────────────

func TestAvatarUpload_ValidJPEG(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "alice", "alice@example.com", "pass", "student")
	h := &ProfileHandler{DB: db}

	req := buildMultipartRequest(t, "avatar", "photo.jpg", "image/jpeg", makeJPEG(t))
	req = withUserContext(req, int(userID), "alice", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.UploadAvatar).ServeHTTP(w, req)

	// Валидный JPEG должен быть принят (200) или обработан без 500
	if w.Code == http.StatusInternalServerError {
		t.Errorf("valid JPEG upload should not cause 500, got: %s", w.Body.String())
	}
}

func TestAvatarUpload_ValidPNG(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "bob", "bob@example.com", "pass", "student")
	h := &ProfileHandler{DB: db}

	req := buildMultipartRequest(t, "avatar", "photo.png", "image/png", makePNG(t))
	req = withUserContext(req, int(userID), "bob", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.UploadAvatar).ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Errorf("valid PNG upload should not cause 500, got: %s", w.Body.String())
	}
}

func TestAvatarUpload_InvalidBytes_RejectsNonImage(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "carol", "carol@example.com", "pass", "student")
	h := &ProfileHandler{DB: db}

	// Отправляем случайные байты с Content-Type image/jpeg — должны быть отклонены
	req := buildMultipartWithContentType(t, "evil.jpg", "image/jpeg", fakeImageBytes())
	req = withUserContext(req, int(userID), "carol", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.UploadAvatar).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("non-image bytes with image/jpeg content-type should be rejected")
	}
}

func TestAvatarUpload_TextFile_Rejected(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "dave", "dave@example.com", "pass", "student")
	h := &ProfileHandler{DB: db}

	textData := []byte("Hello, this is a plain text file, not an image.")
	req := buildMultipartWithContentType(t, "file.txt", "text/plain", textData)
	req = withUserContext(req, int(userID), "dave", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.UploadAvatar).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("text file should be rejected as avatar")
	}
}

func TestAvatarUpload_EmptyFile_Rejected(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "eve", "eve@example.com", "pass", "student")
	h := &ProfileHandler{DB: db}

	req := buildMultipartRequest(t, "avatar", "empty.jpg", "image/jpeg", []byte{})
	req = withUserContext(req, int(userID), "eve", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.UploadAvatar).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("empty file should be rejected")
	}
}

func TestAvatarUpload_NoAuth_Rejected(t *testing.T) {
	db := setupTestDB(t)
	h := &ProfileHandler{DB: db}

	req := buildMultipartRequest(t, "avatar", "photo.jpg", "image/jpeg", makeJPEG(t))
	// Без userContext — неавторизованный запрос
	w := httptest.NewRecorder()
	http.HandlerFunc(h.UploadAvatar).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("unauthenticated avatar upload should be rejected")
	}
}

// ── GetProfile Tests ──────────────────────────────────────────────────────────

func TestGetProfile_ReturnsUserData(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "alice2", "alice2@example.com", "pass", "student")
	h := &ProfileHandler{DB: db}

	req := httptest.NewRequest("GET", "/api/profile", nil)
	req = withUserContext(req, int(userID), "alice2", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.GetProfile).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetProfile → %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["login"] == nil {
		t.Error("response should contain login field")
	}
}

func TestGetProfile_DoesNotLeakPasswordHash(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "alice3", "alice3@example.com", "secretpassword", "student")
	h := &ProfileHandler{DB: db}

	req := httptest.NewRequest("GET", "/api/profile", nil)
	req = withUserContext(req, int(userID), "alice3", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.GetProfile).ServeHTTP(w, req)

	body := w.Body.String()
	// Bcrypt-хеш начинается с $2a$ или $2b$
	if strings.Contains(body, "$2") || strings.Contains(body, "password_hash") {
		t.Error("profile response must not contain password hash")
	}
}

func TestGetProfile_NoAuth_Rejected(t *testing.T) {
	db := setupTestDB(t)
	h := &ProfileHandler{DB: db}

	req := httptest.NewRequest("GET", "/api/profile", nil)
	// Без контекста пользователя
	w := httptest.NewRecorder()
	http.HandlerFunc(h.GetProfile).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("unauthenticated GetProfile should be rejected")
	}
}

func TestGetProfile_WrongUserID_NoData(t *testing.T) {
	db := setupTestDB(t)
	h := &ProfileHandler{DB: db}

	// userID 99999 не существует в БД
	req := httptest.NewRequest("GET", "/api/profile", nil)
	req = withUserContext(req, 99999, "ghost", "student")
	w := httptest.NewRecorder()
	http.HandlerFunc(h.GetProfile).ServeHTTP(w, req)

	// Должен вернуть ошибку, а не 200 с пустыми данными
	if w.Code == http.StatusOK {
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["login"] != nil && resp["login"] != "" {
			t.Error("profile for non-existent user should not return login data")
		}
	}
}
