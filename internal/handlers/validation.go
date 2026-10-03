package handlers

import (
	"errors"
	"regexp"

	"github.com/go-playground/validator/v10"
)

var loginRegex = regexp.MustCompile(`^[a-zA-Z0-9а-яА-ЯёЁ._\-]+$`)
var validate = validator.New()

func init() {
	validate.RegisterValidation("login_chars", func(fl validator.FieldLevel) bool {
		return loginRegex.MatchString(fl.Field().String())
	})
}

func validationError(err error) string {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return "Ошибка валидации"
	}
	e := ve[0]
	switch e.Field() + "." + e.Tag() {
	case "Login.required":
		return "Логин обязателен"
	case "Login.min":
		return "Логин должен быть не короче 3 символов"
	case "Login.max":
		return "Логин должен быть не длиннее 30 символов"
	case "Login.login_chars":
		return "Логин может содержать только буквы, цифры, точку, дефис и подчёркивание"
	case "Email.required":
		return "Email обязателен"
	case "Email.email":
		return "Некорректный формат email"
	case "FullName.min":
		return "Имя слишком короткое"
	case "FullName.max":
		return "Имя слишком длинное"
	}
	return "Некорректные данные: " + e.Field()
}
