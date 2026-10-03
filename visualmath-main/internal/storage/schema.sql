-- 1. Сначала удаляем старые таблицы, если они есть (чтобы создать чисто)
DROP TABLE IF EXISTS test_results;
DROP TABLE IF EXISTS lecture_modules;
DROP TABLE IF EXISTS lectures;
DROP TABLE IF EXISTS oauth_connections;
DROP TABLE IF EXISTS modules;
DROP TABLE IF EXISTS courses;
DROP TABLE IF EXISTS users;

-- 2. Таблица пользователей
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    login TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    full_name TEXT NOT NULL,
    user_type TEXT NOT NULL CHECK (user_type IN ('student', 'teacher', 'admin')),
    group_number TEXT,
    email TEXT UNIQUE NOT NULL,
    email_verified BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- 3. Таблица OAuth (для входа через VK/Google)
CREATE TABLE oauth_connections (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    provider VARCHAR(20) NOT NULL,
    provider_user_id VARCHAR(100) NOT NULL,
    email VARCHAR(255),
    full_name VARCHAR(255),
    avatar_url TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, provider_user_id),
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- 4. Таблица предметов (Курсов)
CREATE TABLE courses (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL
);

-- 5. Таблица Модулей
-- content хранит JSON, но для лучшей производительности в PostgreSQL можно использовать тип JSONB
CREATE TABLE modules (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    type TEXT NOT NULL CHECK(type IN ('text', 'visual', 'question', 'test')),
    content JSONB, 
    course_id INTEGER,
    author_id INTEGER,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(course_id) REFERENCES courses(id),
    FOREIGN KEY(author_id) REFERENCES users(id)
);

-- 6. Таблица Лекций (Контейнер для модулей)
CREATE TABLE lectures (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    course_id INTEGER,
    author_id INTEGER,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(course_id) REFERENCES courses(id),
    FOREIGN KEY(author_id) REFERENCES users(id)
);

-- 7. Связующая таблица: Какие модули входят в лекцию и в каком порядке
CREATE TABLE lecture_modules (
    lecture_id INTEGER,
    module_id INTEGER,
    order_num INTEGER, -- Порядковый номер модуля в лекции (1, 2, 3...)
    PRIMARY KEY (lecture_id, module_id),
    FOREIGN KEY(lecture_id) REFERENCES lectures(id) ON DELETE CASCADE,
    FOREIGN KEY(module_id) REFERENCES modules(id) ON DELETE CASCADE
);

-- 8. Таблица результатов студентов (кто, что решал, сколько баллов)
CREATE TABLE test_results (
    id SERIAL PRIMARY KEY,
    user_id INTEGER,
    module_id INTEGER,
    score INTEGER, -- Оценка или процент правильных ответов
    answers_log JSONB, -- JSON с тем, как именно ответил студент (для истории)
    completed_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id),
    FOREIGN KEY(module_id) REFERENCES modules(id)
);

-- 9. Заполнение начальными данными (Предметы)
INSERT INTO courses (name) VALUES 
('Математический анализ'),
('Линейная алгебра и аналитическая геометрия'),
('Дискретная математика'),
('Экономика'),
('Физика'),
('Дифференциальные уравнения'),
('Литература')
ON CONFLICT (name) DO NOTHING;