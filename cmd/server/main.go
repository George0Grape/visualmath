package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"visualmath/internal/auth"
	"visualmath/internal/handlers"
	"visualmath/internal/storage"
	"visualmath/internal/sync"
)

func main() {
	fmt.Println("🚀 Запускаем VisualMath сервер...")

	godotenv.Load()

	// в продакшне бинарник лежит рядом с папкой web/,
	// при go run — берём текущую директорию
	basePath := "."
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(exeDir, "web", "templates")); err == nil {
			basePath = exeDir
		}
	}

	db := storage.InitSQLite()
	defer db.Close()

	if err := handlers.InitTemplates(basePath); err != nil {
		log.Fatalf("Ошибка загрузки шаблонов: %v", err)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		// На проде (COOKIE_SECURE=true) пустой секрет недопустим: с известным дефолтом
		// кто угодно подделает токены. Падаем, чтобы не подняться в небезопасном виде.
		if os.Getenv("COOKIE_SECURE") == "true" {
			log.Fatal("❌ JWT_SECRET не задан в production-режиме (COOKIE_SECURE=true) — отказ запуска")
		}
		jwtSecret = "dev-secret-change-me"
		log.Println("⚠️  JWT_SECRET не задан — используется небезопасное значение по умолчанию (только для локальной разработки)!")
	}

	if os.Getenv("SMTP_HOST") == "" || os.Getenv("SMTP_USER") == "" || os.Getenv("SMTP_PASS") == "" {
		log.Println("⚠️  SMTP не настроен (SMTP_HOST/SMTP_USER/SMTP_PASS) — письма с кодами подтверждения отправляться не будут!")
	}

	authHandler := &handlers.AuthHandler{DB: db, JWTSecret: jwtSecret, Limiter: handlers.NewLoginLimiter()}

	// Rate-лимитеры по IP против перебора/спама (помимо login-лимитера выше):
	registerLimiter := handlers.NewRateLimiter(10, time.Hour, time.Hour)          // 10 регистраций / час
	joinLimiter := handlers.NewRateLimiter(20, 10*time.Minute, 10*time.Minute)    // 20 попыток join-кода / 10 мин
	commentLimiter := handlers.NewRateLimiter(30, 5*time.Minute, 10*time.Minute)  // 30 комментариев / 5 мин

	socialHandler := handlers.NewSocialAuthHandler(db, jwtSecret)
	vkRedirectURL := os.Getenv("VK_REDIRECT_URL")
	if vkRedirectURL == "" {
		vkRedirectURL = "http://localhost:8080/auth/vk/callback"
	}
	yandexRedirectURL := os.Getenv("YANDEX_REDIRECT_URL")
	if yandexRedirectURL == "" {
		yandexRedirectURL = "http://localhost:8080/auth/yandex/callback"
	}
	if id := os.Getenv("VK_CLIENT_ID"); id != "" {
		socialHandler.Add("vk", &auth.VKProvider{Config: auth.OAuthConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("VK_CLIENT_SECRET"),
			RedirectURL:  vkRedirectURL,
		}})
	} else {
		log.Println("⚠️  VK_CLIENT_ID не задан — вход через ВКонтакте недоступен")
	}
	if id := os.Getenv("YANDEX_CLIENT_ID"); id != "" {
		socialHandler.Add("yandex", &auth.YandexProvider{Config: auth.OAuthConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("YANDEX_CLIENT_SECRET"),
			RedirectURL:  yandexRedirectURL,
		}})
	} else {
		log.Println("⚠️  YANDEX_CLIENT_ID не задан — вход через Яндекс недоступен")
	}
	moduleRepo := &storage.ModuleRepo{DB: db}
	lectureRepo := &storage.LectureRepo{DB: db}
	moduleHandler := &handlers.ModuleHandler{Repo: moduleRepo}
	lectureHandler := &handlers.LectureHandler{Repo: lectureRepo}
	studentViewHandler := &handlers.StudentViewHandler{DB: db}
	wsManager := sync.NewWebSocketManager(db, jwtSecret)
	sessionHandler := &handlers.SessionHandler{DB: db, WSM: wsManager}
	slidesHandler := &handlers.SlidesHandler{DB: db}
	pageHandler := &handlers.PageHandler{}
	dashHandler := &handlers.DashboardHandler{DB: db}
	classroomRepo := storage.NewClassroomRepo(db)
	classroomHandler := handlers.NewClassroomHandler(classroomRepo, basePath)
	profileHandler := &handlers.ProfileHandler{DB: db, JWTSecret: jwtSecret, BasePath: basePath}
	searchHandler := &handlers.SearchHandler{DB: db}

	startTime := time.Now()

	r := chi.NewRouter()
	r.Use(middleware.RequestID) // req-id попадает в логи и в сообщение о панике
	r.Use(middleware.Logger)
	r.Use(handlers.Recoverer)        // наш: пишет стек + отдаёт 500.html / JSON
	r.Use(handlers.SecurityHeaders)  // nosniff, X-Frame-Options, Referrer/Permissions
	r.Use(middleware.Compress(5))
	r.NotFound(pageHandler.NotFound)

	// WebSocket не throttle-им — соединения живут долго, лимит тут только навредит
	r.Get("/ws/session/{id}/teacher", wsManager.TeacherHandler)
	r.Get("/ws/session/{id}/student", wsManager.StudentHandler)
	r.Get("/ws/lecture/{id}/teacher", wsManager.TeacherHandler)
	r.Get("/ws/lecture/{id}/student", wsManager.StudentHandler)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Throttle(100))
		// клиент предупреждает при >80 МБ; здесь запас на multipart-обёртку,
		// чтобы ровно-80-МБ файл не упирался в лимит (nginx стоит 85M)
		r.Use(middleware.RequestSize(84 * 1024 * 1024))

		staticDir := filepath.Join(basePath, "web", "static")
		staticFS := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
		r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// JS/CSS меняются при каждом деплое — заставляем браузер ревалидировать
			// (FileServer отдаёт 304 если файл не изменился, поэтому это дёшево).
			// Картинки/шрифты неизменны — кэшируем надолго.
			switch {
			case strings.HasPrefix(r.URL.Path, "/static/vendor/"):
				// Вендоренные библиотеки (MathJax и пр.) неизменны — кэшируем
				// агрессивно, чтобы не ревалидировать 1+ МБ на каждой странице.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			case strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".css"):
				w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			default:
				w.Header().Set("Cache-Control", "public, max-age=86400")
			}
			// .webmanifest Go по умолчанию отдаёт как text/plain — поправляем.
			if strings.HasSuffix(r.URL.Path, ".webmanifest") {
				w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
			}
			staticFS.ServeHTTP(w, r)
		}))

		// Health-check для мониторинга/systemd: пингуем БД, отдаём uptime.
		// 503 при недоступной БД — балансировщик/проверка увидит «нездоров».
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			status := "ok"
			code := http.StatusOK
			if err := db.Ping(); err != nil {
				status = "degraded"
				code = http.StatusServiceUnavailable
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":         status,
				"uptime_seconds": int(time.Since(startTime).Seconds()),
				"time":           time.Now().UTC().Format(time.RFC3339),
			})
		})

		// Service worker должен лежать в корне, чтобы его scope покрывал весь
		// сайт (из /static/ scope был бы ограничен /static/). no-cache —
		// чтобы браузер быстро подхватывал обновлённый SW.
		r.Get("/sw.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Service-Worker-Allowed", "/")
			http.ServeFile(w, r, filepath.Join(staticDir, "sw.js"))
		})

		r.Get("/", pageHandler.Home)
		r.Get("/login", pageHandler.Login)
		r.Get("/register", pageHandler.Register)
		r.Get("/test", pageHandler.Test)
		r.Get("/test-start", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Test OK"))
		})

		r.With(handlers.RateLimit(registerLimiter)).Post("/api/register", authHandler.Register)
		r.Post("/api/verify-email", authHandler.VerifyEmail)
		r.Post("/api/login", authHandler.Login)

		r.Get("/auth/vk", socialHandler.Redirect)
		r.Get("/auth/vk/callback", socialHandler.Callback)
		r.Get("/auth/yandex", socialHandler.Redirect)
		r.Get("/auth/yandex/callback", socialHandler.Callback)

		r.Get("/student/join-lecture", studentViewHandler.JoinLecture)
		r.Get("/student-view/{id}", studentViewHandler.StudentLectureView)
		r.Get("/student/lecture/slides/{id}", slidesHandler.StudentSlidesPage)
		r.Get("/student/join", studentViewHandler.JoinLecture)
		r.Get("/student/session/{id}", sessionHandler.StudentSessionPage)

		r.Get("/api/sessions/{id}/has_tests", wsManager.HasTestsHandler)
		r.Get("/api/sessions/{id}/results_data", wsManager.ResultsDataAPIHandler)
		r.Get("/api/sessions/{id}/modules/{moduleId}/results.xlsx", wsManager.ResultsModuleXLSXHandler)
		r.Get("/api/sessions/{id}/state", wsManager.SessionStateHandler)

		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware(jwtSecret))

			r.With(handlers.RateLimit(joinLimiter)).Post("/api/sessions/join", sessionHandler.JoinSession)

			r.Get("/dashboard", dashHandler.Dashboard)
			r.Get("/graph3d", pageHandler.Graph3D)
			r.Get("/logout", pageHandler.Logout)
			r.Get("/profile", profileHandler.ProfilePage)
			r.Get("/api/profile", profileHandler.GetProfile)
			r.Put("/api/profile", profileHandler.UpdateProfile)
			r.Post("/api/profile/photo", profileHandler.UploadAvatar)
			r.Post("/api/modules/image", profileHandler.UploadModuleImage)
			r.Get("/api/search", searchHandler.Search)

			// маршруты только для преподавателей
			r.Group(func(r chi.Router) {
				r.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						claims, ok := auth.GetUserFromContext(r.Context())
						if !ok || claims.UserType != "teacher" {
							if strings.HasPrefix(r.URL.Path, "/api/") {
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusForbidden)
								w.Write([]byte(`{"success":false,"message":"Доступ запрещён"}`))
								return
							}
							http.Redirect(w, r, "/dashboard", http.StatusFound)
							return
						}
						next.ServeHTTP(w, r)
					})
				})

				r.Get("/modules", moduleHandler.ListModules)
				r.Get("/modules/create", moduleHandler.CreateModulePage)
				r.Get("/modules/view/{id}", moduleHandler.ViewModulePage)
				r.Get("/modules/edit/{id}", moduleHandler.EditModulePage)
				r.Get("/api/modules/list", moduleHandler.ListModulesAPI)
				r.Post("/api/modules", moduleHandler.CreateModule)
				r.Get("/api/modules/{id}", moduleHandler.GetModule)
				r.Put("/api/modules/{id}", moduleHandler.UpdateModule)
				r.Delete("/api/modules/{id}", moduleHandler.DeleteModule)

				r.Get("/lectures", lectureHandler.LecturesPage)
				r.Get("/lectures/create", lectureHandler.CreateLecturePage)
				r.Get("/lectures/edit/{id}", lectureHandler.EditLecturePage)
				r.Get("/lectures/view/{id}", lectureHandler.ViewLecturePage)
				r.Get("/lectures/launch", lectureHandler.LaunchLectureSelection)
				r.Get("/api/lectures", lectureHandler.ListLectures)
				r.Post("/api/lectures", lectureHandler.CreateLecture)
				r.Put("/api/lectures/{id}", lectureHandler.UpdateLecture)
				r.Delete("/api/lectures/{id}", lectureHandler.DeleteLecture)
				r.Get("/api/modules/available", lectureHandler.GetAvailableModules)
				r.Post("/api/lectures/start", lectureHandler.StartLecture)

				r.Get("/lectures/start/{id}", func(w http.ResponseWriter, r *http.Request) {
					lectureID := chi.URLParam(r, "id")
					sessionID := uuid.New().String()[:8]
					http.Redirect(w, r,
						"/teacher/lecture/slides/"+lectureID+"?session="+sessionID,
						http.StatusFound)
				})
			})

			// эти эндпоинты нужны и студенту, и преподавателю
			r.Get("/profile/{id}", profileHandler.PublicProfilePage)
			r.Get("/api/profile/{id}", profileHandler.GetPublicProfile)
			r.Get("/lectures/published", lectureHandler.PublishedLecturesPage)
			r.Get("/lectures/published/{id}", lectureHandler.PublishedLectureReaderPage)
			r.Get("/api/lectures/published", lectureHandler.PublishedLecturesAPI)
			r.Get("/api/modules/{id}/assign", moduleHandler.AssignModule)
			r.Get("/api/lectures/{id}", lectureHandler.GetLecture)
			r.Get("/api/lectures/{id}/comments", lectureHandler.GetCommentsAPI)
			r.With(handlers.RateLimit(commentLimiter)).Post("/api/lectures/{id}/comments", lectureHandler.PostCommentAPI)
			r.Post("/api/lectures/complete", lectureHandler.CompleteModule)
			r.Get("/api/lectures/progress", lectureHandler.GetStudentProgress)

			r.Get("/teacher/lecture/slides/{id}", slidesHandler.TeacherSlidesPage)
			r.Get("/student-view/{id}", studentViewHandler.StudentLectureView)

			r.Post("/api/sessions/start", sessionHandler.StartSession)
			r.Get("/api/sessions/active", sessionHandler.GetActiveSessions)
			r.Post("/api/sessions/{id}/end", sessionHandler.EndSession)
			r.Get("/api/sessions/{id}/students", sessionHandler.GetSessionStudents)
			r.Get("/api/sessions/{id}/info", sessionHandler.GetSessionInfo)
			r.Get("/teacher/session/{id}", sessionHandler.TeacherSessionPage)
			r.Post("/api/sessions/{id}/modules/{mid}/complete", wsManager.CompleteModuleHTTPHandler)
			r.Get("/api/sessions/{id}/completed_modules", wsManager.CompletedModulesHandler)
			r.Get("/teacher/sessions/{id}/results", sessionHandler.ResultsPage)

			r.Get("/classroom/subjects", classroomHandler.SubjectsPage)
			r.Get("/classroom/subjects/create", classroomHandler.CreateSubjectPage)
			r.Post("/classroom/subjects/create", classroomHandler.CreateSubjectSubmit)
			r.Get("/classroom/subjects/{id}", classroomHandler.SubjectPage)
			r.Get("/classroom/subjects/{id}/analytics", classroomHandler.SubjectAnalyticsPage)
			r.Get("/classroom/subjects/{id}/gradebook.xlsx", classroomHandler.SubjectGradebookXLSX)
			r.Delete("/classroom/subjects/{id}", classroomHandler.DeleteSubject)
			r.Get("/classroom/join", classroomHandler.JoinSubjectPage)
			r.With(handlers.RateLimit(joinLimiter)).Post("/classroom/join", classroomHandler.JoinSubjectSubmit)
			r.Get("/classroom/subjects/{id}/assignments/create", classroomHandler.CreateAssignmentPage)
			r.Post("/classroom/subjects/{id}/assignments/create", classroomHandler.CreateAssignmentSubmit)
			r.Get("/classroom/assignments/{id}", classroomHandler.AssignmentPage)
			r.Delete("/classroom/assignments/{id}", classroomHandler.DeleteAssignment)
			r.Get("/classroom/assignments/{id}/edit", classroomHandler.EditAssignmentPage)
			r.Post("/classroom/assignments/{id}/edit", classroomHandler.EditAssignmentSubmit)
			r.Get("/api/classroom/subjects/{id}/students", classroomHandler.GetSubjectStudents)
			r.Delete("/api/classroom/subjects/{id}/students/{studentId}", classroomHandler.RemoveStudentFromSubject)
			r.Post("/classroom/assignments/{id}/submit", classroomHandler.SubmitWork)
			r.Get("/classroom/submissions/{id}/download", classroomHandler.DownloadSubmission)
			r.Get("/classroom/submissions/{id}/annotated", classroomHandler.DownloadAnnotated)
			r.Get("/classroom/assignments/{id}/condition", classroomHandler.DownloadCondition)
			r.Get("/classroom/assignments/{id}/variant", classroomHandler.DownloadVariant)
			r.Get("/classroom/assignments/{id}/grades.xlsx", classroomHandler.GradesXLSX)
			r.Get("/classroom/submissions/{id}/annotate", classroomHandler.AnnotatePage)
			r.Post("/classroom/submissions/{id}/annotate", classroomHandler.SaveAnnotations)
			r.Post("/classroom/submissions/{id}/grade", classroomHandler.GradeSubmission)
			r.Get("/classroom/submissions/{id}/pdf", classroomHandler.ServeSubmissionPDF)
			r.Get("/classroom/submissions/{id}/pins", classroomHandler.GetSubmissionPins)
			r.Get("/classroom/submissions/{id}/view", classroomHandler.SubmissionView)
			r.Get("/classroom/submissions/{id}/comments", classroomHandler.GetComments)
			r.With(handlers.RateLimit(commentLimiter)).Post("/classroom/submissions/{id}/comments", classroomHandler.PostComment)
			r.Delete("/classroom/submissions/{id}", classroomHandler.WithdrawSubmission)
			r.Post("/api/classroom/subjects/{id}/rename", classroomHandler.UpdateSubjectInline)
			r.Get("/api/classroom/subjects/{id}/my-files", classroomHandler.StudentFilesAPI)
			r.Get("/api/classroom/subjects/{id}/discussion", classroomHandler.GetDiscussion)
			r.With(handlers.RateLimit(commentLimiter)).Post("/api/classroom/subjects/{id}/discussion", classroomHandler.PostDiscussion)
			r.Delete("/api/classroom/discussion/{postId}", classroomHandler.DeleteDiscussionPost)
			r.Get("/api/notifications/badges", classroomHandler.BadgesAPI)
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// очищаем протухшие записи email_verifications раз в час
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			db.Exec(`DELETE FROM email_verifications WHERE expires_at < datetime('now')`)
		}
	}()

	go func() {
		fmt.Printf("✅ Сервер запущен на http://localhost:%s\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Ошибка сервера: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("⏳ Завершаем работу сервера...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Принудительное завершение: %v", err)
	}

	log.Println("✅ Сервер остановлен")
}
