# VisualMath

Веб-платформа для преподавателей и студентов: интерактивные лекции, тесты с LaTeX/MathJax, система сдачи и проверки работ с PDF-аннотациями.

## Возможности

- **Модули** — текстовые материалы и тесты с поддержкой LaTeX (`$формула$`, `$$блок$$`)
- **Лекции** — сборки модулей с настройкой порядка, публикацией и комментариями
- **Classroom** — предметы, задания, сдача работ в PDF, оценки
- **PDF-аннотации** — преподаватель рисует поверх работы студента, ставит пины и комментируемые подчёркивания/хайлайты
- **Живые сессии** — преподаватель запускает лекцию, студенты подключаются по коду через WebSocket
- **Профили** — аватар, информация, смена пароля

## Стек

| Слой | Технология |
|---|---|
| Бэкенд | Go 1.24, [Chi](https://github.com/go-chi/chi) |
| БД | SQLite (WAL-режим) |
| Аутентификация | JWT (golang-jwt/jwt v4) |
| WebSocket | gorilla/websocket |
| PDF | pdfcpu |
| Фронтенд | HTML + CSS + Vanilla JS, MathJax, PDF.js |

## Быстрый старт

```bash
cd visualmath
go mod download
go run ./cmd/server/
```

Сервер запустится на `http://localhost:8080`.

## Переменные окружения

Создай файл `.env` в корне проекта:

```env
JWT_SECRET=your-secret-key
PORT=8080
COOKIE_DOMAIN=

# SMTP для подтверждения email при регистрации
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USER=noreply@example.com
SMTP_PASS=password
```

Без `SMTP_*` регистрация работает, но письма с кодом подтверждения не отправляются (код выводится в лог).

## Структура проекта

```
cmd/server/         — точка входа, маршруты
internal/
  handlers/         — HTTP-хендлеры
  storage/          — SQLite-репозитории
  auth/             — JWT middleware
  sync/             — WebSocket менеджер
web/
  templates/        — HTML-шаблоны
  static/           — CSS, JS, шрифты
```

## Сборка для production

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o visualmath_linux ./cmd/server/

# Windows
go build -o visualmath.exe ./cmd/server/
```

Бинарник нужно положить рядом с папкой `web/` — сервер ищет шаблоны относительно своего расположения.
