package main

import (
	"fmt"
	"log"
	"net/http"
    "os"

	"github.com/go-chi/chi/v5"
    "visualmath/internal/auth"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"visualmath/internal/handlers"
	"visualmath/internal/storage" // Добавляем импорт storage
)

func main() {
	fmt.Println("Запускаем VisualMath сервер...")

	godotenv.Load()

	// Инициализируем базу данных ()
	db := storage.InitPostgres() 
	
    
    // Получаем JWT_SECRET из .env
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET не установлен в .env файле!")
	}

    lectureHandler := handlers.NewLectureHandler(db)

	moduleHandler := handlers.NewModuleHandler(db)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Handle("/static/*", http.StripPrefix("/static/",
		http.FileServer(http.Dir("web/static"))))

	// Публичные маршруты
    r.Get("/lectures", lectureHandler.ListLectures)
	r.Get("/", homeHandler)
	r.Get("/login", loginPageHandler)
	r.Get("/register", registerPageHandler)
	r.Get("/dashboard", dashboardHandler)
	r.Get("/test", testHandler)
    r.Get("/lectures/create", lectureHandler.CreateLecturePage) // Страница конструктора
    r.Post("/api/lectures", lectureHandler.CreateLecture)       // API для сохранения
    // В блоке Маршруты модулей/лекций
    r.Get("/lectures/view/{id}", lectureHandler.ViewLecturePage)

	// Заглушки для OAuth (перенаправляют в кабинет)
	r.Get("/auth/vk", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	r.Get("/auth/google", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})

	// Маршруты модулей
	r.Get("/modules", moduleHandler.ListModules)              // Список модулей
	r.Get("/modules/create", moduleHandler.CreateModulePage)  // Страница создания
	r.Get("/modules/view/{id}", moduleHandler.ViewModulePage) // Просмотр модуля
	r.Get("/modules/edit/{id}", moduleHandler.EditModulePage) // Редактирование модуля
    r.With(auth.AuthMiddleware(jwtSecret)).Post("/api/modules/{id}/submit", moduleHandler.SubmitTest)

	// API endpoints для модулей
	r.Get("/api/modules/list", moduleHandler.ListModulesAPI)        // API: список модулей
	r.Post("/api/modules", moduleHandler.CreateModule)              // API: создание модуля
	r.Get("/api/modules/{id}", moduleHandler.GetModule)             // API: получить модуль
	r.Put("/api/modules/{id}", moduleHandler.UpdateModule)          // API: обновить модуль
	r.Delete("/api/modules/{id}", moduleHandler.DeleteModule)       // API: удалить модуль

	// API заглушки
	r.Post("/api/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"message": "Register endpoint"}`)
	})

	r.Post("/api/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"message": "Login endpoint"}`)
	})

	port := "8080"
	fmt.Printf("Сервер запущен на http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

// homeHandler обрабатывает главную страницу
func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>VisualMath - Главная</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="hero">
        <h1>VisualMath Platform</h1>
        <p>Платформа для интерактивного изучения математики</p>
        
        <!-- Кнопки быстрого входа через OAuth -->
        <div class="oauth-buttons" style="margin-top: 30px;">
            <a href="/auth/vk" class="oauth-btn vk">Войти через ВКонтакте</a>
            <a href="/auth/google" class="oauth-btn google">Войти через Google</a>
        </div>
        
        <!-- Стандартные кнопки -->
        <div class="buttons">
            <a href="/login" class="btn">Войти в аккаунт</a>
            <a href="/register" class="btn secondary">Создать аккаунт</a>
        </div>
    </div>

    <div class="container">
        <div class="features">
            <div class="feature-card">
                <h3>Студентам</h3>
                <p>Доступ к интерактивным материалам, решение задач и отслеживание прогресса.</p>
            </div>
            <div class="feature-card">
                <h3>Преподавателям</h3>
                <p>Создание курсов, управление модулями и контроль успеваемости студентов.</p>
            </div>
            <div class="feature-card">
                <h3>Технологии</h3>
                <p>Современный стек технологий, интерактивные графики и поддержка LaTeX.</p>
            </div>
        </div>
        
        <div style="text-align: center; margin: 40px 0; padding-top: 20px; border-top: 1px solid #eee;">
            <p style="color: #7f8c8d;">
                <a href="/test">Тестовая страница</a> | 
                <a href="/dashboard">Личный кабинет</a>
            </p>
        </div>
    </div>
    
    <script>
        window.addEventListener('DOMContentLoaded', function() {
            const token = localStorage.getItem('token');
            if (token) {
                setTimeout(() => {
                    window.location.href = '/dashboard';
                }, 1000);
            }
        });
    </script>
</body>
</html>
`
	fmt.Fprintf(w, html)
}

// loginPageHandler обрабатывает страницу входа
func loginPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Вход - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="login-container">
        <div class="login-header">
            <h1>Вход в систему</h1>
            <p>Войдите в свой аккаунт VisualMath</p>
        </div>
        
        <div id="message" class="message"></div>
        
        <div class="oauth-buttons">
            <a href="/auth/vk" class="oauth-btn vk">ВКонтакте</a>
            <a href="/auth/google" class="oauth-btn google">Google</a>
        </div>
        
        <div style="text-align: center; margin: 20px 0; color: #95a5a6; font-size: 14px;">
            или
        </div>
        
        <form id="loginForm">
            <div class="form-group">
                <label for="login">Логин или Email:</label>
                <input type="text" id="login" name="login" placeholder="Введите логин или email" required>
            </div>
            
            <div class="form-group">
                <label for="password">Пароль:</label>
                <input type="password" id="password" name="password" placeholder="Введите пароль" required>
            </div>
            
            <button type="submit" class="submit-btn">Войти</button>
        </form>
        
        <div class="form-links">
            <a href="/register">Нет аккаунта? Зарегистрироваться</a>
            <br><br>
            <a href="/">На главную</a>
        </div>
    </div>

    <script>
        document.getElementById('loginForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            const messageDiv = document.getElementById('message');
            messageDiv.style.display = 'none';
            
            const formData = {
                login: document.getElementById('login').value,
                password: document.getElementById('password').value
            };
            
            try {
                const response = await fetch('/api/login', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify(formData)
                });
                
                const result = await response.json();
                
                if (response.ok) {
                    messageDiv.className = 'message success';
                    messageDiv.textContent = 'Вход выполнен успешно! Перенаправление...';
                    messageDiv.style.display = 'block';
                    localStorage.setItem('token', result.token);
                    localStorage.setItem('user', JSON.stringify(result.user));
                    setTimeout(() => { window.location.href = '/dashboard'; }, 1500);
                } else {
                    messageDiv.className = 'message error';
                    messageDiv.textContent = result.message || 'Неверный логин или пароль';
                    messageDiv.style.display = 'block';
                }
            } catch (error) {
                messageDiv.className = 'message error';
                messageDiv.textContent = 'Ошибка сети: ' + error.message;
                messageDiv.style.display = 'block';
            }
        });
        
        window.addEventListener('DOMContentLoaded', function() {
            const token = localStorage.getItem('token');
            if (token) {
                const messageDiv = document.getElementById('message');
                messageDiv.className = 'message success';
                messageDiv.textContent = 'Вы уже авторизованы. Перенаправление...';
                messageDiv.style.display = 'block';
                setTimeout(() => { window.location.href = '/dashboard'; }, 1000);
            }
        });
    </script>
</body>
</html>
`
	fmt.Fprintf(w, html)
}

// registerPageHandler обрабатывает страницу регистрации
func registerPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Регистрация - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="register-container">
        <div class="register-header">
            <h1>Регистрация</h1>
            <p>Создайте аккаунт VisualMath</p>
        </div>
        
        <div id="message" class="message"></div>
        
        <div class="oauth-section">
            <div class="oauth-buttons">
                <a href="/auth/vk" class="oauth-btn vk">VK</a>
                <a href="/auth/google" class="oauth-btn google">Google</a>
            </div>
        </div>
        
        <div style="text-align: center; margin: 20px 0; color: #95a5a6; font-size: 14px;">
            или через email
        </div>
        
        <form id="registerForm">
            <div class="form-group">
                <label for="login">Логин *</label>
                <input type="text" id="login" name="login" placeholder="Придумайте логин" required>
            </div>
            <div class="form-group">
                <label for="email">Email *</label>
                <input type="email" id="email" name="email" placeholder="Ваш email" required>
            </div>
            <div class="form-group">
                <label for="full_name">ФИО *</label>
                <input type="text" id="full_name" name="full_name" placeholder="Иванов Иван Иванович" required>
            </div>
            <div class="form-group">
                <label for="user_type">Тип пользователя *</label>
                <select id="user_type" name="user_type" required>
                    <option value="">Выберите тип</option>
                    <option value="student">Студент</option>
                    <option value="teacher">Преподаватель</option>
                </select>
            </div>
            <div class="form-group">
                <label for="password">Пароль *</label>
                <input type="password" id="password" name="password" placeholder="Придумайте пароль" required>
                <small style="color: #7f8c8d; font-size: 12px;">Минимум 8 символов</small>
            </div>
            
            <div class="form-group" id="groupField" style="display: none;">
                <label for="group_number">Номер группы *</label>
                <input type="text" id="group_number" name="group_number" placeholder="Например: ИУ6-32Б">
            </div>
            
            <button type="submit" class="submit-btn">Создать аккаунт</button>
        </form>
        
        <div class="form-links">
            <a href="/login">Уже есть аккаунт? Войти</a>
            <br><br>
            <a href="/">На главную</a>
        </div>
    </div>

    <script>
        document.getElementById('user_type').addEventListener('change', function() {
            const groupField = document.getElementById('groupField');
            const groupInput = document.getElementById('group_number');
            if (this.value === 'student') {
                groupField.style.display = 'block';
                groupInput.required = true;
            } else {
                groupField.style.display = 'none';
                groupInput.required = false;
            }
        });
        
        document.getElementById('registerForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            const messageDiv = document.getElementById('message');
            messageDiv.style.display = 'none';
            
            const formData = {
                login: document.getElementById('login').value,
                password: document.getElementById('password').value,
                full_name: document.getElementById('full_name').value,
                email: document.getElementById('email').value,
                user_type: document.getElementById('user_type').value,
                group_number: document.getElementById('group_number').value
            };
            
            if (formData.password.length < 8) {
                messageDiv.className = 'message error';
                messageDiv.textContent = 'Пароль должен содержать минимум 8 символов';
                messageDiv.style.display = 'block';
                return;
            }
            
            try {
                const response = await fetch('/api/register', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify(formData)
                });
                const result = await response.json();
                
                if (response.ok) {
                    messageDiv.className = 'message success';
                    messageDiv.textContent = 'Регистрация успешна! Перенаправление...';
                    messageDiv.style.display = 'block';
                    document.getElementById('registerForm').reset();
                    setTimeout(() => { window.location.href = '/login'; }, 2000);
                } else {
                    messageDiv.className = 'message error';
                    messageDiv.textContent = result.message || 'Ошибка регистрации';
                    messageDiv.style.display = 'block';
                }
            } catch (error) {
                messageDiv.className = 'message error';
                messageDiv.textContent = 'Ошибка сети: ' + error.message;
                messageDiv.style.display = 'block';
            }
        });
    </script>
</body>
</html>
`
	fmt.Fprintf(w, html)
}

// dashboardHandler показывает личный кабинет
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Личный кабинет - VisualMath</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="dashboard-container">
        <!-- Боковое меню -->
        <aside class="sidebar">
            <div class="user-info">
                <h3>Тестовый пользователь</h3>
                <p>Тип: Преподаватель</p>
                <div style="margin-top: 15px;">
                    <a href="/" style="color: #e74c3c; font-size: 14px;">Выйти из системы</a>
                </div>
            </div>
            
            <div class="menu-section">
                <h3>Преподаватель</h3>
                <ul>
                    <li><a href="/modules">Библиотека модулей</a></li>
                    <li><a href="/modules/create">Создать модуль</a></li>
                    <li><a href="/lectures">Библиотека лекций</a></li>
                    <li><a href="/lectures/create">Создать лекцию</a></li>
                </ul>
            </div>

            <div class="menu-section">
                <h3>Настройки</h3>
                <ul>
                    <li><a href="#">Профиль</a></li>
                    <li><a href="#">Уведомления</a></li>
                </ul>
            </div>
        </aside>

        <!-- Основной контент -->
        <main class="main-content">
            <div class="welcome-card">
                <h1>Добро пожаловать в VisualMath!</h1>
                <p>Это ваш личный кабинет. Здесь вы можете управлять учебными материалами, создавать новые модули и проводить занятия.</p>
                <div style="margin-top: 20px;">
                     <a href="/modules" class="btn">Перейти к модулям</a>
                     <a href="/modules/create" class="btn secondary" style="border: 2px solid #3498db; color: #3498db;">Создать новый</a>
                </div>
            </div>

            <!-- Статистика -->
            <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 20px; margin-top: 30px;">
                <div class="feature-card" style="width: auto; text-align: left; border-top-color: #2ecc71;">
                    <h3>Активные курсы</h3>
                    <p style="font-size: 24px; color: #2c3e50; font-weight: bold;">3</p>
                    <p style="font-size: 14px; color: #7f8c8d;">Математический анализ, Линейная алгебра</p>
                </div>
                <div class="feature-card" style="width: auto; text-align: left; border-top-color: #e67e22;">
                    <h3>Студентов</h3>
                    <p style="font-size: 24px; color: #2c3e50; font-weight: bold;">128</p>
                    <p style="font-size: 14px; color: #7f8c8d;">Всего на курсах</p>
                </div>
                <div class="feature-card" style="width: auto; text-align: left; border-top-color: #9b59b6;">
                    <h3>Модулей</h3>
                    <p style="font-size: 24px; color: #2c3e50; font-weight: bold;">12</p>
                    <p style="font-size: 14px; color: #7f8c8d;">Создано материалов</p>
                </div>
            </div>
        </main>
    </div>
</body>
</html>
`
	fmt.Fprintf(w, html)
}

// testHandler для проверки работы
func testHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := `
    <!DOCTYPE html>
    <html>
    <head><title>Тест</title></head>
    <body style="padding: 50px; text-align: center; font-family: sans-serif;">
        <h1 style="color: green;">Тест пройден!</h1>
        <p>Сервер работает правильно</p>
        <p><a href="/">Вернуться на главную</a></p>
    </body>
    </html>
    `
	fmt.Fprintf(w, html)
}