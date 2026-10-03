-- Таблица для хранения связей с OAuth провайдерами
CREATE TABLE oauth_connections (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(20) NOT NULL, -- 'vk', 'google'
    provider_user_id VARCHAR(100) NOT NULL, -- ID пользователя у провайдера
    email VARCHAR(255),
    full_name VARCHAR(255),
    avatar_url TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, provider_user_id)
);

-- Индексы для быстрого поиска
CREATE INDEX idx_oauth_user ON oauth_connections(user_id);
CREATE INDEX idx_oauth_provider ON oauth_connections(provider, provider_user_id);
-- Таблица тестовых сессий
CREATE TABLE IF NOT EXISTS test_sessions (
    id TEXT PRIMARY KEY,
    lecture_id INTEGER NOT NULL,
    module_id INTEGER,
    title TEXT NOT NULL,
    duration INTEGER NOT NULL, -- в секундах
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    is_active BOOLEAN DEFAULT TRUE,
    config JSON,
    questions JSON NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (lecture_id) REFERENCES lectures(id),
    FOREIGN KEY (module_id) REFERENCES modules(id)
);

-- Таблица результатов тестов
CREATE TABLE IF NOT EXISTS student_test_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id INTEGER NOT NULL,
    student_name TEXT NOT NULL,
    group_number TEXT NOT NULL,
    test_session_id TEXT NOT NULL,
    lecture_id INTEGER NOT NULL,
    score REAL NOT NULL,
    max_score REAL NOT NULL,
    percentage REAL NOT NULL,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    answers JSON NOT NULL,
    correctness JSON NOT NULL,
    time_spent INTEGER NOT NULL, -- в секундах
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (student_id) REFERENCES users(id),
    FOREIGN KEY (test_session_id) REFERENCES test_sessions(id),
    FOREIGN KEY (lecture_id) REFERENCES lectures(id)
);

-- Индексы для быстрого поиска
CREATE INDEX idx_test_session ON student_test_results(test_session_id);
CREATE INDEX idx_student_test ON student_test_results(student_id, lecture_id);
CREATE INDEX idx_lecture_test ON student_test_results(lecture_id, completed_at);


-- Таблица активных сессий (запущенных лекций)
CREATE TABLE IF NOT EXISTS active_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id VARCHAR(50) NOT NULL UNIQUE, -- Уникальный ID сессии
    lecture_id INTEGER NOT NULL,
    teacher_id INTEGER NOT NULL,
    session_name VARCHAR(255),
    join_code VARCHAR(10), -- Код для присоединения
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMP,
    is_active BOOLEAN DEFAULT 1,
    FOREIGN KEY (lecture_id) REFERENCES lectures(id),
    FOREIGN KEY (teacher_id) REFERENCES users(id)
);

-- Студенты в активных сессиях
CREATE TABLE IF NOT EXISTS session_students (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id VARCHAR(50) NOT NULL,
    student_id INTEGER NOT NULL,
    joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    left_at TIMESTAMP,
    is_active BOOLEAN DEFAULT 1,
    FOREIGN KEY (session_id) REFERENCES active_sessions(session_id),
    FOREIGN KEY (student_id) REFERENCES users(id),
    UNIQUE(session_id, student_id)
);

-- История действий в сессии
CREATE TABLE IF NOT EXISTS session_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id VARCHAR(50) NOT NULL,
    student_id INTEGER NOT NULL,
    action_type VARCHAR(50), -- 'join', 'leave', 'answer', 'complete'
    action_data TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES active_sessions(session_id),
    FOREIGN KEY (student_id) REFERENCES users(id)
);

-- Состояние сессии (текущий слайд и другие данные)
CREATE TABLE IF NOT EXISTS session_state (
    session_id VARCHAR(50) PRIMARY KEY,
    current_slide TEXT DEFAULT '{"slide_index": 0}',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES active_sessions(session_id)
);