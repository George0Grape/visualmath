package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

    "visualmath/internal/auth"
    
	"github.com/go-chi/chi/v5"
)

// ModuleHandler теперь хранит подключение к базе
type ModuleHandler struct {
	DB *sql.DB
}

// Конструктор для создания хендлера с БД
func NewModuleHandler(db *sql.DB) *ModuleHandler {
	return &ModuleHandler{
		DB: db,
	}
}

// --- API МЕТОДЫ (С ЛОГИКОЙ БД) ---

// CreateModule - Создание модуля в БД
func (h *ModuleHandler) CreateModule(w http.ResponseWriter, r *http.Request) {
	// Структура, соответствующая JSON из вашего dashboard.js
	var request struct {
		Title       string      `json:"title"`
		CourseID    int         `json:"course_id"`
		Description string      `json:"description"`
		Type        string      `json:"type"`
		Content     interface{} `json:"content"` // Raw JSON (вопросы, тест или текст)
	}

	// 1. Декодируем запрос
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Превращаем Content обратно в JSON-строку для базы данных
	contentBytes, err := json.Marshal(request.Content)
	if err != nil {
		http.Error(w, "Content encode error", http.StatusInternalServerError)
		return
	}

	// 3. Сохраняем в PostgreSQL
	
	authorID := 1
	var newID int

	query := `
		INSERT INTO modules (title, course_id, description, type, content, author_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		RETURNING id`

	err = h.DB.QueryRow(query,
		request.Title,
		request.CourseID,
		request.Description,
		request.Type,
		contentBytes,
		authorID,
	).Scan(&newID)

	if err != nil {
		http.Error(w, "Database save error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Отправляем ответ
	response := map[string]interface{}{
		"success":   true,
		"message":   "Module created successfully",
		"module_id": newID,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetModule - Получение одного модуля из БД
func (h *ModuleHandler) GetModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")
	id, err := strconv.Atoi(moduleID)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Структура для ответа
	var module struct {
		ID          int             `json:"id"`
		Title       string          `json:"title"`
		CourseID    int             `json:"course_id"`
		Description string          `json:"description"`
		Type        string          `json:"type"`
		Content     json.RawMessage `json:"content"` // Чтобы Go не экранировал JSON дважды
		AuthorID    int             `json:"author_id"`
		CreatedAt   string          `json:"created_at"`
	}

	// Читаем JSONB поле как []byte
	var contentBytes []byte

	query := `SELECT id, title, course_id, description, type, content, author_id, created_at 
			  FROM modules WHERE id = $1`

	err = h.DB.QueryRow(query, id).Scan(
		&module.ID,
		&module.Title,
		&module.CourseID,
		&module.Description,
		&module.Type,
		&contentBytes,
		&module.AuthorID,
		&module.CreatedAt,
	)

	if err == sql.ErrNoRows {
		http.Error(w, "Module not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	module.Content = json.RawMessage(contentBytes)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(module)
}

// ListModulesAPI - Список модулей из БД
func (h *ModuleHandler) ListModulesAPI(w http.ResponseWriter, r *http.Request) {
	// Делаем JOIN с таблицей courses, чтобы получить название предмета, и users для имени автора
	query := `
		SELECT m.id, m.title, c.name, m.description, m.type, u.full_name, m.created_at
		FROM modules m
		LEFT JOIN courses c ON m.course_id = c.id
		LEFT JOIN users u ON m.author_id = u.id
		ORDER BY m.created_at DESC
	`

	rows, err := h.DB.Query(query)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var modules []map[string]interface{}

	for rows.Next() {
		var id int
		var title, courseName, description, mType, authorName, createdAt string

		// Сканируем, учитывая, что description может быть NULL (если база позволяет)
		if err := rows.Scan(&id, &title, &courseName, &description, &mType, &authorName, &createdAt); err != nil {
			continue
		}

		modules = append(modules, map[string]interface{}{
			"id":          id,
			"title":       title,
			"course":      courseName,
			"description": description,
			"type":        mType,
			"author":      authorName,
			"created_at":  createdAt,
		})
	}

	// Если модулей нет, возвращаем пустой массив []
	if modules == nil {
		modules = []map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(modules)
}

// UpdateModule - Обновление существующего модуля в БД
func (h *ModuleHandler) UpdateModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")
	id, err := strconv.Atoi(moduleID)
	if err != nil {
		http.Error(w, "Неверный ID", http.StatusBadRequest)
		return
	}

	var request struct {
		Title       string      `json:"title"`
		CourseID    int         `json:"course_id"`
		Description string      `json:"description"`
		Type        string      `json:"type"`
		Content     interface{} `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	contentBytes, err := json.Marshal(request.Content)
	if err != nil {
		http.Error(w, "Content encode error", http.StatusInternalServerError)
		return
	}

	query := `
		UPDATE modules 
		SET title = $1, course_id = $2, description = $3, type = $4, content = $5
		WHERE id = $6`
	
	_, err = h.DB.Exec(query, request.Title, request.CourseID, request.Description, request.Type, contentBytes, id)
	if err != nil {
		http.Error(w, "Database update error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Модуль успешно обновлен",
	})
}

// DeleteModule - Удаление модуля
func (h *ModuleHandler) DeleteModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(moduleID)

	_, err := h.DB.Exec("DELETE FROM modules WHERE id = $1", id)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success":   true,
		"message":   "Module deleted successfully",
		"module_id": id,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --- HTML СТРАНИЦЫ (ОСТАЛИСЬ БЕЗ ИЗМЕНЕНИЙ, КРОМЕ ПОДКЛЮЧЕНИЯ БД) ---

// ListModules показывает список всех модулей
func (h *ModuleHandler) ListModules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Библиотека модулей - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="container modules-container">
        <div class="modules-header">
            <h1>Библиотека модулей</h1>
            <a href="/modules/create" class="create-btn">Создать новый модуль</a>
        </div>
        
        <div class="search-filter">
            <input type="text" class="search-box" placeholder="Поиск модулей..." id="searchInput">
            <select class="filter-select" id="courseFilter">
                <option value="">Все предметы</option>
                <option value="Математический анализ">Математический анализ</option>
                <option value="Линейная алгебра">Линейная алгебра</option>
                <option value="Дискретная математика">Дискретная математика</option>
                <option value="Экономика">Экономика</option>
            </select>
            <select class="filter-select" id="typeFilter">
                <option value="">Все типы</option>
                <option value="text">Текстовый</option>
                <option value="visual">Визуальный</option>
                <option value="question">Вопросник</option>
                <option value="test">Проверочный</option>
            </select>
            <button class="create-btn" onclick="filterModules()">Применить</button>
        </div>
        
        <div class="modules-grid" id="modulesGrid">
            <!-- Модули будут загружены через JavaScript -->
            <div class="empty-state">
                <h3>Загрузка...</h3>
            </div>
        </div>
    </div>
    
    <script>
        async function loadModules() {
            try {
                const response = await fetch('/api/modules/list');
                const modules = await response.json();
                displayModules(modules);
            } catch (error) {
                console.error('Error loading modules:', error);
            }
        }
        
        function displayModules(modules) {
            const grid = document.getElementById('modulesGrid');
            
            if (!modules || modules.length === 0) {
                grid.innerHTML = '<div class="empty-state"><h3>Нет модулей</h3><p>Создайте свой первый модуль</p><a href="/modules/create" class="create-btn" style="margin-top: 15px;">Создать модуль</a></div>';
                return;
            }
            
            let html = '';
            
            modules.forEach(module => {
                const typeLabels = {
                    'text': { name: 'Текстовый', class: 'type-text' },
                    'visual': { name: 'Визуальный', class: 'type-visual' },
                    'question': { name: 'Вопросник', class: 'type-question' },
                    'test': { name: 'Проверочный', class: 'type-test' }
                };
                
                const typeInfo = typeLabels[module.type] || { name: module.type, class: '' };
                
                html += '<div class="module-card">' +
                        '<div class="module-header">' +
                        '<h3 class="module-title">' + module.title + '</h3>' +
                        '<span class="module-type ' + typeInfo.class + '">' + typeInfo.name + '</span>' +
                        '</div>' +
                        '<div class="module-body">' +
                        '<div class="module-meta">' +
                        '<span>' + (module.course || 'Без предмета') + '</span>' +
                        '<span>' + (module.author || 'Неизвестно') + '</span>' +
                        '</div>' +
                        '<p>' + (module.description || 'Описание отсутствует') + '</p>' +
                        '</div>' +
                        '<div class="module-actions">' +
                        '<a href="/modules/view/' + module.id + '" class="action-btn primary">Открыть</a>' +
                        '<a href="/modules/edit/' + module.id + '" class="action-btn">Редактировать</a>' +
                        '</div>' +
                        '</div>';
            });
            
            grid.innerHTML = html;
        }
        
        window.addEventListener('DOMContentLoaded', loadModules);
    </script>
</body>
</html>`

	fmt.Fprintf(w, html)
}

// CreateModulePage показывает страницу создания модуля
func (h *ModuleHandler) CreateModulePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Создать модуль - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
    <style>
        /* Стили для конструктора вопросов */
        .question-block {
            background: #f8f9fa;
            border: 1px solid #e9ecef;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
            position: relative;
        }
        .remove-q-btn {
            position: absolute;
            top: 10px;
            right: 10px;
            background: #e74c3c;
            color: white;
            border: none;
            border-radius: 4px;
            padding: 5px 10px;
            cursor: pointer;
            font-size: 12px;
        }
        .answer-row {
            display: flex;
            align-items: center;
            margin-bottom: 10px;
            gap: 10px;
        }
        .answer-row input[type="radio"] {
            width: auto;
            margin: 0;
            cursor: pointer;
            transform: scale(1.5); /* Увеличим кружок */
        }
        .answer-row input[type="text"] {
            margin: 0;
            flex-grow: 1;
        }
        .add-q-btn {
            background: #2ecc71;
            color: white;
            border: none;
            padding: 10px 20px;
            border-radius: 5px;
            cursor: pointer;
            font-weight: bold;
            display: block;
            margin: 20px 0;
        }
        .add-q-btn:hover { background: #27ae60; }
        .correct-label { font-size: 12px; color: #7f8c8d; }
    </style>
</head>
<body>
    <div class="container create-container">
        <div class="create-header">
            <h1>Создание модуля</h1>
            <p>Заполните форму для создания учебного модуля</p>
        </div>
        
        <form id="createModuleForm" class="create-form">
            <!-- Основная информация -->
            <div class="form-group">
                <label for="moduleTitle">Название модуля *</label>
                <input type="text" id="moduleTitle" name="title" required 
                       placeholder="Введите название модуля">
            </div>
            
            <div class="form-group">
                <label for="moduleCourse">Предмет *</label>
                <select id="moduleCourse" name="course" required>
                    <option value="">Выберите предмет</option>
                    <option value="1">Математический анализ</option>
                    <option value="2">Линейная алгебра</option>
                    <option value="3">Дискретная математика</option>
                    <option value="4">Экономика</option>
                </select>
            </div>
            
            <div class="form-group">
                <label for="moduleDescription">Краткое описание</label>
                <textarea id="moduleDescription" name="description" 
                          placeholder="Опишите содержание модуля (необязательно)"></textarea>
            </div>
            
            <!-- Выбор типа модуля -->
            <div class="form-group">
                <label>Тип модуля *</label>
                <div class="module-type-selector">
                    <div class="type-option" data-type="text" onclick="selectModuleType('text')">
                        <h4>Текстовый модуль</h4>
                        <p>Лекция, формулы LaTeX</p>
                    </div>
                    <div class="type-option" data-type="question" onclick="selectModuleType('question')">
                        <h4>Вопросник (Тест)</h4>
                        <p>Тест с выбором правильного ответа</p>
                    </div>
                </div>
                <input type="hidden" id="moduleType" name="type" required>
            </div>
            
            <!-- Контент в зависимости от типа -->
            <div id="contentArea">
                <!-- Динамически меняется -->
            </div>
            
            <!-- Кнопки -->
            <div class="form-actions">
                <button type="submit" class="submit-btn">Создать модуль</button>
                <a href="/modules" class="cancel-btn">Отмена</a>
            </div>
        </form>
        
        <!-- Сообщения -->
        <div id="message" class="message"></div>
    </div>
    
    <script>
        let selectedType = '';
        
        function selectModuleType(type) {
            selectedType = type;
            document.getElementById('moduleType').value = type;
            document.querySelectorAll('.type-option').forEach(opt => {
                opt.classList.remove('selected');
            });
            document.querySelector('[data-type="' + type + '"]').classList.add('selected');
            updateContentArea(type);
        }
        
        function updateContentArea(type) {
            const contentArea = document.getElementById('contentArea');
            
            if (type === 'text') {
                contentArea.innerHTML = 
                    '<div class="form-group">' +
                    '<label for="contentText">Текст модуля *</label>' +
                    '<textarea id="contentText" name="content" rows="15" placeholder="Введите текст лекции..." required></textarea>' +
                    '<div class="latex-hint">Подсказка: используйте $...$ для формул</div>' +
                    '</div>';
            } else if (type === 'question') {
                contentArea.innerHTML = 
                    '<div id="questionsContainer"></div>' +
                    '<button type="button" class="add-q-btn" onclick="addQuestion()">+ Добавить вопрос</button>';
                
                // Сразу добавляем один пустой вопрос
                addQuestion();
            }
        }
        
        // Функция добавления визуального блока вопроса
        function addQuestion() {
            const container = document.getElementById('questionsContainer');
            const qIndex = container.children.length; // Индекс для группировки radio button
            
            const div = document.createElement('div');
            div.className = 'question-block';
            div.innerHTML = 
                '<button type="button" class="remove-q-btn" onclick="this.parentElement.remove()">Удалить</button>' +
                '<div class="form-group">' +
                    '<label>Текст вопроса №' + (qIndex + 1) + '</label>' +
                    '<input type="text" class="q-text" placeholder="Введите вопрос" required>' +
                '</div>' +
                '<label>Варианты ответов (отметьте правильный):</label>' +
                '<div class="answer-row">' +
                    '<input type="radio" name="correct_' + qIndex + '" value="0" checked title="Верный ответ">' +
                    '<input type="text" class="a-text" placeholder="Ответ 1" required>' +
                '</div>' +
                '<div class="answer-row">' +
                    '<input type="radio" name="correct_' + qIndex + '" value="1" title="Верный ответ">' +
                    '<input type="text" class="a-text" placeholder="Ответ 2" required>' +
                '</div>' +
                '<div class="answer-row">' +
                    '<input type="radio" name="correct_' + qIndex + '" value="2" title="Верный ответ">' +
                    '<input type="text" class="a-text" placeholder="Ответ 3">' +
                '</div>' +
                '<div class="answer-row">' +
                    '<input type="radio" name="correct_' + qIndex + '" value="3" title="Верный ответ">' +
                    '<input type="text" class="a-text" placeholder="Ответ 4">' +
                '</div>';
            
            container.appendChild(div);
        }

        // Сбор данных формы
        document.getElementById('createModuleForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            const messageDiv = document.getElementById('message');
            messageDiv.style.display = 'none';
            
            let contentData;

            // Логика сбора данных для ТЕСТА
            if (selectedType === 'question') {
                const qBlocks = document.querySelectorAll('.question-block');
                const questions = [];
                
                qBlocks.forEach((block, idx) => {
                    const qText = block.querySelector('.q-text').value;
                    const aInputs = block.querySelectorAll('.a-text');
                    const answers = [];
                    
                    // Собираем только непустые ответы
                    aInputs.forEach(inp => {
                        if(inp.value.trim() !== "") answers.push(inp.value);
                    });
                    
                    // Находим выбранный radio button
                    // Поскольку при удалении блоков индексы сбиваются, ищем radio внутри блока
                    const radios = block.querySelectorAll('input[type="radio"]');
                    let correctIndex = 0;
                    radios.forEach((r, rIdx) => {
                        if (r.checked) correctIndex = rIdx;
                    });

                    if (qText && answers.length >= 2) {
                        questions.push({
                            question: qText,
                            answers: answers,
                            correct: correctIndex
                        });
                    }
                });

                if (questions.length === 0) {
                    alert("Добавьте хотя бы один вопрос с минимум двумя ответами!");
                    return;
                }
                contentData = questions;
            } else {
                // Логика для ТЕКСТА
                contentData = { 
                    text: document.getElementById('contentText').value 
                };
            }
            
            const formData = {
                title: document.getElementById('moduleTitle').value,
                course_id: parseInt(document.getElementById('moduleCourse').value),
                description: document.getElementById('moduleDescription').value,
                type: selectedType,
                content: contentData
            };
            
            try {
                const response = await fetch('/api/modules', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify(formData)
                });
                const result = await response.json();
                
                if (response.ok) {
                    showMessage('Модуль успешно создан! Перенаправление...', 'success');
                    setTimeout(() => { window.location.href = '/modules'; }, 2000);
                } else {
                    showMessage('Ошибка: ' + (result.message || 'Не удалось создать модуль'), 'error');
                }
            } catch (error) {
                showMessage('Ошибка сети: ' + error.message, 'error');
            }
        });
        
        function showMessage(text, type) {
            const messageDiv = document.getElementById('message');
            messageDiv.textContent = text;
            messageDiv.className = 'message ' + type;
            messageDiv.style.display = 'block';
        }
        
        // Инициализация
        window.addEventListener('DOMContentLoaded', function() {
            selectModuleType('text');
        });
        
    </script>
</body>
</html>`

	fmt.Fprintf(w, html)
}


// ViewModulePage показывает страницу просмотра модуля
func (h *ModuleHandler) ViewModulePage(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Просмотр модуля - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
    <style>
        body { background-color: #f4f7f6; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; }
        .view-container { max-width: 800px; margin: 40px auto; padding: 0 20px; }
        .view-header { text-align: center; margin-bottom: 40px; }
        .view-header h1 { color: #2c3e50; font-size: 2.5em; margin-bottom: 10px; }
        
        /* Карточка вопроса */
        .question-card { 
            background: #fff; 
            border-radius: 12px; 
            padding: 30px; 
            margin-bottom: 25px; 
            box-shadow: 0 4px 15px rgba(0,0,0,0.05); 
        }
        .question-text { font-size: 1.25em; font-weight: 600; margin-bottom: 20px; color: #34495e; }
        
        /* Ответы */
        .answer-option { 
            display: flex; 
            align-items: center; 
            padding: 15px; 
            margin: 10px 0; 
            border: 2px solid #f0f2f5; 
            border-radius: 8px; 
            cursor: pointer; 
            transition: background 0.2s;
        }
        .answer-option:hover { background-color: #f8f9fa; border-color: #3498db; }
        .answer-option input { margin-right: 15px; transform: scale(1.5); cursor: pointer; }
        
        /* Подсветка результатов */
        .result-correct { background-color: #d4edda !important; border-color: #28a745 !important; }
        .result-wrong { background-color: #f8d7da !important; border-color: #dc3545 !important; }

        /* Стиль кнопок */
        .btn-style {
            display: block;
            width: 100%;
            padding: 15px;
            border: none;
            border-radius: 8px;
            font-size: 18px;
            font-weight: bold;
            cursor: pointer;
            text-align: center;
            transition: transform 0.1s, box-shadow 0.1s;
            color: white;
            box-sizing: border-box;
        }
        .btn-style:active { transform: scale(0.98); }
    </style>
</head>
<body>
    <div class="view-container">
        <div class="view-header">
            <h1 id="moduleTitle">Загрузка...</h1>
            <p id="moduleInfo" style="color: #7f8c8d;">...</p>
        </div>
        
        <!-- БЛОК РЕЗУЛЬТАТА (Изначально скрыт) -->
        <div id="scoreBox" style="display: none; background: white; padding: 40px; border-radius: 12px; text-align: center; margin-bottom: 30px; border-top: 6px solid #2ecc71; box-shadow: 0 10px 30px rgba(0,0,0,0.1);">
            <h2 id="scoreValue" style="margin: 0; font-size: 4em; color: #2ecc71;">0%</h2>
            <p id="scoreText" style="font-size: 1.3em; color: #555; margin-top: 10px;">Результат</p>
            
            <!-- КНОПКА СКАЧИВАНИЯ -->
            <button id="downloadBtn" class="btn-style" style="margin-top: 25px; background-color: #3498db; width: auto; padding: 12px 30px; display: inline-block;">
                📥 Скачать отчет
            </button>
        </div>

        <!-- Сюда вставляются вопросы -->
        <div id="moduleContent"></div>
        
        <!-- КНОПКА ЗАВЕРШИТЬ -->
        <button id="finishBtn" onclick="finishTest()" class="btn-style" style="display: none; background: linear-gradient(135deg, #2ecc71, #27ae60); margin-top: 30px; box-shadow: 0 4px 10px rgba(46, 204, 113, 0.3);">
            Завершить и проверить
        </button>
        
        <div style="text-align: center; margin-top: 30px; margin-bottom: 50px;">
            <a href="/modules" style="color: #95a5a6; text-decoration: none; font-size: 14px;">← Назад к списку</a>
            <span style="color: #ddd; margin: 0 10px;">|</span>
            <a href="/modules/edit/` + moduleID + `" style="color: #3498db; text-decoration: none; font-size: 14px;">Редактировать</a>
        </div>
    </div>
    
    <script>
    const moduleId = ` + moduleID + `;
    let currentModuleData = null;
    let reportTextForDownload = "";

    async function loadModuleData() {
        try {
            const response = await fetch('/api/modules/' + moduleId);
            if (!response.ok) throw new Error('Модуль не найден');
            currentModuleData = await response.json();
            
            // Заголовки
            document.getElementById('moduleTitle').innerText = currentModuleData.title;
            
            renderContent();
        } catch(e) {
            console.error(e);
            document.getElementById('moduleTitle').innerText = 'Ошибка загрузки';
        }
    }

    function renderContent() {
        const contentDiv = document.getElementById('moduleContent');
        contentDiv.innerHTML = '';

        if (currentModuleData.type === 'question') {
            // --- ЭТО ТЕСТ ---
            if (!currentModuleData.content || currentModuleData.content.length === 0) {
                contentDiv.innerHTML = '<p style="text-align:center">Нет вопросов</p>';
                return;
            }

            currentModuleData.content.forEach((q, idx) => {
                const card = document.createElement('div');
                card.className = 'question-card';
                
                let html = '<div class="question-text">' + (idx + 1) + '. ' + q.question + '</div>';
                
                q.answers.forEach((ans, aIdx) => {
                    html += '<label class="answer-option" id="opt-' + idx + '-' + aIdx + '">' +
                            '<input type="radio" name="question_' + idx + '" value="' + aIdx + '"> ' +
                            '<span style="margin-left: 10px;">' + ans + '</span>' +
                            '</label>';
                });
                card.innerHTML = html;
                contentDiv.appendChild(card);
            });

            // Показываем кнопку завершения (она скрыта по умолчанию)
            document.getElementById('finishBtn').style.display = 'block';
        
        } else if (currentModuleData.type === 'text') {
            // --- ЭТО ЛЕКЦИЯ ---
             contentDiv.innerHTML = '<div class="question-card" style="font-size: 1.2em; line-height: 1.6;">' + 
                (currentModuleData.content.text || '').replace(/\n/g, '<br>') + 
                '</div>';
        } else {
            contentDiv.innerText = "Неизвестный тип модуля: " + currentModuleData.type;
        }
    }

    async function finishTest() {
        const questions = currentModuleData.content;
        const studentAnswers = [];
        let correctCount = 0;
        
        reportTextForDownload = "Результаты теста: " + currentModuleData.title + "\n\n";

        questions.forEach((q, idx) => {
            const selected = document.querySelector('input[name="question_' + idx + '"]:checked');
            const userAnsIdx = selected ? parseInt(selected.value) : -1;
            studentAnswers.push(userAnsIdx);

            const isCorrect = (userAnsIdx === parseInt(q.correct));
            if (isCorrect) correctCount++;

            // Подсветка
            if (selected) {
                const label = document.getElementById('opt-' + idx + '-' + userAnsIdx);
                if (isCorrect) label.classList.add('result-correct');
                else label.classList.add('result-wrong');
            }
            // Показать правильный
            if (!isCorrect) {
                 const correctLabel = document.getElementById('opt-' + idx + '-' + q.correct);
                 if(correctLabel) correctLabel.style.border = "2px solid #2ecc71";
            }

            // Текст для файла
            reportTextForDownload += (idx+1) + '. ' + q.question + '\n';
            const userAnsText = userAnsIdx !== -1 ? q.answers[userAnsIdx] : "Нет ответа";
            reportTextForDownload += 'Ваш ответ: ' + userAnsText + '\n';
            reportTextForDownload += 'Результат: ' + (isCorrect ? "ВЕРНО" : "ОШИБКА") + '\n\n';
        });

        const score = Math.round((correctCount / questions.length) * 100);
        reportTextForDownload += 'ИТОГО: ' + correctCount + ' из ' + questions.length + ' (' + score + '%)';

        // 1. Показываем блок с результатом
        const scoreBox = document.getElementById('scoreBox');
        scoreBox.style.display = 'block';
        
        document.getElementById('scoreValue').innerText = score + '%';
        document.getElementById('scoreText').innerText = 'Правильно: ' + correctCount + ' из ' + questions.length;

        // 2. Настраиваем кнопку скачивания
        const downloadBtn = document.getElementById('downloadBtn');
        downloadBtn.onclick = function() {
            downloadResults(reportTextForDownload, "result.txt");
        };

        // 3. Скрываем кнопку завершения
        document.getElementById('finishBtn').style.display = 'none';
        
        // 4. Скролл наверх
        window.scrollTo({ top: 0, behavior: 'smooth' });

        // 5. Отправка на сервер
        const token = localStorage.getItem('token');
        if (token) {
            fetch('/api/modules/' + currentModuleData.id + '/submit', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': 'Bearer ' + token
                },
                body: JSON.stringify({ answers: studentAnswers })
            }).then(r => console.log("Saved")).catch(e => console.error(e));
        }
    }

    function downloadResults(text, filename) {
    // Если мы хотим именно формат для Excel (CSV)
    // Преобразуем наш текст в формат: Вопрос;Ответ;Результат
    let csvContent = "\uFEFF"; // BOM для корректного отображения кириллицы в Excel
    csvContent += "Вопрос;Ваш ответ;Результат\n";
    
    const questions = currentModuleData.content;
    questions.forEach((q, idx) => {
        const selected = document.querySelector('input[name="question_' + idx + '"]:checked');
        const userAnsIdx = selected ? parseInt(selected.value) : -1;
        const isCorrect = (userAnsIdx === parseInt(q.correct));
        const ansText = userAnsIdx !== -1 ? q.answers[userAnsIdx] : "Нет ответа";
        
        csvContent += q.question + ";" + ansText + ";" + (isCorrect ? "Верно" : "Ошибка") + "\n";
    });

    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement("a");
    const url = URL.createObjectURL(blob);
    link.setAttribute("href", url);
    link.setAttribute("download", "results.csv"); // Теперь файл .csv
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    }

    loadModuleData();
    </script>
</body>
</html>`

	
	fmt.Fprint(w, html)
}

// EditModulePage показывает страницу редактирования модуля
func (h *ModuleHandler) EditModulePage(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Мы используем тот же шаблон, что и при создании, 
	// но с логикой загрузки существующих данных через JS
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Редактирование модуля - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
    <style>
        /* Переиспользуем стили из страницы создания */
        .question-block { background: #f8f9fa; border: 1px solid #e9ecef; border-radius: 8px; padding: 20px; margin-bottom: 20px; position: relative; }
        .remove-q-btn { position: absolute; top: 10px; right: 10px; background: #e74c3c; color: white; border: none; border-radius: 4px; padding: 5px 10px; cursor: pointer; }
        .answer-row { display: flex; align-items: center; margin-bottom: 10px; gap: 10px; }
        .answer-row input[type="text"] { flex-grow: 1; }
        .selected { border-color: #3498db; background: #ebf5fb; }
    </style>
</head>
<body>
    <div class="container create-container">
        <div class="create-header">
            <h1>Редактирование модуля #` + moduleID + `</h1>
            <p>Внесите изменения в учебный модуль</p>
        </div>
        
        <form id="editModuleForm">
            <div class="form-group">
                <label>Название модуля *</label>
                <input type="text" id="moduleTitle" required>
            </div>
            
            <div class="form-group">
                <label>Предмет *</label>
                <select id="moduleCourse" required>
                    <option value="1">Математический анализ</option>
                    <option value="2">Линейная алгебра</option>
                    <option value="3">Дискретная математика</option>
                    <option value="4">Экономика</option>
                </select>
            </div>

            <div class="form-group">
                <label>Краткое описание</label>
                <textarea id="moduleDescription"></textarea>
            </div>

            <!-- Тип модуля (скрытый, так как тип менять нельзя для стабильности данных) -->
            <input type="hidden" id="moduleType">

            <div id="contentArea">
                <!-- Сюда подгрузится контент (текст или вопросы) -->
            </div>

            <div class="form-actions" style="margin-top: 20px;">
                <button type="submit" class="submit-btn">Сохранить изменения</button>
                <a href="/modules/view/` + moduleID + `" class="cancel-btn">Отмена</a>
            </div>
        </form>
        <div id="message" class="message"></div>
    </div>

    <script>
        const moduleId = ` + moduleID + `;

        // 1. Загрузка данных модуля при старте
        async function loadModule() {
            try {
                const response = await fetch('/api/modules/' + moduleId);
                const module = await response.json();

                document.getElementById('moduleTitle').value = module.title;
                document.getElementById('moduleCourse').value = module.course_id;
                document.getElementById('moduleDescription').value = module.description;
                document.getElementById('moduleType').value = module.type;

                renderContent(module);
            } catch (err) {
                console.error("Ошибка загрузки:", err);
            }
        }

        function renderContent(module) {
            const area = document.getElementById('contentArea');
            if (module.type === 'text') {
                area.innerHTML = '<div class="form-group"><label>Текст лекции *</label>' +
                    '<textarea id="contentText" rows="15" required>' + module.content.text + '</textarea></div>';
            } else if (module.type === 'question') {
                area.innerHTML = '<h3>Вопросы теста</h3><div id="questionsContainer"></div>' +
                    '<button type="button" class="add-q-btn" onclick="addQuestion()">+ Добавить вопрос</button>';
                
                module.content.forEach((q, idx) => addQuestion(q));
            }
        }

        function addQuestion(data = null) {
            const container = document.getElementById('questionsContainer');
            const qIndex = container.children.length;
            const div = document.createElement('div');
            div.className = 'question-block';
            
            div.innerHTML = ` + "`" + `
                <button type="button" class="remove-q-btn" onclick="this.parentElement.remove()">Удалить</button>
                <div class="form-group">
                    <label>Текст вопроса</label>
                    <input type="text" class="q-text" value="${data ? data.question : ''}" required>
                </div>
                <label>Варианты (отметьте верный):</label>
                ${[0,1,2,3].map(i => ` + "`" + `
                    <div class="answer-row">
                        <input type="radio" name="correct_${qIndex}" value="${i}" ${data && data.correct == i ? 'checked' : (i==0 ? 'checked' : '')}>
                        <input type="text" class="a-text" value="${data && data.answers[i] ? data.answers[i] : ''}" placeholder="Ответ ${i+1}">
                    </div>
                ` + "`" + `).join('')}
            ` + "`" + `;
            container.appendChild(div);
        }

        // 2. Сохранение изменений
        document.getElementById('editModuleForm').addEventListener('submit', async (e) => {
            e.preventDefault();
            const type = document.getElementById('moduleType').value;
            let contentData;

            if (type === 'text') {
                contentData = { text: document.getElementById('contentText').value };
            } else {
                const blocks = document.querySelectorAll('.question-block');
                contentData = Array.from(blocks).map(block => {
                    const answers = Array.from(block.querySelectorAll('.a-text')).map(i => i.value).filter(v => v !== "");
                    const correct = Array.from(block.querySelectorAll('input[type="radio"]')).findIndex(r => r.checked);
                    return { question: block.querySelector('.q-text').value, answers, correct };
                });
            }

            const formData = {
                title: document.getElementById('moduleTitle').value,
                course_id: parseInt(document.getElementById('moduleCourse').value),
                description: document.getElementById('moduleDescription').value,
                type: type,
                content: contentData
            };

            const res = await fetch('/api/modules/' + moduleId, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(formData)
            });

            if (res.ok) {
                alert("Изменения сохранены!");
                window.location.href = "/modules/view/" + moduleId;
            } else {
                alert("Ошибка при сохранении");
            }
        });

        loadModule();
    </script>
</body>
</html>`
	fmt.Fprint(w, html)
}
// SubmitTest принимает ответы студента, проверяет их и сохраняет результат в БД
func (h *ModuleHandler) SubmitTest(w http.ResponseWriter, r *http.Request) {
	// 1. Получаем ID модуля и ID студента из токена
	moduleID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	
	userClaims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Не удалось получить данные пользователя", http.StatusInternalServerError)
		return
	}
	studentID := userClaims.UserID

	// 2. Получаем ответы студента из тела запроса
	var submission struct {
		Answers []int `json:"answers"` // Массив с индексами ответов [0, 2, -1, ...]
	}
	if err := json.NewDecoder(r.Body).Decode(&submission); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	// 3. Получаем правильные ответы из базы
	var contentJSON []byte
	err := h.DB.QueryRow("SELECT content FROM modules WHERE id = $1 AND type = 'question'", moduleID).Scan(&contentJSON)
	if err != nil {
		http.Error(w, "Тест не найден или не является вопросником", http.StatusNotFound)
		return
	}

	var questions []struct {
		Correct int `json:"correct"`
	}
	if err := json.Unmarshal(contentJSON, &questions); err != nil {
		http.Error(w, "Ошибка чтения структуры теста", http.StatusInternalServerError)
		return
	}

	// 4. Считаем результат
	correctCount := 0
	if len(questions) > 0 {
		for i, q := range questions {
			if i < len(submission.Answers) && submission.Answers[i] == q.Correct {
				correctCount++
			}
		}
	}
	
	score := 0
	if len(questions) > 0 {
		score = int(float64(correctCount) / float64(len(questions)) * 100)
	}
	
	// 5. Сохраняем результат в таблицу test_results
	answersLog, _ := json.Marshal(submission.Answers)

	query := `
		INSERT INTO test_results (user_id, module_id, score, answers_log, completed_at)
		VALUES ($1, $2, $3, $4, NOW())
	`
	_, err = h.DB.Exec(query, studentID, moduleID, score, answersLog)
	if err != nil {
		http.Error(w, "Не удалось сохранить результат в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 6. Отправляем успешный ответ
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Результаты успешно сохранены",
		"score":   score,
	})
}