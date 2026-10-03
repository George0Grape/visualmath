package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5" // Добавили этот импорт
)

type LectureHandler struct {
	DB *sql.DB
}

func NewLectureHandler(db *sql.DB) *LectureHandler {
	return &LectureHandler{DB: db}
}

// CreateLecturePage - Показывает страницу конструктора лекций
func (h *LectureHandler) CreateLecturePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	// Здесь мы просто отдаем HTML
	fmt.Fprint(w, createLectureHTML)
}

// CreateLecture - API метод для сохранения лекции в БД
func (h *LectureHandler) CreateLecture(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		CourseID    int    `json:"course_id"`
		ModuleIDs   []int  `json:"module_ids"` // Список ID модулей в нужном порядке
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Начинаем транзакцию
	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, "Ошибка транзакции", http.StatusInternalServerError)
		return
	}

	// 1. Создаем саму лекцию
	var lectureID int
	authorID := 1 // Заглушка
	err = tx.QueryRow(`
		INSERT INTO lectures (title, description, course_id, author_id, created_at)
		VALUES ($1, $2, $3, $4, NOW()) RETURNING id`,
		request.Title, request.Description, request.CourseID, authorID,
	).Scan(&lectureID)

	if err != nil {
		tx.Rollback()
		http.Error(w, "Ошибка сохранения лекции: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Привязываем модули к лекции
	for i, modID := range request.ModuleIDs {
		_, err = tx.Exec(`
			INSERT INTO lecture_modules (lecture_id, module_id, order_num)
			VALUES ($1, $2, $3)`,
			lectureID, modID, i+1,
		)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Ошибка привязки модуля", http.StatusInternalServerError)
			return
		}
	}

	tx.Commit()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Лекция успешно собрана!",
	})
}

// ListLectures - Страница со списком всех лекций
func (h *LectureHandler) ListLectures(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Получаем лекции из базы
	rows, err := h.DB.Query(`
		SELECT l.id, l.title, l.description, c.name 
		FROM lectures l 
		LEFT JOIN courses c ON l.course_id = c.id 
		ORDER BY l.created_at DESC`)
	
	if err != nil {
		http.Error(w, "Ошибка БД", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var listHTML string
	for rows.Next() {
		var id int
		var title, desc, courseName string
		rows.Scan(&id, &title, &desc, &courseName)
		
		listHTML += fmt.Sprintf(`
			<div class="module-card" style="padding:15px; margin-bottom:10px; border:1px solid #ddd; background:white; border-radius:8px;">
				<h3>%s</h3>
				<p>Предмет: %s</p>
				<a href="/lectures/view/%d" class="btn primary" style="padding:5px 15px; text-decoration:none;">Открыть лекцию</a>
			</div>`, title, courseName, id)
	}

	// Выводим простую страницу списка
	fmt.Fprintf(w, `
		<html><head><link rel="stylesheet" href="/static/css/style.css"></head>
		<body style="padding:40px; background:#f4f7f6;">
			<h1>Библиотека лекций</h1>
			<a href="/dashboard">← В кабинет</a> | <a href="/lectures/create">+ Создать новую</a>
			<div style="margin-top:20px;">%s</div>
		</body></html>`, listHTML)
}

// ViewLecturePage - Страница, где студент проходит лекцию модуль за модулем
func (h *LectureHandler) ViewLecturePage(w http.ResponseWriter, r *http.Request) {
	lectureID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	// Передаем ID лекции в HTML шаблон
	fmt.Fprint(w, fmt.Sprintf(viewLectureHTML, lectureID))
}

// --- HTML ШАБЛОНЫ ---

const createLectureHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>Конструктор лекций - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
    <style>
        .module-item { 
            padding: 10px; border: 1px solid #ddd; margin-bottom: 5px; 
            cursor: pointer; background: white; border-radius: 5px;
            display: flex; justify-content: space-between;
        }
        .module-item:hover { background: #f0f7ff; }
        .selected-list { border: 2px dashed #3498db; min-height: 50px; padding: 10px; border-radius: 8px; }
        .btn-add { background: #2ecc71; color: white; border: none; padding: 2px 10px; border-radius: 3px; cursor: pointer; }
    </style>
</head>
<body>
    <div class="container" style="max-width: 900px; margin: 40px auto; padding: 20px;">
        <h1>Создание новой лекции</h1>
        <div class="form-group">
            <label>Название лекции</label>
            <input type="text" id="lecTitle" placeholder="Например: Основы производных">
        </div>
        <div class="form-group">
            <label>Предмет</label>
            <select id="lecCourse">
                <option value="1">Математический анализ</option>
                <option value="2">Линейная алгебра</option>
            </select>
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 20px; margin-top: 20px;">
            <div>
                <h3>1. Доступные модули</h3>
                <div id="availableModules">Загрузка...</div>
            </div>
            <div>
                <h3>2. Состав лекции</h3>
                <div id="selectedModules" class="selected-list"></div>
            </div>
        </div>
        <button onclick="saveLecture()" class="submit-btn" style="margin-top: 30px; width: 100%;">Создать лекцию</button>
    </div>
    <script>
        let allModules = [];
        let selectedModuleIDs = [];
        async function loadModules() {
            const res = await fetch('/api/modules/list');
            allModules = await res.json();
            renderAvailable();
        }
        function renderAvailable() {
            const container = document.getElementById('availableModules');
            container.innerHTML = '';
            allModules.forEach(mod => {
                if (selectedModuleIDs.includes(mod.id)) return;
                const div = document.createElement('div');
                div.className = 'module-item';
                div.innerHTML = '<span>[' + mod.type + '] ' + mod.title + '</span><button class="btn-add" onclick="addModule(' + mod.id + ')">+</button>';
                container.appendChild(div);
            });
        }
        function addModule(id) {
            selectedModuleIDs.push(id);
            renderAvailable();
            renderSelected();
        }
        function removeModule(id) {
            selectedModuleIDs = selectedModuleIDs.filter(m => m !== id);
            renderAvailable();
            renderSelected();
        }
        function renderSelected() {
            const container = document.getElementById('selectedModules');
            container.innerHTML = '';
            selectedModuleIDs.forEach((id, index) => {
                const mod = allModules.find(m => m.id === id);
                const div = document.createElement('div');
                div.className = 'module-item';
                div.innerHTML = '<span>' + (index+1) + '. ' + mod.title + '</span><button style="background:red; color:white; border:none;" onclick="removeModule(' + id + ')">×</button>';
                container.appendChild(div);
            });
        }
        async function saveLecture() {
            const data = {
                title: document.getElementById('lecTitle').value,
                description: "",
                course_id: parseInt(document.getElementById('lecCourse').value),
                module_ids: selectedModuleIDs
            };
            const res = await fetch('/api/lectures', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(data)
            });
            if (res.ok) { alert("Лекция создана!"); window.location.href = "/dashboard"; }
        }
        loadModules();
    </script>
</body>
</html>
`

const viewLectureHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>Прохождение лекции - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
    <style>
        .step-container { max-width: 800px; margin: 30px auto; padding: 20px; background: white; border-radius: 12px; box-shadow: 0 4px 15px rgba(0,0,0,0.1); }
        .progress-bar { height: 10px; background: #eee; border-radius: 5px; margin-bottom: 20px; overflow: hidden; }
        .progress-fill { height: 100%; background: #3498db; width: 0%%; transition: width 0.3s; }
        .nav-buttons { display: flex; justify-content: space-between; margin-top: 30px; }
        .hidden { display: none; }
    </style>
</head>
<body>
    <div class="step-container">
        <div class="progress-bar"><div id="progressFill" class="progress-fill"></div></div>
        <h2 id="lectureTitle">Загрузка...</h2>
        <hr>
        <div id="moduleContentArea"></div>
        <div class="nav-buttons">
            <button id="prevBtn" class="btn btn-back hidden" onclick="prevStep()">Назад</button>
            <button id="nextBtn" class="btn primary" onclick="nextStep()">Далее</button>
        </div>
    </div>
    <script>
        const lectureId = %s;
        let modules = [];
        let currentStep = 0;
        async function loadLecture() {
            const res = await fetch('/api/modules/list'); 
            modules = await res.json(); 
            renderStep();
        }
        function renderStep() {
            if (modules.length === 0) return;
            const mod = modules[currentStep];
            const area = document.getElementById('moduleContentArea');
            document.getElementById('progressFill').style.width = ((currentStep + 1) / modules.length * 100) + '%%';
            area.innerHTML = '<h3>' + mod.title + '</h3><div id="content">Загрузка...</div>';
            fetch('/api/modules/' + mod.id).then(r => r.json()).then(data => {
                if (data.type === 'text') {
                    area.querySelector('#content').innerHTML = data.content.text.replace(/\n/g, '<br>');
                } else {
                    area.querySelector('#content').innerHTML = '<p>Модуль-тест. <a href="/modules/view/'+mod.id+'" target="_blank">Открыть тест в новом окне</a></p>';
                }
            });
            document.getElementById('prevBtn').classList.toggle('hidden', currentStep === 0);
            document.getElementById('nextBtn').innerText = (currentStep === modules.length - 1) ? 'Завершить' : 'Далее';
        }
        function nextStep() {
            if (currentStep < modules.length - 1) { currentStep++; renderStep(); }
            else { alert('Лекция пройдена!'); window.location.href = '/dashboard'; }
        }
        function prevStep() {
            if (currentStep > 0) { currentStep--; renderStep(); }
        }
        loadLecture();
    </script>
</body>
</html>
`