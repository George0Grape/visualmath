# Security-аудит VisualMath

**Дата:** 2026-06-27  
**Объём:** код Go (`internal/`, `cmd/`), шаблоны/JS (`web/`), зависимости.  
**Метод:** ручной обзор + `govulncheck` + grep-проверки (SQLi, IDOR, path-traversal, заголовки).

Шкала: 🔴 высокий · 🟠 средний · 🟡 низкий · 🟢 в порядке / закрыто.

---

## Сводка

| # | Область | Статус | Действие |
|---|---------|--------|----------|
| 1 | Уязвимости зависимостей (CVE) | 🔴→🟢 | Пересборка новым тулчейном Go (этот цикл) |
| 2 | SQL-инъекции | 🟢 | — (параметризовано) |
| 3 | IDOR / доступ к чужим файлам | 🟢 | — (проверки владельца на месте) |
| 4 | Path-traversal в загрузках/скачивании | 🟢 | — (`filepath.Base`, пути из БД) |
| 5 | HTTP security-заголовки | 🟠→🟢 | Добавлены (этот цикл); CSP отложен |
| 6 | Rate-limiting | 🟠→🟢 | Расширен на register/join/comments (этот цикл) |
| 7 | XSS + токен в localStorage | 🟠 | Рекомендация (см. ниже) |
| 8 | CSRF | 🟡 | Приемлемо (SameSite=Lax); рекомендация |
| 9 | Загрузки: валидация типа | 🟡 | Рекомендация (magic-bytes) |
| 10 | HSTS | 🟡→🟢 | Добавлен в nginx (этот цикл) |
| 11 | Пароли (bcrypt) | 🟢 | — (DefaultCost) |

---

## Детали

### 1. 🔴→🟢 Уязвимости зависимостей (CVE)
`govulncheck` нашёл **20 уязвимостей, достижимых из кода**, все — в стандартной библиотеке тулчейна **go1.25.5**, которым собирался бинарник. Ключевая:

- **GO-2026-4341 — memory exhaustion в `net/url`** (DoS). Достижима из `handlers.PublishedLecturesAPI` (`url.URL.Query`) и `handlers.SubmitWork` (`ParseMultipartForm` → `url.ParseQuery`). Специально сформированная query-строка/тело может исчерпать память. **nginx не защищает** — query-строка проксируется в Go как есть.
- Несколько CVE в `crypto/tls` (handshake, session resumption) — экспозиция ниже: Go-сервер за nginx слушает HTTP на localhost, TLS используется только для исходящих (OAuth, SMTP).

Дополнительно `-mode=binary` выявил уязвимости в `golang.org/x/image/webp` (`webp.Decode` — достижим при декодировании загруженных изображений): **GO-2026-4961** и **GO-2026-5061** (panic на кривом VP8 alpha).

**Исправление:** пересборка тулчейном **go1.26.4** (через `GOTOOLCHAIN=go1.26.4` — Go сам скачивает тулчейн; сборка на сервере через git невозможна, см. примечание) + апгрейд `golang.org/x/image` → **v0.43.0**. После этого `govulncheck -mode=binary` показывает **0 достижимых уязвимостей**. **Сделано в этом цикле.**

> **Примечание по деплою:** серверный git-чекаут заброшен (древний коммит, локально изменён `.env` с продовыми секретами) — `git pull`/`reset` на сервере недопустим. Бинарник собирается локально нужным тулчейном и заливается scp.

**Рекомендация на будущее:** добавить `govulncheck ./...` в регулярную проверку (CI или ручной чек перед деплоем).

### 2. 🟢 SQL-инъекции
Запросы используют плейсхолдеры `?`; конкатенации пользовательского ввода в SQL не найдено. `LIKE`-поиск передаёт `"%"+q+"%"` как **аргумент**, а не как часть строки запроса — это безопасно.

### 3. 🟢 IDOR / доступ к чужим данным
Эндпоинты скачивания (`DownloadSubmission`, `DownloadAnnotated`, `DownloadVariant`, условия/варианты) проверяют принадлежность: `sub.StudentID == userID` **или** `IsTeacherOfSubject(...)`. Оценивание/отзыв работ покрыты тестами (`classroom_test.go`).

### 4. 🟢 Path-traversal
Имена загружаемых файлов проходят через `filepath.Base(header.Filename)` (срезает `../`), а пути для отдачи строятся из значений **из БД**, не из URL-параметров. Прямой обход директорий не виден.

### 5. 🟠→🟢 HTTP security-заголовки
Раньше не выставлялся ни один. Добавлен middleware `SecurityHeaders`:
- `X-Content-Type-Options: nosniff` — против MIME-sniffing;
- `X-Frame-Options: SAMEORIGIN` — против кликджекинга;
- `Referrer-Policy: strict-origin-when-cross-origin`;
- `Permissions-Policy: geolocation=(), microphone=(), camera=()`.

**CSP — добавлен.** После самохостинга всех зависимостей (включая PDF.js, см. ниже) внешних источников не осталось, поэтому политика ограничена `'self'` с послаблениями: `script-src 'unsafe-inline' 'unsafe-eval'` (inline-обработчики + eval в MathJax/Plotly), `img-src data: blob:`, `worker-src 'self' blob:` (воркер PDF.js), `connect-src ws: wss:` (WebSocket). Плюс `frame-ancestors/base-uri/form-action 'self'`, `object-src 'none'`. Проверено headless: MathJax рендерится (значит `unsafe-eval` honored), страницы грузятся. Дальнейшее ужесточение (убрать `unsafe-inline` через nonce) — отдельная большая задача.

### 6. 🟠→🟢 Rate-limiting
Был только на `/login` (5 попыток / 15 мин / блок 15 мин). Механизм обобщён (`NewRateLimiter`) и добавлен middleware `RateLimit` на:
- `POST /api/register` — 10 / час (спам-регистрации);
- `POST /api/sessions/join` и `POST /classroom/join` — 20 / 10 мин (**перебор join-кодов**);
- постинг комментариев (lecture + classroom) — 30 / 5 мин.

Лимит по IP (через `X-Real-IP`/`X-Forwarded-For`). При превышении — `429` + `Retry-After`.

### 7. 🟠 XSS и хранение токена
JWT хранится и в **HttpOnly+Secure cookie** (серверная аутентификация — хорошо), и в **localStorage** (для WS/fetch на фронте). Копия в localStorage достижима из JS, поэтому **любой XSS = кража токена/захват сессии**.
- `showToast` экранирует `<` — ок.
- **Рекомендация:** провести ревизию всех `innerHTML` с пользовательскими данными (комментарии, имена, тексты модулей); по возможности уйти от localStorage-токена к cookie-only + передавать токен в WS через сабпротокол/cookie.

### 8. 🟡 CSRF
Cookies — `SameSite=Lax`, что блокирует межсайтовые POST/DELETE (основной вектор CSRF). Для большинства операций этого достаточно. **Рекомендация:** для особо чувствительных действий (удаление предмета/работ, смена оценки) рассмотреть anti-CSRF токен или `SameSite=Strict`.

### 9. 🟡→🟢 Валидация загрузок
Раньше проверялось только расширение `.pdf`. Добавлена проверка **сигнатуры `%PDF-`** в первых байтах (`looksLikePDF`) во всех точках загрузки (сдачи, условия, варианты) — отсекает подмену типа. Лимит 50 МБ, путь безопасен.

### 9a. 🟠→🟢 Внешние зависимости (supply chain)
**PDF.js** грузился с `cdnjs.cloudflare.com` (последняя внешняя зависимость — supply-chain-риск + помеха CSP). Завендорен локально в `web/static/vendor/pdfjs/` (`pdf.min.js` + `pdf.worker.min.js`, v3.11.174). Теперь внешних ресурсов нет вовсе — это и позволило включить строгий CSP.

### 10. 🟡→🟢 HSTS
`Strict-Transport-Security` выставляется на **nginx** (терминирует TLS), а не в Go за прокси. **Сделано:** `add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;` в блоке `visualmath.ru:443`. Проверено на проде — заголовок отдаётся. Конфиг забэкаплен в `visualmath.bak.hsts`.

### 11. 🟢 Пароли
`bcrypt.GenerateFromPassword(..., bcrypt.DefaultCost)` (cost 10) — приемлемо. JWT-секрет: при `COOKIE_SECURE=true` пустой/дефолтный секрет приводит к отказу запуска — хорошая защита от слабого секрета в проде.

---

## Что сделано в этом цикле
- ✅ Пересборка новым тулчейном Go → закрыты 20 CVE stdlib (вкл. `net/url` DoS) + `x/image/webp` → v0.43.0.
- ✅ `SecurityHeaders` middleware (nosniff, X-Frame-Options, Referrer/Permissions-Policy).
- ✅ CSP (строгий `'self'` + минимальные послабления, аварийный тумблер `CSP_DISABLE`).
- ✅ Расширенный rate-limiting (register, join-коды, комментарии).
- ✅ Валидация `%PDF-` сигнатуры на всех загрузках.
- ✅ Самохостинг PDF.js (убрана последняя внешняя зависимость).
- ✅ HSTS на nginx.

## Бэклог (приоритет ↓)
1. 🟠 Уйти от токена в localStorage / ревизия `innerHTML` (XSS-устойчивость).
2. 🟡 Ужесточить CSP: убрать `'unsafe-inline'` через nonce (большая задача — много inline-скриптов).
3. 🟢 `govulncheck` в регулярную проверку (ручной чек перед деплоем — CI пользователь не хочет).
