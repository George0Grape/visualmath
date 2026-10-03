package sync

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
)

type QInfo struct {
	ModuleID    int
	QIdx        int
	Question    string
	CorrectText string
	CorrectIdxs string // comma-separated, e.g. "0" or "0,2"
	AnswerCount int
}

type StudentResult struct {
	Name   string
	Scores []float64
}

// moduleQuestion — вопрос модуля с правильным ответом
type moduleQuestion struct {
	Question    string
	Answers     []string
	CorrectIdxs []int // один или несколько правильных индексов
}

// QuestionResult — результат по одному вопросу для студента
type QuestionResult struct {
	Question       string   `json:"question"`
	Answers        []string `json:"answers"`
	YourAnswerIdxs []int    `json:"your_answer_idxs"` // пусто если нет ответа
	CorrectIdxs    []int    `json:"correct_idxs"`
	IsCorrect      bool     `json:"is_correct"`
	Points         float64  `json:"points"`
}

// Таймауты WebSocket: pingPeriod держим меньше pongWait и меньше typical nginx idle (60s),
// чтобы прокси не разрывал соединение раньше, чем мы узнаем об этом через pong.
const (
	pongWait   = 30 * time.Second
	pingPeriod = 25 * time.Second
	writeWait  = 10 * time.Second

	// Лимит подключений одного студента к одной сессии. Защищает от утечки:
	// открытые вкладки × flaky сеть могут быстро накопить зомби-соединения.
	maxConnsPerStudent = 3
)

// isSameStudent — два подключения принадлежат одному студенту.
// По ID если хотя бы один >0 (авторизован), иначе по имени (оба гости).
func isSameStudent(a, b *StudentConn) bool {
	if a.ID > 0 || b.ID > 0 {
		return a.ID == b.ID
	}
	return a.Name != "" && a.Name == b.Name
}

// safeConn сериализует записи в один *websocket.Conn — gorilla/websocket требует одного писателя.
// Чтение из conn потокобезопасно без мьютекса, поэтому ReadMessage — прямой passthrough.
type safeConn struct {
	conn *websocket.Conn
	wmu  sync.Mutex
}

func newSafeConn(c *websocket.Conn) *safeConn {
	return &safeConn{conn: c}
}

func (s *safeConn) WriteMessage(msgType int, data []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	// дедлайн на каждую запись: медленный клиент не должен блокировать broadcast всем остальным
	s.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return s.conn.WriteMessage(msgType, data)
}

func (s *safeConn) ReadMessage() (int, []byte, error) {
	return s.conn.ReadMessage()
}

func (s *safeConn) Close() error {
	return s.conn.Close()
}

// Poll — временный опрос внутри лекции (не сохраняется в БД)
type Poll struct {
	ID           string
	Question     string
	Options      []string
	Votes        []int
	StudentVotes map[string]int // studentName -> optionIndex
	Active       bool
	IsModule     bool // true = заранее созданный модуль, слайд уже есть у студентов
	Position     int  // позиция слайда (1-based), 0 для module polls
	Results      map[string]interface{} // итоговый payload poll_results (nil если ещё не завершён)
	mu           sync.Mutex
}

// answerEntry — одна запись ответа для батч-записи в БД.
type answerEntry struct {
	SessionID   string
	StudentName string
	ModuleID    int
	QuestionIdx int
	Answer      string
}

type WebSocketManager struct {
	DB               *sql.DB
	JWTSecret        string
	upgrader         websocket.Upgrader
	mu               sync.RWMutex
	teacherConns     map[string]*safeConn
	studentConns     map[string][]*StudentConn
	testAnswers      map[string]map[string]*StudentAnswers
	completedModules map[string]map[int]bool // sessionID -> moduleID -> true
	activePolls      map[string]*Poll              // sessionID -> текущий активный опрос
	sessionPolls     map[string]map[string]*Poll   // sessionID -> pollID -> все опросы сессии

	// answerWriter: буферизованная запись ответов в БД
	answerCh   chan answerEntry
	flushDone  chan struct{} // закрывается при завершении writer goroutine
}

type StudentAnswers struct {
	Name    string
	Answers map[int]map[int]string
}

type StudentConn struct {
	Conn *safeConn
	ID   int
	Name string
}

func NewWebSocketManager(db *sql.DB, jwtSecret string) *WebSocketManager {
	wm := &WebSocketManager{
		DB:        db,
		JWTSecret: jwtSecret,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		teacherConns:     make(map[string]*safeConn),
		studentConns:     make(map[string][]*StudentConn),
		testAnswers:      make(map[string]map[string]*StudentAnswers),
		completedModules: make(map[string]map[int]bool),
		activePolls:      make(map[string]*Poll),
		sessionPolls:     make(map[string]map[string]*Poll),
		answerCh:         make(chan answerEntry, 1024),
		flushDone:        make(chan struct{}),
	}
	go wm.answerWriter()
	return wm
}

// answerWriter — фоновая горутина, которая батчами пишет ответы студентов в БД.
// Собирает записи из канала answerCh каждые 500ms или при накоплении 100 штук,
// и пишет их одной транзакцией — вместо 50 отдельных INSERT за 1 захват writer-lock.
func (wm *WebSocketManager) answerWriter() {
	defer close(wm.flushDone)

	const (
		flushInterval = 500 * time.Millisecond
		maxBatch      = 100
	)

	buf := make([]answerEntry, 0, maxBatch)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-wm.answerCh:
			if !ok {
				// канал закрыт — flush остатков и выход
				if len(buf) > 0 {
					wm.flushAnswers(buf)
				}
				return
			}
			buf = append(buf, entry)
			if len(buf) >= maxBatch {
				wm.flushAnswers(buf)
				buf = buf[:0]
			}
		case <-ticker.C:
			if len(buf) > 0 {
				wm.flushAnswers(buf)
				buf = buf[:0]
			}
		}
	}
}

// flushAnswers пишет пачку ответов в БД одной транзакцией.
func (wm *WebSocketManager) flushAnswers(batch []answerEntry) {
	tx, err := wm.DB.Begin()
	if err != nil {
		log.Printf("❌ answerWriter: begin tx: %v", err)
		return
	}
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO session_answers
			(session_id, student_name, module_id, question_idx, answer, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		log.Printf("❌ answerWriter: prepare: %v", err)
		return
	}
	defer stmt.Close()

	now := time.Now()
	for _, e := range batch {
		if _, err := stmt.Exec(e.SessionID, e.StudentName, e.ModuleID, e.QuestionIdx, e.Answer, now); err != nil {
			log.Printf("⚠️ answerWriter: exec row: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("❌ answerWriter: commit: %v", err)
		return
	}
	log.Printf("💾 answerWriter: flushed %d answers to DB", len(batch))
}

// Close останавливает фоновую запись. Вызывается при graceful shutdown.
func (wm *WebSocketManager) Close() {
	close(wm.answerCh)
	<-wm.flushDone // ждём flush остатков
}

// LoadAnswersFromDB восстанавливает ответы из БД в память для указанной сессии.
// Вызывается при подключении первого участника к сессии после рестарта сервера.
func (wm *WebSocketManager) LoadAnswersFromDB(sessionID string) {
	wm.mu.RLock()
	_, alreadyLoaded := wm.testAnswers[sessionID]
	wm.mu.RUnlock()
	if alreadyLoaded {
		return
	}

	rows, err := wm.DB.Query(
		`SELECT student_name, module_id, question_idx, answer FROM session_answers WHERE session_id = ?`,
		sessionID,
	)
	if err != nil {
		log.Printf("⚠️ LoadAnswersFromDB(%s): %v", sessionID, err)
		return
	}
	defer rows.Close()

	count := 0
	wm.mu.Lock()
	if wm.testAnswers[sessionID] == nil {
		wm.testAnswers[sessionID] = make(map[string]*StudentAnswers)
	}
	for rows.Next() {
		var name, answer string
		var moduleID, qIdx int
		if err := rows.Scan(&name, &moduleID, &qIdx, &answer); err != nil {
			continue
		}
		sa := wm.testAnswers[sessionID][name]
		if sa == nil {
			sa = &StudentAnswers{Name: name, Answers: make(map[int]map[int]string)}
			wm.testAnswers[sessionID][name] = sa
		}
		if sa.Answers[moduleID] == nil {
			sa.Answers[moduleID] = make(map[int]string)
		}
		sa.Answers[moduleID][qIdx] = answer
		count++
	}
	wm.mu.Unlock()

	if count > 0 {
		log.Printf("♻️ LoadAnswersFromDB(%s): restored %d answers", sessionID, count)
	}
}

// parseWSToken проверяет JWT перед апгрейдом WebSocket соединения.
// Сначала смотрит ?token= (localStorage браузера), потом куку как запасной вариант.
// Возвращает подписанные claims — не доверяем user_id из cookie в StudentHandler.
func (wm *WebSocketManager) parseWSToken(r *http.Request) (userType string, userID int, fullName string, ok bool) {
	if wm.JWTSecret == "" {
		return "", 0, "", false
	}

	raw := r.URL.Query().Get("token")
	if raw == "" {
		if cookie, err := r.Cookie("token"); err == nil {
			raw = cookie.Value
		}
	}
	if raw == "" {
		return "", 0, "", false
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(decoded, claims, func(t *jwt.Token) (interface{}, error) {
		// явная проверка алгоритма: иначе атакующий может пробовать alg=none / RS256 confusion
		if _, isHMAC := t.Method.(*jwt.SigningMethodHMAC); !isHMAC {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(wm.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return "", 0, "", false
	}
	userType, _ = claims["user_type"].(string)
	if uidF, ok := claims["user_id"].(float64); ok {
		userID = int(uidF)
	}
	fullName, _ = claims["full_name"].(string)
	return userType, userID, fullName, true
}

// TeacherHandler — WebSocket для преподавателя
func (wm *WebSocketManager) TeacherHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("🔥 panic in TeacherHandler: %v", rec)
		}
	}()
	sessionID := chi.URLParam(r, "id")

	userType, _, _, ok := wm.parseWSToken(r)
	if !ok || userType != "teacher" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	log.Printf("🟢 Teacher connecting to session: %s", sessionID)

	// восстанавливаем ответы из БД если сервер перезапускался
	wm.LoadAnswersFromDB(sessionID)

	conn, err := wm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Failed to upgrade teacher connection: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 * 1024)

	wm.mu.Lock()
	wm.teacherConns[sessionID] = newSafeConn(conn)
	wm.mu.Unlock()

	defer func() {
		wm.mu.Lock()
		delete(wm.teacherConns, sessionID)
		wm.mu.Unlock()
		log.Printf("🔴 Teacher disconnected from session: %s", sessionID)
	}()

	log.Printf("✅ Teacher connected to session: %s", sessionID)
	// при переподключении препод должен увидеть все опросы, которые уже были
	wm.sendActivePollToTeacher(sessionID)

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Teacher disconnected from session %s: %v", sessionID, err)
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("❌ Failed to parse teacher message: %v", err)
			continue
		}

		if msgType, ok := msg["type"].(string); ok {
			switch msgType {
			case "ping":
				wm.teacherConns[sessionID].WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
				continue
			case "slide_changed":
				wm.saveCurrentSlide(sessionID, msg)
				wm.broadcastToStudents(sessionID, messageType, message)
			case "create_poll":
				wm.handleCreatePoll(sessionID, msg)
			case "init_module_poll":
				wm.handleInitModulePoll(sessionID, msg)
			case "end_poll":
				wm.handleEndPoll(sessionID, msg)
			default:
				wm.broadcastToStudents(sessionID, messageType, message)
			}
		}
	}
}

// StudentHandler — WebSocket для студента
func (wm *WebSocketManager) StudentHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("🔥 panic in StudentHandler: %v", rec)
		}
	}()
	sessionID := chi.URLParam(r, "id")

	userType, tokenUserID, tokenFullName, ok := wm.parseWSToken(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if userType != "student" {
		http.Error(w, "Forbidden: students only", http.StatusForbidden)
		return
	}

	log.Printf("🔵 Student attempting to connect to session: %s", sessionID)

	// восстанавливаем ответы из БД если сервер перезапускался
	wm.LoadAnswersFromDB(sessionID)

	conn, err := wm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Failed to upgrade student connection: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 * 1024)

	// user_id и full_name берём из подписанного JWT, а не из cookie:
	// cookie можно подделать через DevTools → атакующий бы выкинул чужого студента через eviction.
	studentID := tokenUserID
	if studentID == 0 {
		studentID = -int(time.Now().UnixNano() % 1000000)
	}
	studentName := tokenFullName
	if studentName == "" {
		studentName = "Студент"
	}

	var lectureID int
	var isActive bool
	err = wm.DB.QueryRow(`
        SELECT lecture_id, is_active FROM active_sessions WHERE session_id = ?
    `, sessionID).Scan(&lectureID, &isActive)

	if err != nil {
		log.Printf("⚠️ Session not found in DB: %s — rejecting student", sessionID)
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"lecture_end","payload":{"message":"Лекция не найдена"}}`))
		return
	} else if !isActive {
		log.Printf("⚠️ Session inactive: %s — rejecting student", sessionID)
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"lecture_end","payload":{"message":"Лекция уже завершена"}}`))
		return
	}

	log.Printf("✅ Student connecting to valid session %s (lecture %d)", sessionID, lectureID)

	safe := newSafeConn(conn)
	studentConn := &StudentConn{
		Conn: safe,
		ID:   studentID,
		Name: studentName,
	}

	wm.mu.Lock()
	wm.studentConns[sessionID] = append(wm.studentConns[sessionID], studentConn)
	// Лимит: идём от новых к старым, считаем совпадения по студенту;
	// всё, что сверх maxConnsPerStudent — отправляем в evicted для закрытия вне локa.
	var evicted []*StudentConn
	conns := wm.studentConns[sessionID]
	matches := 0
	for i := len(conns) - 1; i >= 0; i-- {
		if !isSameStudent(conns[i], studentConn) {
			continue
		}
		matches++
		if matches > maxConnsPerStudent {
			evicted = append(evicted, conns[i])
			conns = append(conns[:i], conns[i+1:]...)
		}
	}
	wm.studentConns[sessionID] = conns
	studentCount := len(conns)
	wm.mu.Unlock()

	for _, ev := range evicted {
		log.Printf("♻️ Evicting stale conn for student %q in session %s (over limit)", ev.Name, sessionID)
		ev.Conn.WriteMessage(websocket.TextMessage,
			[]byte(`{"type":"session_replaced","payload":{"message":"Открыто новое подключение, старое закрыто"}}`))
		ev.Conn.Close()
	}

	wm.notifyTeacher(sessionID, "student_connected", map[string]interface{}{
		"id":    studentID,
		"name":  studentName,
		"count": studentCount,
	})

	log.Printf("👤 Student %s connected to session: %s (total: %d)", studentName, sessionID, studentCount)

	wm.sendActivePoll(sessionID, safe)
	wm.sendCurrentSlide(sessionID, safe)
	wm.sendCompletedModulesState(sessionID, studentName, studentID, safe)

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	defer func() {
		wm.mu.Lock()
		for i, sc := range wm.studentConns[sessionID] {
			if sc.Conn == safe {
				wm.studentConns[sessionID] = append(wm.studentConns[sessionID][:i], wm.studentConns[sessionID][i+1:]...)
				break
			}
		}
		studentCount := len(wm.studentConns[sessionID])
		wm.mu.Unlock()

		wm.notifyTeacher(sessionID, "student_disconnected", map[string]interface{}{
			"id":    studentID,
			"name":  studentName,
			"count": studentCount,
		})

		log.Printf("🔴 Student %s disconnected from session: %s (remaining: %d)", studentName, sessionID, studentCount)
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Student %s disconnected: %v", studentName, err)
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("❌ Failed to parse student message: %v", err)
			continue
		}

		if msgType, ok := msg["type"].(string); ok {
			switch msgType {
			case "ping":
				safe.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
				continue
			case "slide_changed":
				wm.notifyTeacher(sessionID, "student_slide_change", map[string]interface{}{
					"student_id":   studentID,
					"student_name": studentName,
					"slide_index":  msg["payload"],
				})
			case "poll_vote":
				wm.handlePollVote(sessionID, studentName, msg)
			case "test_answer":
				// comma-ok на КАЖДОМ приведении: кривой payload от клиента не должен
				// ронять соединение паникой на unchecked type assertion
				payloadMap, _ := msg["payload"].(map[string]interface{})
				moduleIDf, _ := payloadMap["module_id"].(float64)
				moduleID := int(moduleIDf)
				wm.saveTestAnswer(sessionID, studentName, msg)
				score := wm.calcStudentScoreByID(sessionID, studentID, studentName)
				scoreMsg, _ := json.Marshal(map[string]interface{}{
					"type":    "test_score",
					"payload": score,
				})
				safe.WriteMessage(websocket.TextMessage, scoreMsg)
				// если преподаватель завершил модуль пока студент отвечал — отправляем результат сразу
				wm.mu.RLock()
				alreadyDone := wm.completedModules[sessionID] != nil && wm.completedModules[sessionID][moduleID]
				wm.mu.RUnlock()
				if alreadyDone {
					wm.mu.RLock()
					sa := wm.testAnswers[sessionID][studentName]
					wm.mu.RUnlock()
					qs := wm.getPersonalizedModuleQuestions(sessionID, studentID, moduleID)
					result := buildModuleResult(moduleID, qs, sa)
					if msgData, err := json.Marshal(map[string]interface{}{"type": "module_completed", "payload": result}); err == nil {
						safe.WriteMessage(websocket.TextMessage, msgData)
					}
				}
			}
		}

		wm.forwardToTeacher(sessionID, messageType, message)
	}
}

// broadcastToStudents отправляет сообщение всем подключённым студентам параллельно.
// Параллельность критична: запись в медленного клиента может висеть до writeWait,
// и при последовательной рассылке один плохой интернет задерживал бы слайд у всей аудитории.
func (wm *WebSocketManager) broadcastToStudents(sessionID string, messageType int, message []byte) {
	wm.mu.RLock()
	src := wm.studentConns[sessionID]
	students := make([]*StudentConn, len(src)) // копия указателей — дальше без лока менеджера
	copy(students, src)
	wm.mu.RUnlock()

	if len(students) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(students))
	for _, student := range students {
		go func(s *StudentConn) {
			defer wg.Done()
			if err := s.Conn.WriteMessage(messageType, message); err != nil {
				log.Printf("❌ broadcast to student %s (session %s): %v", s.Name, sessionID, err)
			}
		}(student)
	}
	wg.Wait()
}

// notifyTeacher шлёт событие преподавателю, если он подключён
func (wm *WebSocketManager) notifyTeacher(sessionID string, msgType string, payload interface{}) {
	wm.mu.RLock()
	teacherConn, ok := wm.teacherConns[sessionID]
	wm.mu.RUnlock()

	if !ok || teacherConn == nil {
		log.Printf("⚠️ No teacher connected to session %s", sessionID)
		return
	}

	msg := map[string]interface{}{
		"type":    msgType,
		"payload": payload,
	}

	message, err := json.Marshal(msg)
	if err != nil {
		log.Printf("❌ Failed to marshal teacher notification: %v", err)
		return
	}

	if err := teacherConn.WriteMessage(websocket.TextMessage, message); err != nil {
		log.Printf("❌ Failed to notify teacher in session %s: %v", sessionID, err)
	}
}

// broadcastAll отправляет сообщение всем: и преподавателю, и студентам
func (wm *WebSocketManager) broadcastAll(sessionID string, data interface{}) {
	msg, err := json.Marshal(data)
	if err != nil {
		return
	}
	wm.mu.RLock()
	tc := wm.teacherConns[sessionID]
	src := wm.studentConns[sessionID]
	students := make([]*StudentConn, len(src))
	copy(students, src)
	wm.mu.RUnlock()
	if tc != nil {
		tc.WriteMessage(websocket.TextMessage, msg)
	}
	var wg sync.WaitGroup
	wg.Add(len(students))
	for _, sc := range students {
		go func(s *StudentConn) {
			defer wg.Done()
			s.Conn.WriteMessage(websocket.TextMessage, msg)
		}(sc)
	}
	wg.Wait()
}

// handleCreatePoll создаёт новый опрос и сразу рассылает его всем
func (wm *WebSocketManager) handleCreatePoll(sessionID string, msg map[string]interface{}) {
	payload, _ := msg["payload"].(map[string]interface{})
	if payload == nil {
		return
	}
	question, _ := payload["question"].(string)
	rawOptions, _ := payload["options"].([]interface{})
	positionF, _ := payload["position"].(float64)
	position := int(positionF)

	options := make([]string, 0, len(rawOptions))
	for _, o := range rawOptions {
		if s, ok := o.(string); ok && s != "" {
			options = append(options, s)
		}
	}
	if question == "" || len(options) < 2 {
		return
	}

	pollID := fmt.Sprintf("poll_%d", time.Now().UnixNano())
	poll := &Poll{
		ID:           pollID,
		Question:     question,
		Options:      options,
		Votes:        make([]int, len(options)),
		StudentVotes: make(map[string]int),
		Active:       true,
		Position:     position,
	}

	wm.mu.Lock()
	wm.activePolls[sessionID] = poll
	if wm.sessionPolls[sessionID] == nil {
		wm.sessionPolls[sessionID] = make(map[string]*Poll)
	}
	wm.sessionPolls[sessionID][pollID] = poll
	wm.mu.Unlock()

	wm.broadcastAll(sessionID, map[string]interface{}{
		"type": "poll_inserted",
		"payload": map[string]interface{}{
			"poll_id":  pollID,
			"question": question,
			"options":  options,
			"position": position,
		},
	})
	log.Printf("📊 Poll created in session %s: %s (pos %d)", sessionID, question, position)
}

// handleInitModulePoll регистрирует question-модуль как опрос без broadcast —
// слайд уже есть у студентов, просто нужно запомнить для статистики
func (wm *WebSocketManager) handleInitModulePoll(sessionID string, msg map[string]interface{}) {
	payload, _ := msg["payload"].(map[string]interface{})
	if payload == nil {
		return
	}
	pollID, _ := payload["poll_id"].(string)
	question, _ := payload["question"].(string)
	optRaw, _ := payload["options"].([]interface{})
	if pollID == "" || question == "" || len(optRaw) < 2 {
		return
	}
	options := make([]string, len(optRaw))
	for i, o := range optRaw {
		options[i], _ = o.(string)
	}

	wm.mu.Lock()
	if wm.sessionPolls[sessionID] == nil {
		wm.sessionPolls[sessionID] = make(map[string]*Poll)
	}
	// не перезаписываем голоса если препод переподключился
	if wm.sessionPolls[sessionID][pollID] == nil {
		wm.sessionPolls[sessionID][pollID] = &Poll{
			ID:           pollID,
			Question:     question,
			Options:      options,
			Votes:        make([]int, len(options)),
			StudentVotes: make(map[string]int),
			Active:       true,
			IsModule:     true,
		}
	}
	wm.mu.Unlock()
	log.Printf("📋 Module poll registered in session %s: %s", sessionID, pollID)
}

// handlePollVote засчитывает голос студента, предыдущий голос автоматически отменяется
func (wm *WebSocketManager) handlePollVote(sessionID string, studentName string, msg map[string]interface{}) {
	payload, _ := msg["payload"].(map[string]interface{})
	if payload == nil {
		return
	}
	pollID, _ := payload["poll_id"].(string)
	optIdxF, _ := payload["option_index"].(float64)
	optIdx := int(optIdxF)

	// activePolls — текущий обычный опрос; sessionPolls — question-модули
	wm.mu.RLock()
	poll := wm.activePolls[sessionID]
	if poll == nil || poll.ID != pollID {
		if wm.sessionPolls[sessionID] != nil {
			poll = wm.sessionPolls[sessionID][pollID]
		} else {
			poll = nil
		}
	}
	wm.mu.RUnlock()

	if poll == nil || poll.ID != pollID || !poll.Active {
		return
	}
	poll.mu.Lock()
	if optIdx >= 0 && optIdx < len(poll.Votes) {
		// студент передумал — убираем предыдущий голос
		if prev, voted := poll.StudentVotes[studentName]; voted && prev >= 0 && prev < len(poll.Votes) {
			poll.Votes[prev]--
		}
		poll.Votes[optIdx]++
		poll.StudentVotes[studentName] = optIdx
	}
	poll.mu.Unlock()
}

// handleEndPoll завершает опрос и рассылает итоги всем участникам
func (wm *WebSocketManager) handleEndPoll(sessionID string, msg map[string]interface{}) {
	payload, _ := msg["payload"].(map[string]interface{})
	var pollID string
	if payload != nil {
		pollID, _ = payload["poll_id"].(string)
	}

	wm.mu.Lock()
	poll := wm.activePolls[sessionID]
	if poll != nil && (pollID == "" || poll.ID == pollID) {
		poll.Active = false
		delete(wm.activePolls, sessionID)
	} else {
		// это question-модуль, а не обычный опрос
		poll = nil
		if pollID != "" && wm.sessionPolls[sessionID] != nil {
			if p, ok := wm.sessionPolls[sessionID][pollID]; ok {
				p.Active = false
				poll = p
			}
		}
	}
	wm.mu.Unlock()

	if poll == nil {
		return
	}

	poll.mu.Lock()
	total := 0
	for _, v := range poll.Votes {
		total += v
	}
	percentages := make([]float64, len(poll.Votes))
	for i, v := range poll.Votes {
		if total > 0 {
			percentages[i] = math.Round(float64(v)/float64(total)*100*10) / 10
		}
	}
	votes := make([]int, len(poll.Votes))
	copy(votes, poll.Votes)
	resultsPayload := map[string]interface{}{
		"poll_id":     poll.ID,
		"question":    poll.Question,
		"options":     poll.Options,
		"votes":       votes,
		"percentages": percentages,
		"total":       total,
	}
	poll.Results = resultsPayload
	poll.mu.Unlock()

	wm.broadcastAll(sessionID, map[string]interface{}{
		"type":    "poll_results",
		"payload": resultsPayload,
	})
	log.Printf("📊 Poll ended in session %s, total votes: %d", sessionID, total)
}

// sendAllPolls восстанавливает состояние всех опросов при подключении:
// шлёт poll_inserted для каждого, а если опрос уже завершён — ещё и poll_results
func (wm *WebSocketManager) sendAllPolls(sessionID string, send func([]byte)) {
	wm.mu.RLock()
	polls := wm.sessionPolls[sessionID]
	wm.mu.RUnlock()
	for _, poll := range polls {
		poll.mu.Lock()
		insertedMsg, _ := json.Marshal(map[string]interface{}{
			"type": "poll_inserted",
			"payload": map[string]interface{}{
				"poll_id":   poll.ID,
				"question":  poll.Question,
				"options":   poll.Options,
				"is_module": poll.IsModule,
				"position": poll.Position,
			},
		})
		var resultsMsg []byte
		if poll.Results != nil {
			restorePayload := make(map[string]interface{}, len(poll.Results)+1)
			for k, v := range poll.Results {
				restorePayload[k] = v
			}
			restorePayload["is_restore"] = true
			resultsMsg, _ = json.Marshal(map[string]interface{}{
				"type":    "poll_results",
				"payload": restorePayload,
			})
		}
		poll.mu.Unlock()
		send(insertedMsg)
		if resultsMsg != nil {
			send(resultsMsg)
		}
	}
}

// sendActivePoll восстанавливает все опросы для нового студента
func (wm *WebSocketManager) sendActivePoll(sessionID string, conn *safeConn) {
	wm.sendAllPolls(sessionID, func(msg []byte) {
		conn.WriteMessage(websocket.TextMessage, msg)
	})
}

// sendActivePollToTeacher восстанавливает все опросы для переподключившегося препода
func (wm *WebSocketManager) sendActivePollToTeacher(sessionID string) {
	wm.mu.RLock()
	tc := wm.teacherConns[sessionID]
	wm.mu.RUnlock()
	if tc == nil {
		return
	}
	wm.sendAllPolls(sessionID, func(msg []byte) {
		tc.WriteMessage(websocket.TextMessage, msg)
	})
}

// forwardToTeacher пробрасывает сырое сообщение преподавателю как есть
func (wm *WebSocketManager) forwardToTeacher(sessionID string, messageType int, message []byte) {
	wm.mu.RLock()
	teacherConn, ok := wm.teacherConns[sessionID]
	wm.mu.RUnlock()

	if !ok || teacherConn == nil {
		return
	}

	if err := teacherConn.WriteMessage(messageType, message); err != nil {
		log.Printf("❌ Failed to forward message to teacher: %v", err)
	}
}

// sendCurrentSlide шлёт студенту слайд на котором сейчас вся аудитория
func (wm *WebSocketManager) sendCurrentSlide(sessionID string, conn *safeConn) {
	var slideData string
	err := wm.DB.QueryRow(`
        SELECT current_slide FROM session_state WHERE session_id = ?
    `, sessionID).Scan(&slideData)

	var slideIndex int = 0

	if err == nil && slideData != "" {
		var slideState map[string]interface{}
		if err := json.Unmarshal([]byte(slideData), &slideState); err == nil {
			if idx, ok := slideState["slide_index"].(float64); ok {
				slideIndex = int(idx)
			}
		}
	}

	log.Printf("📍 Sending current slide %d to new student in session %s", slideIndex, sessionID)

	msg := map[string]interface{}{
		"type": "slide_changed",
		"payload": map[string]interface{}{
			"slide_index": slideIndex,
		},
	}

	message, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, message)
}

// saveCurrentSlide запоминает в БД какой слайд сейчас открыт — нужно для восстановления после перезапуска
func (wm *WebSocketManager) saveCurrentSlide(sessionID string, msg map[string]interface{}) {
	payload, ok := msg["payload"].(map[string]interface{})
	if !ok {
		log.Printf("⚠️ Invalid payload format for slide_changed")
		return
	}

	slideIndex, ok := payload["slide_index"].(float64)
	if !ok {
		log.Printf("⚠️ Invalid slide_index format")
		return
	}

	slideData := fmt.Sprintf(`{"slide_index": %d}`, int(slideIndex))

	log.Printf("💾 Saving current slide %d for session %s", int(slideIndex), sessionID)

	_, err := wm.DB.Exec(`
        INSERT OR REPLACE INTO session_state (session_id, current_slide, updated_at)
        VALUES (?, ?, ?)
    `, sessionID, slideData, time.Now())

	if err != nil {
		log.Printf("❌ Failed to save current slide: %v", err)
	}
}

// SessionStateHandler возвращает текущий слайд сессии (GET /api/sessions/{id}/state)
func (wm *WebSocketManager) SessionStateHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	var slideData string
	slideIndex := 0
	err := wm.DB.QueryRow(`SELECT current_slide FROM session_state WHERE session_id = ?`, sessionID).Scan(&slideData)
	if err == nil && slideData != "" {
		var state map[string]interface{}
		if json.Unmarshal([]byte(slideData), &state) == nil {
			if idx, ok := state["slide_index"].(float64); ok {
				slideIndex = int(idx)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"slide_index": slideIndex})
}

// GetStudentCount возвращает число студентов онлайн в сессии
func (wm *WebSocketManager) GetStudentCount(sessionID string) int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	return len(wm.studentConns[sessionID])
}

// CloseSession принудительно завершает все WebSocket соединения в сессии
func (wm *WebSocketManager) CloseSession(sessionID string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	log.Printf("🔴 Closing session: %s", sessionID)

	if tc, ok := wm.teacherConns[sessionID]; ok {
		tc.Close()
		delete(wm.teacherConns, sessionID)
	}

	if students, ok := wm.studentConns[sessionID]; ok {
		for _, student := range students {
			student.Conn.WriteMessage(websocket.TextMessage,
				[]byte(`{"type": "lecture_end", "payload": {"message": "Лекция завершена"}}`))
			student.Conn.Close()
		}
		delete(wm.studentConns, sessionID)
	}
}

// saveTestAnswer принимает ответы студента и сохраняет в памяти
func (wm *WebSocketManager) saveTestAnswer(sessionID, studentName string, msg map[string]interface{}) {
	payload, ok := msg["payload"].(map[string]interface{})
	if !ok {
		return
	}
	moduleIDf, _ := payload["module_id"].(float64)
	moduleID := int(moduleIDf)

	// поздние ответы на закрытый модуль не принимаем
	wm.mu.RLock()
	alreadyDone := wm.completedModules[sessionID] != nil && wm.completedModules[sessionID][moduleID]
	wm.mu.RUnlock()
	if alreadyDone {
		log.Printf("⚠️ Ignoring late answer from %s for completed module %d in session %s", studentName, moduleID, sessionID)
		return
	}

	answersRaw, _ := payload["answers"].(map[string]interface{})

	wm.mu.Lock()
	if wm.testAnswers[sessionID] == nil {
		wm.testAnswers[sessionID] = make(map[string]*StudentAnswers)
	}
	if wm.testAnswers[sessionID][studentName] == nil {
		wm.testAnswers[sessionID][studentName] = &StudentAnswers{
			Name:    studentName,
			Answers: make(map[int]map[int]string),
		}
	}
	sa := wm.testAnswers[sessionID][studentName]
	if sa.Answers[moduleID] == nil {
		sa.Answers[moduleID] = make(map[int]string)
	}
	for qIdxStr, ans := range answersRaw {
		qIdx := 0
		if _, err := fmt.Sscanf(qIdxStr, "%d", &qIdx); err != nil {
			log.Printf("⚠️ invalid question index %q from %s: %v", qIdxStr, studentName, err)
			continue
		}
		var ansStr string
		switch v := ans.(type) {
		case string:
			ansStr = v
		case float64:
			ansStr = fmt.Sprintf("%d", int(v))
		case []interface{}:
			// чекбоксы дают массив индексов, сортируем для стабильного сравнения
			parts := make([]string, 0, len(v))
			for _, item := range v {
				if f, ok := item.(float64); ok {
					parts = append(parts, fmt.Sprintf("%d", int(f)))
				}
			}
			sort.Strings(parts)
			ansStr = strings.Join(parts, ",")
		default:
			ansStr = fmt.Sprintf("%v", v)
		}
		sa.Answers[moduleID][qIdx] = ansStr

		// неблокирующая отправка в очередь записи — если канал полон, пропускаем
		// (ответ уже в памяти, допишется при следующем вызове)
		select {
		case wm.answerCh <- answerEntry{
			SessionID:   sessionID,
			StudentName: studentName,
			ModuleID:    moduleID,
			QuestionIdx: qIdx,
			Answer:      ansStr,
		}:
		default:
			log.Printf("⚠️ answerCh full, skipping DB write for %s Q%d", studentName, qIdx)
		}
	}
	wm.mu.Unlock()
}

// calcStudentScore считает итоговый балл студента по всем вопросам в сессии
func (wm *WebSocketManager) calcStudentScore(sessionID, studentName string) map[string]interface{} {
	return wm.calcStudentScoreByID(sessionID, 0, studentName)
}

// calcStudentScoreByID считает балл с учётом персонального банка вопросов.
// Если studentID > 0 и модуль использует банк — берёт персональный набор студента.
func (wm *WebSocketManager) calcStudentScoreByID(sessionID string, studentID int, studentName string) map[string]interface{} {
	wm.mu.RLock()
	sa := wm.testAnswers[sessionID][studentName]
	wm.mu.RUnlock()
	if sa == nil {
		return map[string]interface{}{"correct": 0, "total": 0, "score10": 0.0}
	}

	// Получаем все тестовые модули лекции
	var lectureID int
	if err := wm.DB.QueryRow("SELECT lecture_id FROM active_sessions WHERE session_id = ?", sessionID).Scan(&lectureID); err != nil {
		return map[string]interface{}{"correct": 0, "total": 0, "score10": 0.0}
	}
	rows, err := wm.DB.Query(`
		SELECT m.id FROM modules m
		JOIN lecture_modules lm ON lm.module_id = m.id
		WHERE lm.lecture_id = ? AND m.module_type IN ('test','question')
		ORDER BY lm.module_order
	`, lectureID)
	if err != nil {
		return map[string]interface{}{"correct": 0, "total": 0, "score10": 0.0}
	}
	defer rows.Close()

	correct := 0
	total := 0
	for rows.Next() {
		var moduleID int
		if rows.Scan(&moduleID) != nil {
			continue
		}
		qs := wm.getPersonalizedModuleQuestions(sessionID, studentID, moduleID)
		for qIdx, q := range qs {
			total++
			correctIdxsStr := correctIdxsToStr(q.CorrectIdxs)
			var studentAnswer string
			if sa.Answers[moduleID] != nil {
				studentAnswer = sa.Answers[moduleID][qIdx]
			}
			if answersMatch(studentAnswer, correctIdxsStr) {
				correct++
			}
		}
	}

	score10 := 0.0
	if total > 0 {
		score10 = float64(correct) / float64(total) * 10
	}
	return map[string]interface{}{"correct": correct, "total": total, "score10": score10}
}

// getSessionQuestions собирает все вопросы лекции с правильными ответами для проверки
func (wm *WebSocketManager) getSessionQuestions(sessionID string) ([]map[string]interface{}, int) {
	var lectureID int
	err := wm.DB.QueryRow("SELECT lecture_id FROM active_sessions WHERE session_id = ?", sessionID).Scan(&lectureID)
	if err != nil {
		log.Printf("getSessionQuestions: session %s not found: %v", sessionID, err)
		return nil, 0
	}
	rows, err := wm.DB.Query(`
		SELECT m.id, COALESCE(m.title, ''), m.content FROM modules m
		JOIN lecture_modules lm ON lm.module_id = m.id
		WHERE lm.lecture_id = ? AND m.module_type IN ('test','question')
		ORDER BY lm.module_order
	`, lectureID)
	if err != nil {
		log.Printf("getSessionQuestions: query error: %v", err)
		return nil, 0
	}
	defer rows.Close()

	var questions []map[string]interface{}
	for rows.Next() {
		var moduleID int
		var moduleTitle, contentStr string
		if err := rows.Scan(&moduleID, &moduleTitle, &contentStr); err != nil {
			log.Printf("getSessionQuestions: scan error: %v", err)
			continue
		}
		if contentStr == "" {
			log.Printf("getSessionQuestions: module %d has empty content, skipping", moduleID)
			continue
		}

		// основной формат — плоский массив [{question, answers, correct}]
		var qs []map[string]interface{}
		if err := json.Unmarshal([]byte(contentStr), &qs); err != nil {
			// старый формат {"questions": [...]} или просто объект без вопросов (текстовый модуль)
			var wrapper map[string]interface{}
			if err2 := json.Unmarshal([]byte(contentStr), &wrapper); err2 == nil {
				if arr, ok := wrapper["questions"].([]interface{}); ok {
					for _, item := range arr {
						if qmap, ok := item.(map[string]interface{}); ok {
							qs = append(qs, qmap)
						}
					}
				}
				// объект без "questions" — текстовый или визуальный модуль, пропускаем тихо
				if len(qs) == 0 {
					continue
				}
			} else {
				log.Printf("getSessionQuestions: module %d invalid JSON: %v", moduleID, err2)
				continue
			}
		}

		log.Printf("getSessionQuestions: module %d has %d questions", moduleID, len(qs))
		for qIdx, q := range qs {
			answers, _ := q["answers"].([]interface{})
			correctIdxs := extractCorrectIdxs(q["correct"])
			correctText := ""
			if len(correctIdxs) == 1 && correctIdxs[0] >= 0 && correctIdxs[0] < len(answers) {
				correctText, _ = answers[correctIdxs[0]].(string)
			}
			var answerTexts []string
			for _, a := range answers {
				if s, ok := a.(string); ok {
					answerTexts = append(answerTexts, s)
				}
			}
			questions = append(questions, map[string]interface{}{
				"module_id":     moduleID,
				"module_title":  moduleTitle,
				"q_idx":         qIdx,
				"question":      q["question"],
				"correct_text":  correctText,
				"correct_idxs":  correctIdxsToStr(correctIdxs),
				"answer_count":  len(answers),
				"answers_texts": answerTexts,
			})
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("getSessionQuestions: rows error: %v", err)
	}
	log.Printf("getSessionQuestions: session %s → lecture %d → %d questions total", sessionID, lectureID, len(questions))
	return questions, len(questions)
}

// ResultsXLSXHandler отдаёт Excel-файл с результатами всех студентов
func (wm *WebSocketManager) ResultsXLSXHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	questions, totalQ := wm.getSessionQuestions(sessionID)
	if totalQ == 0 {
		http.Error(w, "No test questions in this lecture", http.StatusNotFound)
		return
	}

	wm.mu.RLock()
	answers := wm.testAnswers[sessionID]
	wm.mu.RUnlock()

	var qInfos []QInfo
	for _, q := range questions {
		ac, _ := q["answer_count"].(int)
		qInfos = append(qInfos, QInfo{
			ModuleID:    q["module_id"].(int),
			QIdx:        q["q_idx"].(int),
			Question:    fmt.Sprintf("%v", q["question"]),
			CorrectText: q["correct_text"].(string),
			CorrectIdxs: q["correct_idxs"].(string),
			AnswerCount: ac,
		})
	}

	var studentResults []StudentResult
	for name, sa := range answers {
		scores := make([]float64, len(qInfos))
		for i, q := range qInfos {
			if sa.Answers[q.ModuleID] != nil {
				ans := sa.Answers[q.ModuleID][q.QIdx]
				if ans != "" {
					if answersMatch(ans, q.CorrectIdxs) {
						scores[i] = 1.0
					} else if q.AnswerCount > 0 {
						scores[i] = math.Round(-1.0/float64(q.AnswerCount)*1000) / 1000
					}
				}
			}
		}
		studentResults = append(studentResults, StudentResult{Name: name, Scores: scores})
	}

	log.Printf("📊 Excel: %d questions, %d students answered", len(qInfos), len(studentResults))
	for _, sr := range studentResults {
		log.Printf("  Student: %s, scores: %v", sr.Name, sr.Scores)
	}
	xlsx, err := buildXLSX(qInfos, studentResults, totalQ)
	if err != nil {
		log.Printf("Excel generation error: %v", err)
		http.Error(w, "Failed to generate Excel", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"results_%s.xlsx\"", sessionID))
	w.Write(xlsx)
}

// buildXLSX собирает xlsx вручную через zip — без сторонних библиотек
func buildXLSX(questions []QInfo, results []StudentResult, totalQ int) ([]byte, error) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	xmlEsc := func(s string) string {
		s = strings.ReplaceAll(s, "&", "&amp;")
		s = strings.ReplaceAll(s, "<", "&lt;")
		s = strings.ReplaceAll(s, ">", "&gt;")
		s = strings.ReplaceAll(s, "\"", "&quot;")
		return s
	}
	colName := func(n int) string {
		result := ""
		for n > 0 {
			n--
			result = string(rune('A'+n%26)) + result
			n /= 26
		}
		return result
	}

	strs := []string{}
	strIdx := map[string]int{}
	si := func(s string) int {
		if idx, ok := strIdx[s]; ok {
			return idx
		}
		strIdx[s] = len(strs)
		strs = append(strs, s)
		return len(strs) - 1
	}

	n := len(questions)
	headers := []string{"ФИО студента"}
	for i := range questions {
		headers = append(headers, fmt.Sprintf("Вопрос %d", i+1))
	}
	headers = append(headers, "Балл")
	for _, h := range headers {
		si(h)
	}
	for _, r := range results {
		si(r.Name)
	}

	var sheetXML strings.Builder
	sheetXML.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sheetXML.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	sheetXML.WriteString(`<sheetData>`)

	sheetXML.WriteString(`<row r="1">`)
	for ci, h := range headers {
		sheetXML.WriteString(fmt.Sprintf(`<c r="%s1" t="s" s="1"><v>%d</v></c>`, colName(ci+1), si(h)))
	}
	sheetXML.WriteString(`</row>`)

	for ri, r := range results {
		row := ri + 2
		sheetXML.WriteString(fmt.Sprintf(`<row r="%d">`, row))
		sheetXML.WriteString(fmt.Sprintf(`<c r="A%d" t="s"><v>%d</v></c>`, row, si(r.Name)))
		var total float64
		for ci, score := range r.Scores {
			style := "3"
			if score <= 0 {
				style = "4"
			}
			sheetXML.WriteString(fmt.Sprintf(`<c r="%s%d" s="%s"><v>%g</v></c>`, colName(ci+2), row, style, score))
			total += score
		}
		total = math.Round(total*100) / 100
		totalCol := colName(n + 2)
		sheetXML.WriteString(fmt.Sprintf(`<c r="%s%d" s="5"><v>%g</v></c>`, totalCol, row, total))
		sheetXML.WriteString(`</row>`)
	}

	sheetXML.WriteString(`</sheetData>`)
	sheetXML.WriteString(fmt.Sprintf(`<freezePane xSplit="0" ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`))
	sheetXML.WriteString(`</worksheet>`)

	var ssXML strings.Builder
	ssXML.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	ssXML.WriteString(fmt.Sprintf(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" uniqueCount="%d">`, len(strs), len(strs)))
	for _, s := range strs {
		ssXML.WriteString(fmt.Sprintf(`<si><t xml:space="preserve">%s</t></si>`, xmlEsc(s)))
	}
	ssXML.WriteString(`</sst>`)

	stylesXML := `<?xml version="1.0" encoding="UTF-8"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <fonts>
    <font><sz val="11"/><name val="Arial"/></font>
    <font><sz val="11"/><b/><color rgb="FFFFFFFF"/><name val="Arial"/></font>
    <font><sz val="9"/><i/><color rgb="FF555555"/><name val="Arial"/></font>
    <font><sz val="11"/><name val="Arial"/></font>
    <font><sz val="11"/><b/><color rgb="FF1A5276"/><name val="Arial"/></font>
  </fonts>
  <fills>
    <fill><patternFill patternType="none"/></fill>
    <fill><patternFill patternType="gray125"/></fill>
    <fill><patternFill patternType="solid"><fgColor rgb="FF2C3E50"/></patternFill></fill>
    <fill><patternFill patternType="solid"><fgColor rgb="FFD5F5E3"/></patternFill></fill>
    <fill><patternFill patternType="solid"><fgColor rgb="FFFADBD8"/></patternFill></fill>
  </fills>
  <borders><border><left/><right/><top/><bottom/><diagonal/></border></borders>
  <cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
  <cellXfs>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
    <xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"><alignment horizontal="center" wrapText="1"/></xf>
    <xf numFmtId="0" fontId="3" fillId="3" borderId="0" xfId="0" applyFill="1"><alignment horizontal="center"/></xf>
    <xf numFmtId="0" fontId="3" fillId="4" borderId="0" xfId="0" applyFill="1"><alignment horizontal="center"/></xf>
    <xf numFmtId="0" fontId="4" fillId="0" borderId="0" xfId="0" applyFont="1"><alignment horizontal="center"/></xf>
  </cellXfs>
</styleSheet>`

	workbookXML := `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Результаты" sheetId="1" r:id="rId1"/></sheets>
</workbook>`

	wbRels := `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

	contentTypes := `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
  <Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

	rootRels := `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

	files := map[string]string{
		"[Content_Types].xml":        contentTypes,
		"_rels/.rels":                rootRels,
		"xl/workbook.xml":            workbookXML,
		"xl/_rels/workbook.xml.rels": wbRels,
		"xl/worksheets/sheet1.xml":   sheetXML.String(),
		"xl/sharedStrings.xml":       ssXML.String(),
		"xl/styles.xml":              stylesXML,
	}

	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		f.Write([]byte(data))
	}
	zw.Close()
	return buf.Bytes(), nil
}

// HasTestModules проверяет, есть ли в лекции хоть один тест
func (wm *WebSocketManager) HasTestModules(sessionID string) bool {
	_, total := wm.getSessionQuestions(sessionID)
	return total > 0
}

// GetStudentScore возвращает текущий счёт студента (публичная обёртка)
func (wm *WebSocketManager) GetStudentScore(sessionID, studentName string) map[string]interface{} {
	return wm.calcStudentScore(sessionID, studentName)
}

// HasTestsHandler отвечает есть ли тесты — нужно для отображения кнопки результатов
func (wm *WebSocketManager) HasTestsHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	hasTests := wm.HasTestModules(sessionID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"has_tests": hasTests})
}

// getPersonalizedModuleQuestions возвращает персональный набор вопросов студента из банка.
// Если у студента нет назначения или studentID <= 0 — читает полный контент модуля.
func (wm *WebSocketManager) getPersonalizedModuleQuestions(sessionID string, studentID, moduleID int) []moduleQuestion {
	if studentID > 0 {
		var questionsJSON string
		err := wm.DB.QueryRow(
			`SELECT questions_json FROM student_module_assignments WHERE session_id=? AND student_id=? AND module_id=?`,
			sessionID, studentID, moduleID,
		).Scan(&questionsJSON)
		if err == nil && questionsJSON != "" {
			var qs []map[string]interface{}
			if json.Unmarshal([]byte(questionsJSON), &qs) == nil && len(qs) > 0 {
				result := make([]moduleQuestion, 0, len(qs))
				for _, q := range qs {
					answers, _ := q["answers"].([]interface{})
					ansStrs := make([]string, len(answers))
					for i, a := range answers {
						ansStrs[i] = fmt.Sprintf("%v", a)
					}
					result = append(result, moduleQuestion{
						Question:    fmt.Sprintf("%v", q["question"]),
						Answers:     ansStrs,
						CorrectIdxs: extractCorrectIdxs(q["correct"]),
					})
				}
				return result
			}
		}
	}
	return wm.getModuleQuestionsDetailed(moduleID)
}

// getModuleQuestionsDetailed загружает вопросы конкретного модуля с правильными ответами
func (wm *WebSocketManager) getModuleQuestionsDetailed(moduleID int) []moduleQuestion {
	var contentStr string
	err := wm.DB.QueryRow("SELECT content FROM modules WHERE id = ?", moduleID).Scan(&contentStr)
	if err != nil {
		return nil
	}
	var qs []map[string]interface{}
	if err := json.Unmarshal([]byte(contentStr), &qs); err != nil {
		// попробуем старый формат {"questions": [...]}
		var wrapper map[string]interface{}
		if err2 := json.Unmarshal([]byte(contentStr), &wrapper); err2 == nil {
			if arr, ok := wrapper["questions"].([]interface{}); ok {
				for _, item := range arr {
					if qmap, ok := item.(map[string]interface{}); ok {
						qs = append(qs, qmap)
					}
				}
			}
		}
		if len(qs) == 0 {
			return nil
		}
	}
	result := make([]moduleQuestion, 0, len(qs))
	for _, q := range qs {
		answers, _ := q["answers"].([]interface{})
		ansStrs := make([]string, len(answers))
		for i, a := range answers {
			ansStrs[i] = fmt.Sprintf("%v", a)
		}
		correctIdxs := extractCorrectIdxs(q["correct"])
		result = append(result, moduleQuestion{
			Question:    fmt.Sprintf("%v", q["question"]),
			Answers:     ansStrs,
			CorrectIdxs: correctIdxs,
		})
	}
	return result
}

// extractCorrectIdxs парсит поле correct — может быть числом (старый формат) или массивом
func extractCorrectIdxs(val interface{}) []int {
	switch v := val.(type) {
	case float64:
		return []int{int(v)}
	case []interface{}:
		idxs := make([]int, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				idxs = append(idxs, int(f))
			}
		}
		if len(idxs) > 0 {
			return idxs
		}
	}
	return []int{0}
}

// correctIdxsToStr превращает []int в строку типа "0" или "0,2"
func correctIdxsToStr(idxs []int) string {
	parts := make([]string, len(idxs))
	for i, idx := range idxs {
		parts[i] = fmt.Sprintf("%d", idx)
	}
	return strings.Join(parts, ",")
}

// parseIdxSet разбирает строку "0,2" в map для быстрой проверки
func parseIdxSet(s string) map[int]bool {
	result := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var idx int
		if _, err := fmt.Sscan(part, &idx); err == nil {
			result[idx] = true
		}
	}
	return result
}

// answerToLabel преобразует строку индексов ("0,2") в буквы ("а, в")
func answerToLabel(ans string) string {
	if ans == "" {
		return ""
	}
	var lbls []string
	for _, p := range strings.Split(ans, ",") {
		if idx, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && idx >= 0 {
			lbls = append(lbls, string(rune('а'+idx)))
		}
	}
	return strings.Join(lbls, ", ")
}

// answersMatch проверяет совпадение ответа студента с правильными индексами
func answersMatch(studentAns, correctIdxs string) bool {
	if studentAns == "" {
		return false
	}
	studentSet := parseIdxSet(studentAns)
	correctSet := parseIdxSet(correctIdxs)
	if len(studentSet) != len(correctSet) {
		return false
	}
	for idx := range correctSet {
		if !studentSet[idx] {
			return false
		}
	}
	return true
}

// extractCorrectIdx берёт первый правильный индекс (для обратной совместимости)
func extractCorrectIdx(val interface{}) int {
	switch v := val.(type) {
	case float64:
		return int(v)
	case []interface{}:
		if len(v) > 0 {
			if f, ok := v[0].(float64); ok {
				return int(f)
			}
		}
	}
	return 0
}

// buildModuleResult считает результат по модулю: +1 за верный, -1/N за неверный, 0 за пропущенный
func buildModuleResult(moduleID int, qs []moduleQuestion, sa *StudentAnswers) map[string]interface{} {
	qResults := make([]QuestionResult, 0, len(qs))
	totalPoints := 0.0

	for qIdx, q := range qs {
		n := len(q.Answers)
		correctIdxsStr := correctIdxsToStr(q.CorrectIdxs)

		// Определяем индексы ответа студента
		var yourIdxs []int
		var ansStr string
		if sa != nil && sa.Answers[moduleID] != nil {
			ansStr = sa.Answers[moduleID][qIdx]
		}
		if ansStr != "" {
			for idx := range parseIdxSet(ansStr) {
				if idx >= 0 && idx < n {
					yourIdxs = append(yourIdxs, idx)
				}
			}
			sort.Ints(yourIdxs)
		}

		isCorrect := len(yourIdxs) > 0 && answersMatch(ansStr, correctIdxsStr)
		var points float64
		if len(yourIdxs) == 0 {
			points = 0
		} else if isCorrect {
			points = 1.0
		} else {
			if n > 0 {
				points = -1.0 / float64(n)
			}
		}
		totalPoints += points

		qResults = append(qResults, QuestionResult{
			Question:       q.Question,
			Answers:        q.Answers,
			YourAnswerIdxs: yourIdxs,
			CorrectIdxs:    q.CorrectIdxs,
			IsCorrect:      isCorrect,
			Points:         math.Round(points*1000) / 1000,
		})
	}

	return map[string]interface{}{
		"module_id":    moduleID,
		"questions":    qResults,
		"total_points": math.Round(totalPoints*100) / 100,
		"max_points":   float64(len(qs)),
	}
}

// CompleteModule закрывает модуль: фиксирует в БД и отправляет каждому студенту его личные результаты
func (wm *WebSocketManager) CompleteModule(sessionID string, moduleID int) error {
	_, err := wm.DB.Exec(`
		INSERT OR IGNORE INTO completed_modules (session_id, module_id, completed_at)
		VALUES (?, ?, ?)
	`, sessionID, moduleID, time.Now())
	if err != nil {
		return fmt.Errorf("save completed_modules: %w", err)
	}

	wm.mu.Lock()
	if wm.completedModules[sessionID] == nil {
		wm.completedModules[sessionID] = make(map[int]bool)
	}
	wm.completedModules[sessionID][moduleID] = true
	// снимаем слепок соединений под локом, дальше работаем без него
	studentsCopy := make([]*StudentConn, len(wm.studentConns[sessionID]))
	copy(studentsCopy, wm.studentConns[sessionID])
	answersCopy := make(map[string]*StudentAnswers)
	for name, sa := range wm.testAnswers[sessionID] {
		answersCopy[name] = sa
	}
	wm.mu.Unlock()

	for _, sc := range studentsCopy {
		sa := answersCopy[sc.Name]
		qs := wm.getPersonalizedModuleQuestions(sessionID, sc.ID, moduleID)
		result := buildModuleResult(moduleID, qs, sa)
		msgData, err := json.Marshal(map[string]interface{}{
			"type":    "module_completed",
			"payload": result,
		})
		if err != nil {
			continue
		}
		if err := sc.Conn.WriteMessage(websocket.TextMessage, msgData); err != nil {
			log.Printf("❌ Failed to send module_completed to %s: %v", sc.Name, err)
		}
	}

	log.Printf("✅ Module %d completed in session %s, notified %d students", moduleID, sessionID, len(studentsCopy))
	return nil
}

// CompleteModuleHTTPHandler обрабатывает POST /api/sessions/{id}/modules/{mid}/complete
func (wm *WebSocketManager) CompleteModuleHTTPHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	moduleID, err := strconv.Atoi(chi.URLParam(r, "mid"))
	if err != nil || moduleID <= 0 {
		http.Error(w, "invalid module_id", http.StatusBadRequest)
		return
	}
	if err := wm.CompleteModule(sessionID, moduleID); err != nil {
		log.Printf("CompleteModule error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// CompletedModulesHandler возвращает список завершённых module_id из БД
// (GET /api/sessions/{id}/completed_modules)
func (wm *WebSocketManager) CompletedModulesHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	rows, err := wm.DB.Query(`SELECT module_id FROM completed_modules WHERE session_id = ?`, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	// восстанавливаем состояние в памяти на случай если сервер перезапускался
	wm.mu.Lock()
	if wm.completedModules[sessionID] == nil {
		wm.completedModules[sessionID] = make(map[int]bool)
	}
	for _, id := range ids {
		wm.completedModules[sessionID][id] = true
	}
	wm.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"completed_module_ids": ids})
}

// sendCompletedModulesState при подключении студента отправляет результаты по всем уже закрытым модулям
func (wm *WebSocketManager) sendCompletedModulesState(sessionID, studentName string, studentID int, conn *safeConn) {
	wm.mu.RLock()
	completed := wm.completedModules[sessionID]
	var sa *StudentAnswers
	if wm.testAnswers[sessionID] != nil {
		sa = wm.testAnswers[sessionID][studentName]
	}
	wm.mu.RUnlock()

	for moduleID := range completed {
		qs := wm.getPersonalizedModuleQuestions(sessionID, studentID, moduleID)
		result := buildModuleResult(moduleID, qs, sa)
		msgData, err := json.Marshal(map[string]interface{}{
			"type":    "module_completed",
			"payload": result,
		})
		if err != nil {
			continue
		}
		conn.WriteMessage(websocket.TextMessage, msgData)
	}
}

// ResultsDataAPIHandler отдаёт JSON с ответами всех студентов, сгруппированными по модулям
// (GET /api/sessions/{id}/results_data)
func (wm *WebSocketManager) ResultsDataAPIHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	questions, _ := wm.getSessionQuestions(sessionID)

	type QRow struct {
		Number   int    `json:"number"`
		Question string `json:"question"`
	}
	type CellData struct {
		IsCorrect   bool   `json:"is_correct"`
		HasAnswer   bool   `json:"has_answer"`
		ChosenLabel string `json:"chosen_label"`
	}
	type StudentRow struct {
		Name  string     `json:"name"`
		Cells []CellData `json:"cells"`
		Total float64    `json:"total"`
	}
	type ModuleBlock struct {
		ModuleID    int          `json:"module_id"`
		ModuleTitle string       `json:"module_title"`
		Questions   []QRow       `json:"questions"`
		Students    []StudentRow `json:"students"`
	}

	// группируем вопросы по модулям, порядок важен
	type modMeta struct {
		id    int
		title string
		qs    []map[string]interface{}
	}
	var moduleOrder []int
	moduleMap := map[int]*modMeta{}
	for _, q := range questions {
		mid := q["module_id"].(int)
		if _, ok := moduleMap[mid]; !ok {
			moduleOrder = append(moduleOrder, mid)
			moduleMap[mid] = &modMeta{
				id:    mid,
				title: fmt.Sprintf("%v", q["module_title"]),
			}
		}
		moduleMap[mid].qs = append(moduleMap[mid].qs, q)
	}

	wm.mu.RLock()
	ansMap := wm.testAnswers[sessionID]
	wm.mu.RUnlock()

	var blocks []ModuleBlock
	for _, mid := range moduleOrder {
		meta := moduleMap[mid]
		qRows := make([]QRow, len(meta.qs))
		for i, q := range meta.qs {
			qRows[i] = QRow{Number: i + 1, Question: fmt.Sprintf("%v", q["question"])}
		}

		var studentRows []StudentRow
		for name, sa := range ansMap {
			cells := make([]CellData, len(meta.qs))
			var total float64
			for i, q := range meta.qs {
				qIdx := q["q_idx"].(int)
				correctIdxs := q["correct_idxs"].(string)
				n := q["answer_count"].(int)
				var ans string
				if sa.Answers[mid] != nil {
					ans = sa.Answers[mid][qIdx]
				}
				isCorrect := answersMatch(ans, correctIdxs)
				cells[i] = CellData{IsCorrect: isCorrect, HasAnswer: ans != "", ChosenLabel: answerToLabel(ans)}
				if ans == "" {
					// пропустил
				} else if isCorrect {
					total += 1.0
				} else if n > 0 {
					total -= 1.0 / float64(n)
				}
			}
			total = math.Round(total*100) / 100
			studentRows = append(studentRows, StudentRow{Name: name, Cells: cells, Total: total})
		}
		sort.Slice(studentRows, func(i, j int) bool { return studentRows[i].Name < studentRows[j].Name })

		blocks = append(blocks, ModuleBlock{
			ModuleID:    mid,
			ModuleTitle: meta.title,
			Questions:   qRows,
			Students:    studentRows,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"modules":    blocks,
	})
}

// ResultsModuleXLSXHandler — GET /api/sessions/{id}/modules/{moduleId}/results.xlsx
func (wm *WebSocketManager) ResultsModuleXLSXHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	moduleID, err := strconv.Atoi(chi.URLParam(r, "moduleId"))
	if err != nil || moduleID <= 0 {
		http.Error(w, "invalid module_id", http.StatusBadRequest)
		return
	}

	questions, _ := wm.getSessionQuestions(sessionID)

	var qInfos []QInfo
	for _, q := range questions {
		if q["module_id"].(int) != moduleID {
			continue
		}
		ac, _ := q["answer_count"].(int)
		qInfos = append(qInfos, QInfo{
			ModuleID:    moduleID,
			QIdx:        q["q_idx"].(int),
			Question:    fmt.Sprintf("%v", q["question"]),
			CorrectText: q["correct_text"].(string),
			CorrectIdxs: q["correct_idxs"].(string),
			AnswerCount: ac,
		})
	}
	if len(qInfos) == 0 {
		http.Error(w, "No questions for this module", http.StatusNotFound)
		return
	}

	wm.mu.RLock()
	answers := wm.testAnswers[sessionID]
	wm.mu.RUnlock()

	var studentResults []StudentResult
	for name, sa := range answers {
		scores := make([]float64, len(qInfos))
		for i, q := range qInfos {
			if sa.Answers[q.ModuleID] != nil {
				ans := sa.Answers[q.ModuleID][q.QIdx]
				if ans != "" {
					if answersMatch(ans, q.CorrectIdxs) {
						scores[i] = 1.0
					} else if q.AnswerCount > 0 {
						scores[i] = math.Round(-1.0/float64(q.AnswerCount)*1000) / 1000
					}
				}
			}
		}
		studentResults = append(studentResults, StudentResult{Name: name, Scores: scores})
	}

	xlsx, err := buildXLSX(qInfos, studentResults, len(qInfos))
	if err != nil {
		http.Error(w, "Failed to generate Excel", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"module_%d_%s.xlsx\"", moduleID, sessionID))
	w.Write(xlsx)
}
