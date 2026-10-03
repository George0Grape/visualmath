package storage

import (
    "database/sql"
    "log"
    "os"
    "path/filepath"
    "strings"
    "time"

    _ "modernc.org/sqlite"
)

// sqliteDSN строит строку подключения для modernc.org/sqlite (pure Go, без CGO).
// Каждый pragma через _pragma=... применяется к каждому новому соединению
// автоматически; _txlock=immediate открывает пишущие транзакции через
// BEGIN IMMEDIATE — меньше гонок между писателями при WAL.
func sqliteDSN(dbPath string) string {
    return dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=cache_size(10000)&_pragma=temp_store(MEMORY)&_pragma=foreign_keys(ON)&_txlock=immediate"
}

func InitSQLite() *sql.DB {
    dbPath := "visualmath.db"
    if exe, err := os.Executable(); err == nil {
        exeDir := filepath.Dir(exe)
        if _, statErr := os.Stat(filepath.Join(exeDir, "web", "templates")); statErr == nil {
            dbPath = filepath.Join(exeDir, "visualmath.db")
        }
    }
    dsn := sqliteDSN(dbPath)
    log.Printf("📂 База данных: %s", dbPath)
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        log.Fatal(err)
    }
    if err := db.Ping(); err != nil {
        log.Fatalf("не удалось подключиться к БД: %v", err)
    }

    // WAL даёт параллельных читателей; busy_timeout обеспечивает retry при конкуренции писателей
    db.SetMaxOpenConns(10)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(time.Hour)

    // Подтверждаем, что ключевые pragma применились (видно в логах при старте/деплое)
    var journalMode, foreignKeys string
    db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
    db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
    log.Printf("🔧 SQLite: journal_mode=%s foreign_keys=%s", journalMode, foreignKeys)

    queries := []string{
        `CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            login TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL,
            full_name TEXT NOT NULL,
            user_type TEXT NOT NULL CHECK (user_type IN ('student', 'teacher', 'admin')),
            group_number TEXT,
            email TEXT UNIQUE NOT NULL,
            email_verified BOOLEAN DEFAULT FALSE,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,

        `CREATE TABLE IF NOT EXISTS courses (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT UNIQUE NOT NULL
        )`,

        `CREATE TABLE IF NOT EXISTS modules (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT NOT NULL,
            course TEXT NOT NULL,
            description TEXT,
            module_type TEXT CHECK(module_type IN ('text', 'question', 'test')),
            content TEXT,
            author_id INTEGER,
            published BOOLEAN DEFAULT 1,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (author_id) REFERENCES users(id)
        )`,

        `CREATE TABLE IF NOT EXISTS lectures (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT NOT NULL,
            course TEXT NOT NULL,
            description TEXT,
            author_id INTEGER,
            published BOOLEAN DEFAULT 1,
            allow_back BOOLEAN DEFAULT 1,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (author_id) REFERENCES users(id)
        )`,

        // каскадное удаление — при удалении лекции её модули тоже уходят
        `CREATE TABLE IF NOT EXISTS lecture_modules (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            lecture_id INTEGER,
            module_id INTEGER,
            module_order INTEGER,
            FOREIGN KEY (lecture_id) REFERENCES lectures(id) ON DELETE CASCADE,
            FOREIGN KEY (module_id) REFERENCES modules(id)
        )`,

        `CREATE TABLE IF NOT EXISTS oauth_connections (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER,
            provider TEXT NOT NULL,
            provider_user_id TEXT NOT NULL,
            email TEXT,
            full_name TEXT,
            avatar_url TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
            UNIQUE(provider, provider_user_id)
        )`,

        `CREATE TABLE IF NOT EXISTS test_sessions (
            id TEXT PRIMARY KEY,
            lecture_id INTEGER NOT NULL,
            module_id INTEGER,
            title TEXT NOT NULL,
            duration INTEGER NOT NULL,
            started_at DATETIME NOT NULL,
            ended_at DATETIME,
            is_active BOOLEAN DEFAULT TRUE,
            config TEXT,
            questions TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (lecture_id) REFERENCES lectures(id),
            FOREIGN KEY (module_id) REFERENCES modules(id)
        )`,

        `CREATE TABLE IF NOT EXISTS student_test_results (
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
            answers TEXT NOT NULL,
            correctness TEXT NOT NULL,
            time_spent INTEGER NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (student_id) REFERENCES users(id),
            FOREIGN KEY (test_session_id) REFERENCES test_sessions(id),
            FOREIGN KEY (lecture_id) REFERENCES lectures(id)
        )`,

        `CREATE TABLE IF NOT EXISTS student_progress (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            student_id INTEGER NOT NULL,
            lecture_id INTEGER NOT NULL,
            module_id INTEGER NOT NULL,
            completed BOOLEAN DEFAULT FALSE,
            score REAL DEFAULT 0,
            started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            completed_at DATETIME,
            FOREIGN KEY (student_id) REFERENCES users(id),
            FOREIGN KEY (lecture_id) REFERENCES lectures(id),
            FOREIGN KEY (module_id) REFERENCES modules(id)
        )`,

        `CREATE TABLE IF NOT EXISTS active_sessions (
            session_id VARCHAR(50) PRIMARY KEY,
            lecture_id INTEGER NOT NULL,
            teacher_id INTEGER NOT NULL,
            session_name VARCHAR(255),
            join_code VARCHAR(10) UNIQUE NOT NULL,
            is_active BOOLEAN DEFAULT 1,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            ended_at DATETIME,
            FOREIGN KEY (lecture_id) REFERENCES lectures(id),
            FOREIGN KEY (teacher_id) REFERENCES users(id)
        )`,

        `CREATE TABLE IF NOT EXISTS session_students (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id VARCHAR(50) NOT NULL,
            student_id INTEGER NOT NULL,
            joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            left_at DATETIME,
            is_active BOOLEAN DEFAULT 1,
            FOREIGN KEY (session_id) REFERENCES active_sessions(session_id) ON DELETE CASCADE,
            FOREIGN KEY (student_id) REFERENCES users(id),
            UNIQUE(session_id, student_id)
        )`,

        `CREATE TABLE IF NOT EXISTS session_activity (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id VARCHAR(50) NOT NULL,
            student_id INTEGER NOT NULL,
            action_type VARCHAR(50),
            action_data TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (session_id) REFERENCES active_sessions(session_id),
            FOREIGN KEY (student_id) REFERENCES users(id)
        )`,

        `CREATE TABLE IF NOT EXISTS session_state (
            session_id VARCHAR(50) PRIMARY KEY,
            current_slide TEXT DEFAULT '{"slide_index": 0}',
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (session_id) REFERENCES active_sessions(session_id)
        )`,

        `CREATE TABLE IF NOT EXISTS completed_modules (
            session_id VARCHAR(50) NOT NULL,
            module_id INTEGER NOT NULL,
            completed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (session_id, module_id)
        )`,

        `CREATE TABLE IF NOT EXISTS session_answers (
            session_id VARCHAR(50) NOT NULL,
            student_name TEXT NOT NULL,
            module_id INTEGER NOT NULL,
            question_idx INTEGER NOT NULL,
            answer TEXT NOT NULL,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (session_id, student_name, module_id, question_idx)
        )`,

        `CREATE TABLE IF NOT EXISTS subjects (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT NOT NULL,
            description TEXT,
            invite_code TEXT UNIQUE NOT NULL,
            created_by INTEGER NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (created_by) REFERENCES users(id)
        )`,

        `CREATE TABLE IF NOT EXISTS subject_teachers (
            subject_id INTEGER NOT NULL,
            teacher_id INTEGER NOT NULL,
            PRIMARY KEY (subject_id, teacher_id),
            FOREIGN KEY (subject_id) REFERENCES subjects(id) ON DELETE CASCADE,
            FOREIGN KEY (teacher_id) REFERENCES users(id)
        )`,

        `CREATE TABLE IF NOT EXISTS subject_students (
            subject_id INTEGER NOT NULL,
            student_id INTEGER NOT NULL,
            joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (subject_id, student_id),
            FOREIGN KEY (subject_id) REFERENCES subjects(id) ON DELETE CASCADE,
            FOREIGN KEY (student_id) REFERENCES users(id)
        )`,

        `CREATE TABLE IF NOT EXISTS assignments (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            subject_id INTEGER NOT NULL,
            title TEXT NOT NULL,
            description TEXT,
            condition_file TEXT,
            deadline DATETIME,
            max_score INTEGER NOT NULL DEFAULT 100,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (subject_id) REFERENCES subjects(id) ON DELETE CASCADE
        )`,

        `CREATE TABLE IF NOT EXISTS submissions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            assignment_id INTEGER NOT NULL,
            student_id INTEGER NOT NULL,
            file_path TEXT NOT NULL,
            annotated_file_path TEXT,
            status TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted','reviewed')),
            teacher_comment TEXT,
            grade INTEGER,
            submitted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            reviewed_at DATETIME,
            FOREIGN KEY (assignment_id) REFERENCES assignments(id) ON DELETE CASCADE,
            FOREIGN KEY (student_id) REFERENCES users(id),
            UNIQUE (assignment_id, student_id)
        )`,

        `CREATE TABLE IF NOT EXISTS student_module_assignments (
            session_id VARCHAR(50) NOT NULL,
            student_id INTEGER NOT NULL,
            module_id INTEGER NOT NULL,
            questions_json TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (session_id, student_id, module_id),
            FOREIGN KEY (session_id) REFERENCES active_sessions(session_id) ON DELETE CASCADE,
            FOREIGN KEY (student_id) REFERENCES users(id),
            FOREIGN KEY (module_id) REFERENCES modules(id)
        )`,

        `CREATE TABLE IF NOT EXISTS email_verifications (
            email TEXT PRIMARY KEY,
            code TEXT NOT NULL,
            data_json TEXT NOT NULL,
            expires_at DATETIME NOT NULL
        )`,
    }

    for _, query := range queries {
        _, err := db.Exec(query)
        if err != nil {
            log.Printf("Warning executing query: %v\nQuery: %s", err, query)
        }
    }

    // миграции — добавляем колонки в существующие таблицы если их нет
    rows, err := db.Query(`
        SELECT COUNT(*) FROM pragma_table_info('student_progress')
        WHERE name='session_id'
    `)
    var count int
    if err == nil && rows.Next() {
        rows.Scan(&count)
        rows.Close()
        if count == 0 {
            _, err = db.Exec(`ALTER TABLE student_progress ADD COLUMN session_id TEXT`)
            if err != nil {
                log.Printf("Warning adding session_id column: %v", err)
            }
        }
    }

    maxScoreRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name='max_score'`)
    if err == nil {
        var maxScoreCount int
        if maxScoreRows.Next() {
            maxScoreRows.Scan(&maxScoreCount)
        }
        maxScoreRows.Close()
        if maxScoreCount == 0 {
            _, err = db.Exec(`ALTER TABLE assignments ADD COLUMN max_score INTEGER NOT NULL DEFAULT 100`)
            if err != nil {
                log.Printf("Warning adding max_score column: %v", err)
            }
        }
    }

    gradeRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('submissions') WHERE name='grade'`)
    if err == nil {
        var gradeCount int
        if gradeRows.Next() {
            gradeRows.Scan(&gradeCount)
        }
        gradeRows.Close()
        if gradeCount == 0 {
            _, err = db.Exec(`ALTER TABLE submissions ADD COLUMN grade INTEGER`)
            if err != nil {
                log.Printf("Warning adding grade column: %v", err)
            }
        }
    }

    pinsRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('submissions') WHERE name='pins_json'`)
    if err == nil {
        var pinsCount int
        if pinsRows.Next() {
            pinsRows.Scan(&pinsCount)
        }
        pinsRows.Close()
        if pinsCount == 0 {
            _, err = db.Exec(`ALTER TABLE submissions ADD COLUMN pins_json TEXT`)
            if err != nil {
                log.Printf("Warning adding pins_json column: %v", err)
            }
        }
    }

    annRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('submissions') WHERE name='annotations_json'`)
    if err == nil {
        var annCount int
        if annRows.Next() {
            annRows.Scan(&annCount)
        }
        annRows.Close()
        if annCount == 0 {
            _, err = db.Exec(`ALTER TABLE submissions ADD COLUMN annotations_json TEXT`)
            if err != nil {
                log.Printf("Warning adding annotations_json column: %v", err)
            }
        }
    }

    // strokes_json — мазки ручкой/маркером (pages payload) для повторного
    // редактирования: без него редактор открывался пустым и затирал разметку.
    strokeRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('submissions') WHERE name='strokes_json'`)
    if err == nil {
        var strokeCount int
        if strokeRows.Next() {
            strokeRows.Scan(&strokeCount)
        }
        strokeRows.Close()
        if strokeCount == 0 {
            _, err = db.Exec(`ALTER TABLE submissions ADD COLUMN strokes_json TEXT`)
            if err != nil {
                log.Printf("Warning adding strokes_json column: %v", err)
            }
        }
    }

    for _, col := range []struct{ name, def string }{
        {"university", "TEXT DEFAULT ''"},
        {"last_profile_edit", "DATETIME"},
        {"avatar_url", "TEXT DEFAULT ''"},
    } {
        rows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name=?`, col.name)
        if e == nil {
            var cnt int
            if rows.Next() {
                rows.Scan(&cnt)
            }
            rows.Close()
            if cnt == 0 {
                if _, e2 := db.Exec(`ALTER TABLE users ADD COLUMN ` + col.name + ` ` + col.def); e2 != nil {
                    log.Printf("Warning adding users.%s: %v", col.name, e2)
                }
            }
        }
    }

    qcRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('modules') WHERE name='questions_count'`)
    if err == nil {
        var qcCount int
        if qcRows.Next() {
            qcRows.Scan(&qcCount)
        }
        qcRows.Close()
        if qcCount == 0 {
            _, err = db.Exec(`ALTER TABLE modules ADD COLUMN questions_count INTEGER DEFAULT 0`)
            if err != nil {
                log.Printf("Warning adding questions_count column: %v", err)
            }
        }
    }

    seenRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('submissions') WHERE name='student_seen'`)
    if err == nil {
        var seenCount int
        if seenRows.Next() {
            seenRows.Scan(&seenCount)
        }
        seenRows.Close()
        if seenCount == 0 {
            _, err = db.Exec(`ALTER TABLE submissions ADD COLUMN student_seen INTEGER DEFAULT 0`)
            if err != nil {
                log.Printf("Warning adding student_seen column: %v", err)
            } else {
                // все уже проверенные работы помечаем как просмотренные — они были до внедрения бейджей
                db.Exec(`UPDATE submissions SET student_seen=1 WHERE status='reviewed'`)
            }
        }
    }

    strictRows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name='strict_deadline'`)
    if err == nil {
        var strictCount int
        if strictRows.Next() {
            strictRows.Scan(&strictCount)
        }
        strictRows.Close()
        if strictCount == 0 {
            _, err = db.Exec(`ALTER TABLE assignments ADD COLUMN strict_deadline INTEGER NOT NULL DEFAULT 0`)
            if err != nil {
                log.Printf("Warning adding strict_deadline column: %v", err)
            }
        }
    }

    for _, col := range []struct{ name, def string }{
        {"hide_tests", "INTEGER NOT NULL DEFAULT 0"},
    } {
        htRows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('lectures') WHERE name=?`, col.name)
        if e == nil {
            var cnt int
            if htRows.Next() {
                htRows.Scan(&cnt)
            }
            htRows.Close()
            if cnt == 0 {
                if _, e2 := db.Exec(`ALTER TABLE lectures ADD COLUMN ` + col.name + ` ` + col.def); e2 != nil {
                    log.Printf("Warning adding lectures.%s: %v", col.name, e2)
                }
            }
        }
    }

    for _, col := range []struct{ name, def string }{
        {"grading_mode", "TEXT NOT NULL DEFAULT 'total'"},
    } {
        gmRows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name=?`, col.name)
        if e == nil {
            var cnt int
            if gmRows.Next() { gmRows.Scan(&cnt) }
            gmRows.Close()
            if cnt == 0 {
                if _, e2 := db.Exec(`ALTER TABLE assignments ADD COLUMN ` + col.name + ` ` + col.def); e2 != nil {
                    log.Printf("Warning adding assignments.%s: %v", col.name, e2)
                }
            }
        }
    }

    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS assignment_variants (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        assignment_id INTEGER NOT NULL,
        variant_index INTEGER NOT NULL,
        file_path     TEXT NOT NULL,
        FOREIGN KEY (assignment_id) REFERENCES assignments(id) ON DELETE CASCADE
    )`); err != nil {
        log.Printf("Warning creating assignment_variants: %v", err)
    }

    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS student_variant_assignments (
        assignment_id INTEGER NOT NULL,
        student_id    INTEGER NOT NULL,
        variant_index INTEGER NOT NULL,
        file_path     TEXT NOT NULL,
        PRIMARY KEY (assignment_id, student_id),
        FOREIGN KEY (assignment_id) REFERENCES assignments(id) ON DELETE CASCADE
    )`); err != nil {
        log.Printf("Warning creating student_variant_assignments: %v", err)
    }

    for _, col := range []struct{ name, def string }{
        {"variant_distribution", "INTEGER NOT NULL DEFAULT 0"},
    } {
        vdRows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name=?`, col.name)
        if e == nil {
            var cnt int
            if vdRows.Next() { vdRows.Scan(&cnt) }
            vdRows.Close()
            if cnt == 0 {
                if _, e2 := db.Exec(`ALTER TABLE assignments ADD COLUMN ` + col.name + ` ` + col.def); e2 != nil {
                    log.Printf("Warning adding assignments.%s: %v", col.name, e2)
                }
            }
        }
    }

    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS assignment_tasks (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        assignment_id INTEGER NOT NULL,
        task_number   INTEGER NOT NULL,
        label         TEXT NOT NULL DEFAULT '',
        max_score     INTEGER NOT NULL DEFAULT 0,
        FOREIGN KEY (assignment_id) REFERENCES assignments(id) ON DELETE CASCADE
    )`); err != nil {
        log.Printf("Warning creating assignment_tasks: %v", err)
    }

    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS submission_task_grades (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        submission_id INTEGER NOT NULL,
        task_number   INTEGER NOT NULL,
        score         INTEGER NOT NULL DEFAULT 0,
        UNIQUE (submission_id, task_number),
        FOREIGN KEY (submission_id) REFERENCES submissions(id) ON DELETE CASCADE
    )`); err != nil {
        log.Printf("Warning creating submission_task_grades: %v", err)
    }

    aboutRows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='about'`)
    if e == nil {
        var cnt int
        if aboutRows.Next() {
            aboutRows.Scan(&cnt)
        }
        aboutRows.Close()
        if cnt == 0 {
            if _, e2 := db.Exec(`ALTER TABLE users ADD COLUMN about TEXT DEFAULT ''`); e2 != nil {
                log.Printf("Warning adding users.about: %v", e2)
            }
        }
    }

    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS lecture_comments (
        id         INTEGER PRIMARY KEY AUTOINCREMENT,
        lecture_id INTEGER NOT NULL,
        user_id    INTEGER NOT NULL,
        text       TEXT NOT NULL,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (lecture_id) REFERENCES lectures(id) ON DELETE CASCADE,
        FOREIGN KEY (user_id)    REFERENCES users(id)
    )`); err != nil {
        log.Printf("Warning creating lecture_comments table: %v", err)
    }

    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS submission_comments (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        submission_id INTEGER NOT NULL,
        user_id       INTEGER NOT NULL,
        user_type     TEXT NOT NULL,
        message       TEXT NOT NULL,
        created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (submission_id) REFERENCES submissions(id) ON DELETE CASCADE,
        FOREIGN KEY (user_id)       REFERENCES users(id)
    )`); err != nil {
        log.Printf("Warning creating submission_comments table: %v", err)
    }

    // Общая лента обсуждения предмета (видна всем участникам предмета)
    if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS subject_discussions (
        id         INTEGER PRIMARY KEY AUTOINCREMENT,
        subject_id INTEGER NOT NULL,
        user_id    INTEGER NOT NULL,
        user_type  TEXT NOT NULL,
        message    TEXT NOT NULL,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (subject_id) REFERENCES subjects(id) ON DELETE CASCADE,
        FOREIGN KEY (user_id)    REFERENCES users(id)
    )`); err != nil {
        log.Printf("Warning creating subject_discussions table: %v", err)
    }

    for _, col := range []struct{ name, def string }{
        {"allow_comments", "INTEGER NOT NULL DEFAULT 1"},
        {"publish_at", "DATETIME"},
    } {
        acRows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name=?`, col.name)
        if e == nil {
            var cnt int
            if acRows.Next() { acRows.Scan(&cnt) }
            acRows.Close()
            if cnt == 0 {
                if _, e2 := db.Exec(`ALTER TABLE assignments ADD COLUMN ` + col.name + ` ` + col.def); e2 != nil {
                    log.Printf("Warning adding assignments.%s: %v", col.name, e2)
                }
            }
        }
    }

    qmRows, e := db.Query(`SELECT COUNT(*) FROM pragma_table_info('modules') WHERE name='questions_mode'`)
    if e == nil {
        var cnt int
        if qmRows.Next() { qmRows.Scan(&cnt) }
        qmRows.Close()
        if cnt == 0 {
            if _, e2 := db.Exec(`ALTER TABLE modules ADD COLUMN questions_mode TEXT NOT NULL DEFAULT 'per_student'`); e2 != nil {
                log.Printf("Warning adding modules.questions_mode: %v", e2)
            }
        }
    }

    // Миграция: добавляем 'graph3d' в CHECK constraint таблицы modules
    var modulesSql string
    if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='modules'`).Scan(&modulesSql); err == nil {
        if !strings.Contains(modulesSql, "graph3d") {
            db.Exec(`PRAGMA foreign_keys=OFF`)
            tx, txErr := db.Begin()
            if txErr == nil {
                ok := true
                if _, e := tx.Exec(`CREATE TABLE modules_new (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    title TEXT NOT NULL,
                    course TEXT NOT NULL,
                    description TEXT,
                    module_type TEXT CHECK(module_type IN ('text', 'visual', 'question', 'test', 'graph3d')),
                    content TEXT,
                    author_id INTEGER,
                    published BOOLEAN DEFAULT 1,
                    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    questions_count INTEGER DEFAULT 0,
                    questions_mode TEXT NOT NULL DEFAULT 'per_student',
                    FOREIGN KEY (author_id) REFERENCES users(id)
                )`); e != nil {
                    log.Printf("Warning creating modules_new: %v", e)
                    tx.Rollback(); ok = false
                }
                if ok {
                    if _, e := tx.Exec(`INSERT INTO modules_new (id,title,course,description,module_type,content,author_id,published,created_at,questions_count,questions_mode)
                        SELECT id,title,course,description,module_type,content,author_id,published,created_at,
                        COALESCE(questions_count,0), COALESCE(questions_mode,'per_student') FROM modules`); e != nil {
                        log.Printf("Warning copying modules data: %v", e)
                        tx.Rollback(); ok = false
                    }
                }
                if ok {
                    if _, e := tx.Exec(`DROP TABLE modules`); e != nil {
                        log.Printf("Warning dropping modules: %v", e)
                        tx.Rollback(); ok = false
                    }
                }
                if ok {
                    if _, e := tx.Exec(`ALTER TABLE modules_new RENAME TO modules`); e != nil {
                        log.Printf("Warning renaming modules_new: %v", e)
                        tx.Rollback(); ok = false
                    }
                }
                if ok {
                    tx.Commit()
                    log.Println("Migrated modules table: CHECK constraint now includes graph3d")
                }
            }
            db.Exec(`PRAGMA foreign_keys=ON`)
        }
    }

    testQueries := []string{
        `INSERT OR IGNORE INTO courses (name) VALUES
            ('Математический анализ'),
            ('Линейная алгебра и аналитическая геометрия'),
            ('Дискретная математика'),
            ('Экономика'),
            ('Физика'),
            ('Дифференциальные уравнения'),
            ('Теория вероятностей')`,

        `INSERT OR IGNORE INTO users (login, password_hash, full_name, user_type, email) VALUES
            ('teacher', '$2a$10$N9qo8uLOickgx2ZMRZoMye3Zg7fZkF6gX3fYV7qG9t6UvQ6JQzW1a', 'Иванов И.И.', 'teacher', 'teacher@example.com')`,

        `INSERT OR IGNORE INTO users (login, password_hash, full_name, user_type, email, group_number) VALUES
            ('student', '$2a$10$N9qo8uLOickgx2ZMRZoMye3Zg7fZkF6gX3fYV7qG9t6UvQ6JQzW1a', 'Петров П.П.', 'student', 'student@example.com', 'ИУ6-32Б')`,

    }

    for _, query := range testQueries {
        _, err := db.Exec(query)
        if err != nil {
            log.Printf("Warning inserting test data: %v", err)
        }
    }

    indexQueries := []string{
        `CREATE INDEX IF NOT EXISTS idx_modules_author ON modules(author_id)`,
        `CREATE INDEX IF NOT EXISTS idx_lectures_author ON lectures(author_id)`,
        `CREATE INDEX IF NOT EXISTS idx_lecture_modules_lecture ON lecture_modules(lecture_id)`,
        `CREATE INDEX IF NOT EXISTS idx_lecture_modules_module ON lecture_modules(module_id)`,
        `CREATE INDEX IF NOT EXISTS idx_oauth_user ON oauth_connections(user_id)`,
        `CREATE INDEX IF NOT EXISTS idx_oauth_provider ON oauth_connections(provider, provider_user_id)`,
        `CREATE INDEX IF NOT EXISTS idx_test_session ON student_test_results(test_session_id)`,
        `CREATE INDEX IF NOT EXISTS idx_student_test ON student_test_results(student_id, lecture_id)`,
        `CREATE INDEX IF NOT EXISTS idx_lecture_test ON student_test_results(lecture_id, completed_at)`,
        `CREATE INDEX IF NOT EXISTS idx_progress_student ON student_progress(student_id)`,
        `CREATE INDEX IF NOT EXISTS idx_progress_lecture ON student_progress(lecture_id)`,
        `CREATE INDEX IF NOT EXISTS idx_active_sessions_teacher ON active_sessions(teacher_id)`,
        `CREATE INDEX IF NOT EXISTS idx_active_sessions_lecture ON active_sessions(lecture_id)`,
        `CREATE INDEX IF NOT EXISTS idx_session_students_session ON session_students(session_id)`,
        `CREATE INDEX IF NOT EXISTS idx_session_students_student ON session_students(student_id)`,
        `CREATE INDEX IF NOT EXISTS idx_subjects_created_by ON subjects(created_by)`,
        `CREATE INDEX IF NOT EXISTS idx_subject_teachers_teacher ON subject_teachers(teacher_id)`,
        `CREATE INDEX IF NOT EXISTS idx_subject_students_student ON subject_students(student_id)`,
        `CREATE INDEX IF NOT EXISTS idx_assignments_subject ON assignments(subject_id)`,
        `CREATE INDEX IF NOT EXISTS idx_submissions_assignment ON submissions(assignment_id)`,
        `CREATE INDEX IF NOT EXISTS idx_submissions_student ON submissions(student_id)`,
        `CREATE INDEX IF NOT EXISTS idx_subject_discussions_subject ON subject_discussions(subject_id)`,
    }

    for _, query := range indexQueries {
        _, err := db.Exec(query)
        if err != nil {
            log.Printf("Warning creating index: %v", err)
        }
    }

    SetupFTS(db)

    log.Println("✅ База данных инициализирована")
    return db
}

// SetupFTS поднимает полнотекстовый поиск (FTS5) по модулям и лекциям.
// modernc.org/sqlite собран с SQLITE_ENABLE_FTS5, так что отдельных зависимостей не нужно.
//
// Используем external-content таблицы (content='modules'/'lectures'): сам индекс
// не хранит копии текстов, а ссылается на исходные строки по rowid=id. Триггеры
// держат индекс в синхроне при вставке/обновлении/удалении. Любая ошибка здесь
// не фатальна — поиск просто откатится на LIKE (см. SearchHandler).
func SetupFTS(db *sql.DB) {
    stmts := []string{
        // --- модули ---
        `CREATE VIRTUAL TABLE IF NOT EXISTS modules_fts USING fts5(
            title, description, content,
            content='modules', content_rowid='id',
            tokenize='unicode61 remove_diacritics 2'
        )`,
        `CREATE TRIGGER IF NOT EXISTS modules_fts_ai AFTER INSERT ON modules BEGIN
            INSERT INTO modules_fts(rowid, title, description, content)
            VALUES (new.id, new.title, new.description, new.content);
        END`,
        `CREATE TRIGGER IF NOT EXISTS modules_fts_ad AFTER DELETE ON modules BEGIN
            INSERT INTO modules_fts(modules_fts, rowid, title, description, content)
            VALUES ('delete', old.id, old.title, old.description, old.content);
        END`,
        `CREATE TRIGGER IF NOT EXISTS modules_fts_au AFTER UPDATE ON modules BEGIN
            INSERT INTO modules_fts(modules_fts, rowid, title, description, content)
            VALUES ('delete', old.id, old.title, old.description, old.content);
            INSERT INTO modules_fts(rowid, title, description, content)
            VALUES (new.id, new.title, new.description, new.content);
        END`,
        // --- лекции ---
        `CREATE VIRTUAL TABLE IF NOT EXISTS lectures_fts USING fts5(
            title, description,
            content='lectures', content_rowid='id',
            tokenize='unicode61 remove_diacritics 2'
        )`,
        `CREATE TRIGGER IF NOT EXISTS lectures_fts_ai AFTER INSERT ON lectures BEGIN
            INSERT INTO lectures_fts(rowid, title, description)
            VALUES (new.id, new.title, new.description);
        END`,
        `CREATE TRIGGER IF NOT EXISTS lectures_fts_ad AFTER DELETE ON lectures BEGIN
            INSERT INTO lectures_fts(lectures_fts, rowid, title, description)
            VALUES ('delete', old.id, old.title, old.description);
        END`,
        `CREATE TRIGGER IF NOT EXISTS lectures_fts_au AFTER UPDATE ON lectures BEGIN
            INSERT INTO lectures_fts(lectures_fts, rowid, title, description)
            VALUES ('delete', old.id, old.title, old.description);
            INSERT INTO lectures_fts(rowid, title, description)
            VALUES (new.id, new.title, new.description);
        END`,
    }
    for _, s := range stmts {
        if _, err := db.Exec(s); err != nil {
            log.Printf("⚠️  FTS5 недоступен (поиск откатится на LIKE): %v", err)
            return
        }
    }

    // rebuild перечитывает контент из исходных таблиц — дёшево на нашем объёме
    // и чинит индекс, если таблица modules пересоздавалась миграцией graph3d.
    db.Exec(`INSERT INTO modules_fts(modules_fts) VALUES('rebuild')`)
    db.Exec(`INSERT INTO lectures_fts(lectures_fts) VALUES('rebuild')`)
    log.Println("🔎 FTS5: полнотекстовый поиск по модулям и лекциям готов")
}