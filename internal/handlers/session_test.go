package handlers

import (
	"strings"
	"testing"
)

// ─── generateJoinCode ───────────────────────────────────────────────────────
// Генерирует 6-символьный код для входа студентов в сессию.
// Убраны похожие символы (0/O, 1/I/L) чтобы диктовать код голосом.

func TestGenerateJoinCode_Length(t *testing.T) {
	code := generateJoinCode()
	if len(code) != 6 {
		t.Errorf("generateJoinCode() length = %d, want 6", len(code))
	}
}

func TestGenerateJoinCode_Charset(t *testing.T) {
	// Допустимые символы — те же, что в самой функции
	const allowed = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

	// Генерируем 100 кодов и проверяем каждый символ
	for i := 0; i < 100; i++ {
		code := generateJoinCode()
		for _, ch := range code {
			if !strings.ContainsRune(allowed, ch) {
				t.Errorf("generateJoinCode() содержит недопустимый символ %q в коде %q", string(ch), code)
			}
		}
	}
}

func TestGenerateJoinCode_NoConfusingChars(t *testing.T) {
	// Символы, которые НЕ должны появляться (легко спутать при диктовке)
	forbidden := "0OoIiLl1"

	for i := 0; i < 100; i++ {
		code := generateJoinCode()
		for _, ch := range code {
			if strings.ContainsRune(forbidden, ch) {
				t.Errorf("generateJoinCode() содержит confusing-символ %q в коде %q", string(ch), code)
			}
		}
	}
}

func TestGenerateJoinCode_Uniqueness(t *testing.T) {
	// 1000 кодов — при пространстве 31^6 ≈ 887M вариантов коллизии быть не должно
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		code := generateJoinCode()
		if seen[code] {
			t.Errorf("generateJoinCode() дубликат: %q", code)
		}
		seen[code] = true
	}
}
