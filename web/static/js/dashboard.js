// Dashboard JavaScript

class Dashboard {
    constructor() {
        this.token = localStorage.getItem('token');
        this.user = JSON.parse(localStorage.getItem('user') || '{}');
        this.init();
    }
    
    init() {
        if (!this.token) {
            this.redirectToLogin();
            return;
        }
        
        this.loadUserData();
        this.setupEventListeners();
        this.showSectionsByRole();
    }
    
    async loadUserData() {
        try {
            const response = await fetch('/api/user/profile', {
                headers: {
                    'Authorization': `Bearer ${this.token}`
                }
            });
            
            if (response.ok) {
                const userData = await response.json();
                this.user = userData;
                localStorage.setItem('user', JSON.stringify(userData));
                this.updateUI();
            } else {
                this.redirectToLogin();
            }
            
        } catch (error) {
            console.error('Error loading user data:', error);
        }
    }
    
    updateUI() {
        // Обновляем информацию о пользователе
        document.getElementById('userName').textContent = this.user.full_name;
        document.getElementById('sidebarUserName').textContent = this.user.full_name;
        document.getElementById('sidebarUserType').textContent = `Тип: ${this.user.user_type}`;
        document.getElementById('sidebarUserGroup').textContent = `Группа: ${this.user.group_number || 'Не указана'}`;
        
        document.getElementById('profileType').textContent = this.user.user_type;
        document.getElementById('profileGroup').textContent = this.user.group_number || 'Не указана';
        document.getElementById('profileEmail').textContent = this.user.email;
        
        document.getElementById('welcomeMessage').textContent = 
            `Добро пожаловать, ${this.user.full_name}! Вы вошли как ${this.user.user_type}.`;
    }
    
    showSectionsByRole() {
        const role = this.user.user_type;
        
        // Показываем соответствующие секции меню
        if (role === 'student') {
            document.querySelector('.student-section').classList.add('show-section');
        } else if (role === 'teacher') {
            document.querySelector('.teacher-section').classList.add('show-section');
        } else if (role === 'admin') {
            document.querySelector('.admin-section').classList.add('show-section');
            document.querySelector('.teacher-section').classList.add('show-section');
        }
    }
    
    setupEventListeners() {
        // Навигация по меню
        document.querySelectorAll('.menu-link').forEach(link => {
            link.addEventListener('click', (e) => {
                e.preventDefault();
                this.switchSection(e.target.dataset.section);
            });
        });
        
        // Выход из системы
        document.getElementById('logoutBtn').addEventListener('click', () => {
            this.logout();
        });
        
        // Создание модуля
        const moduleForm = document.getElementById('createModuleForm');
        if (moduleForm) {
            moduleForm.addEventListener('submit', (e) => {
                e.preventDefault();
                this.createModule();
            });
        }
        
        // Изменение типа модуля
        const moduleTypeSelect = document.getElementById('moduleType');
        if (moduleTypeSelect) {
            moduleTypeSelect.addEventListener('change', (e) => {
                this.changeModuleType(e.target.value);
            });
        }
    }
    
    switchSection(sectionId) {
        // Скрываем все секции
        document.querySelectorAll('.content-section').forEach(section => {
            section.classList.remove('active');
        });
        
        // Убираем активный класс у всех ссылок
        document.querySelectorAll('.menu-link').forEach(link => {
            link.classList.remove('active');
        });
        
        // Показываем выбранную секцию
        const targetSection = document.getElementById(sectionId);
        if (targetSection) {
            targetSection.classList.add('active');
        }
        
        // Активируем соответствующую ссылку
        const activeLink = document.querySelector(`[data-section="${sectionId}"]`);
        if (activeLink) {
            activeLink.classList.add('active');
        }
        
        // Загружаем данные для секции
        this.loadSectionData(sectionId);
    }
    
    loadSectionData(sectionId) {
        switch(sectionId) {
            case 'create-module':
                this.loadCourses();
                break;
            case 'lectures':
                this.loadLectures();
                break;
        }
    }
    
    async loadCourses() {
        try {
            // Здесь будет запрос к API для получения курсов
            // Временные данные
            const courses = [
                { id: 1, name: 'Математический анализ' },
                { id: 2, name: 'Линейная алгебра и аналитическая геометрия' },
                { id: 3, name: 'Дискретная математика' },
                { id: 4, name: 'Экономика' }
            ];
            
            const select = document.getElementById('moduleCourse');
            select.innerHTML = '<option value="">Выберите предмет</option>';
            
            courses.forEach(course => {
                const option = document.createElement('option');
                option.value = course.id;
                option.textContent = course.name;
                select.appendChild(option);
            });
            
        } catch (error) {
            console.error('Error loading courses:', error);
        }
    }
    
    changeModuleType(type) {
        const container = document.getElementById('moduleContentContainer');
        
        switch(type) {
            case 'text':
                container.innerHTML = `
                    <div class="form-group">
                        <label for="moduleText">Текст модуля *</label>
                        <textarea id="moduleText" name="content" 
                                  placeholder="Введите текст лекции..." required></textarea>
                        <small>Можно использовать LaTeX для формул: $E = mc^2$</small>
                        
                        <div style="margin-top: 15px;">
                            <label>Загрузить изображения:</label>
                            <input type="file" id="moduleImages" multiple accept="image/*">
                            <div id="imagePreview"></div>
                        </div>
                    </div>
                `;
                break;
                
            case 'visual':
                container.innerHTML = `
                    <div class="form-group">
                        <label for="visualFile">Файл визуального модуля *</label>
                        <input type="file" id="visualFile" name="visual_file" required>
                        <small>Поддерживаемые форматы: .json, .xml из библиотек VisualMath</small>
                        
                        <div style="margin-top: 15px;">
                            <label>Конфигурация:</label>
                            <textarea id="visualConfig" name="config" 
                                      placeholder='{"width": 800, "height": 600, "interactive": true}'></textarea>
                        </div>
                    </div>
                `;
                break;
                
            case 'question':
                container.innerHTML = `
                    <div class="form-group">
                        <label for="questions">Вопросы (JSON формат) *</label>
                        <textarea id="questions" name="questions" 
                                  placeholder='[
  {
    "question": "Вопрос 1",
    "answers": ["Ответ 1", "Ответ 2", "Ответ 3"],
    "correct": 0
  }
]' required></textarea>
                        <small>Каждый вопрос должен иметь поле "question", "answers" (массив) и "correct" (индекс правильного ответа)</small>
                    </div>
                `;
                break;
                
            case 'test':
                container.innerHTML = `
                    <div class="form-group">
                        <label for="testConfig">Конфигурация теста *</label>
                        <textarea id="testConfig" name="test_config" 
                                  placeholder='{
  "time_limit": 60,
  "questions_count": 10,
  "passing_score": 70,
  "show_results": true
}' required></textarea>
                        
                        <div style="margin-top: 15px;">
                            <label>Выбрать вопросы из:</label>
                            <select id="testSource">
                                <option value="manual">Ввести вручную</option>
                                <option value="library">Библиотека вопросов</option>
                            </select>
                        </div>
                    </div>
                `;
                break;
                
            default:
                container.innerHTML = '<p>Выберите тип модуля</p>';
        }
    }
    
    async createModule() {
        const form = document.getElementById('createModuleForm');
        const formData = new FormData(form);
        
        const moduleData = {
            title: formData.get('title'),
            course_id: parseInt(formData.get('course_id')),
            module_type: formData.get('module_type'),
            content: this.getModuleContent(formData)
        };
        
        try {
            const response = await fetch('/api/modules', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.token}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(moduleData)
            });
            
            if (response.ok) {
                alert('✅ Модуль успешно создан!');
                form.reset();
                document.getElementById('moduleContentContainer').innerHTML = 
                    '<p>Выберите тип модуля</p>';
            } else {
                const error = await response.text();
                alert('❌ Ошибка: ' + error);
            }
            
        } catch (error) {
            alert('❌ Ошибка сети: ' + error.message);
        }
    }
    
    getModuleContent(formData) {
        const type = formData.get('module_type');
        
        switch(type) {
            case 'text':
                return {
                    text: formData.get('content'),
                    images: [] // Здесь будут ссылки на загруженные изображения
                };
                
            case 'question':
                try {
                    return JSON.parse(formData.get('questions'));
                } catch {
                    return { error: 'Неверный JSON формат' };
                }
                
            case 'test':
                try {
                    return JSON.parse(formData.get('test_config'));
                } catch {
                    return { error: 'Неверный JSON формат' };
                }
                
            default:
                return {};
        }
    }
    
    logout() {
        localStorage.removeItem('token');
        localStorage.removeItem('user');
        this.redirectToLogin();
    }
    
    redirectToLogin() {
        window.location.href = '/login';
    }
    
    async searchLectures() {
        const searchText = document.getElementById('lectureSearch').value;
        const sortBy = document.getElementById('lectureSort').value;
        
        try {
            const response = await fetch(`/api/lectures?search=${encodeURIComponent(searchText)}&sort=${sortBy}`, {
                headers: {
                    'Authorization': `Bearer ${this.token}`
                }
            });
            
            if (response.ok) {
                const lectures = await response.json();
                this.displayLectures(lectures);
            }
            
        } catch (error) {
            console.error('Error searching lectures:', error);
        }
    }
    
    displayLectures(lectures) {
        const container = document.getElementById('lectureList');
        
        if (!lectures || lectures.length === 0) {
            container.innerHTML = '<p>Лекции не найдены</p>';
            return;
        }
        
        const html = lectures.map(lecture => `
            <div class="lecture-card">
                <h3>${lecture.title}</h3>
                <p><strong>Предмет:</strong> ${lecture.course_name}</p>
                <p><strong>Автор:</strong> ${lecture.author_name}</p>
                <p><strong>Дата:</strong> ${new Date(lecture.created_at).toLocaleDateString()}</p>
                <button onclick="viewLecture(${lecture.id})">Открыть</button>
            </div>
        `).join('');
        
        container.innerHTML = html;
    }
}

// Глобальные функции
function changeModuleType() {
    const select = document.getElementById('moduleType');
    if (select) {
        window.dashboard.changeModuleType(select.value);
    }
}

function searchLectures() {
    if (window.dashboard) {
        window.dashboard.searchLectures();
    }
}

// Инициализация при загрузке страницы
document.addEventListener('DOMContentLoaded', () => {
    window.dashboard = new Dashboard();
});