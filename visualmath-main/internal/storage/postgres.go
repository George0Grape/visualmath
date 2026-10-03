package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq" // Драйвер для Postgres
)

func InitPostgres() *sql.DB {
	// Считываем настройки из .env или используем значения по умолчанию
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" { dbHost = "localhost" }
	
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" { dbPort = "5432" }
	
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" { dbUser = "postgres" }
	
	dbPass := os.Getenv("DB_PASSWORD")
	if dbPass == "" { dbPass = "password" } // Поменяйте на свой пароль, если нет в .env
	
	dbName := os.Getenv("DB_NAME")
	if dbName == "" { dbName = "visualmath" }

	// Формируем строку подключения
	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		log.Fatal("Ошибка открытия соединения с БД: ", err)
	}

	// Проверяем соединение
	err = db.Ping()
	if err != nil {
		log.Fatalf("Не удалось подключиться к базе данных (DSN=%s): %v", psqlInfo, err)
	}

	fmt.Println("Успешное подключение к PostgreSQL!")

	// Создаем таблицы
	createTablesPostgres(db)

	return db
}

func createTablesPostgres(db *sql.DB) {
	queries := []string{
		// 1. Таблица пользователей
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			login TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			full_name TEXT NOT NULL,
			user_type TEXT NOT NULL, -- 'student' или 'teacher'
			group_number TEXT,
			email TEXT UNIQUE NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		// 2. Таблица курсов
		`CREATE TABLE IF NOT EXISTS courses (
			id SERIAL PRIMARY KEY,
			name TEXT UNIQUE NOT NULL
		);`,

		// 3. Таблица модулей
		`CREATE TABLE IF NOT EXISTS modules (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			course_id INTEGER REFERENCES courses(id),
			description TEXT,
			type TEXT NOT NULL,
			content JSONB, 
			author_id INTEGER REFERENCES users(id),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		
		// 4. Создаем базовые курсы (если их нет)
		`INSERT INTO courses (name) VALUES 
			('Математический анализ'),
			('Линейная алгебра'),
			('Дискретная математика'),
			('Экономика')
		ON CONFLICT (name) DO NOTHING;`,

		
		`INSERT INTO users (id, login, password_hash, full_name, user_type, email) 
		 OVERRIDING SYSTEM VALUE -- Нужно для принудительной вставки ID в Postgres
		 VALUES (1, 'teacher', 'hash_stub', 'Главный Преподаватель', 'teacher', 'teacher@example.com')
		 ON CONFLICT (id) DO NOTHING;`, 
		 // Если пользователь с ID 1 уже есть, ничего не делаем
	}

	for _, query := range queries {
		_, err := db.Exec(query)
		if err != nil {
			log.Printf("Ошибка при инициализации БД: %v\nЗапрос: %s", err, query)
		}
	}
}