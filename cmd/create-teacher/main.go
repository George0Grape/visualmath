package main

import (
    "database/sql"
    "flag"
    "fmt"
    "log"

    "golang.org/x/crypto/bcrypt"
    _ "modernc.org/sqlite"
)

func main() {
    dbPath := flag.String("db", "visualmath.db", "path to visualmath sqlite database")
    login := flag.String("login", "", "teacher login")
    email := flag.String("email", "", "teacher email")
    password := flag.String("password", "", "teacher password")
    name := flag.String("name", "Teacher", "teacher display name")
    flag.Parse()

    if *login == "" || *email == "" || *password == "" {
        log.Fatal("required: -login, -email, -password")
    }

    hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
    if err != nil {
        log.Fatal(err)
    }

    db, err := sql.Open("sqlite", *dbPath)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    _, err = db.Exec(`
        INSERT INTO users (login, password_hash, full_name, user_type, email, email_verified)
        VALUES (?, ?, ?, 'teacher', ?, 1)
        ON CONFLICT(login) DO UPDATE SET
            password_hash = excluded.password_hash,
            full_name = excluded.full_name,
            user_type = 'teacher',
            email = excluded.email,
            email_verified = 1
    `, *login, string(hash), *name, *email)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("teacher account ready: %s (%s)\n", *login, *email)
}
