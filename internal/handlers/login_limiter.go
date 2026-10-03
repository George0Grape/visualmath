package handlers

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	maxFailedAttempts = 5               // попыток до блокировки
	attemptWindow     = 15 * time.Minute // окно подсчёта попыток
	lockDuration      = 15 * time.Minute // длительность блокировки
)

type ipEntry struct {
	failures  int
	firstFail time.Time
	lockedUntil time.Time
}

type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
	// конфигурация порога (вынесена из глобальных const, чтобы один и тот же
	// механизм переиспользовать для login, регистрации, join-кодов и т.п.)
	maxAttempts  int
	window       time.Duration
	lockDuration time.Duration
}

// NewRateLimiter создаёт лимитер с заданным порогом: maxAttempts попыток за
// window, после чего IP блокируется на lock. Подходит и для счётчика неудач
// (login), и для общего rate-limit (вызывать RecordFailure на каждый запрос).
func NewRateLimiter(maxAttempts int, window, lock time.Duration) *LoginLimiter {
	l := &LoginLimiter{
		entries:      make(map[string]*ipEntry),
		maxAttempts:  maxAttempts,
		window:       window,
		lockDuration: lock,
	}
	go l.cleanup()
	return l
}

// NewLoginLimiter — лимитер для /login (5 попыток / 15 мин / блок 15 мин).
func NewLoginLimiter() *LoginLimiter {
	return NewRateLimiter(maxFailedAttempts, attemptWindow, lockDuration)
}

// IsBlocked возвращает true если IP заблокирован
func (l *LoginLimiter) IsBlocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		return false
	}
	if time.Now().Before(e.lockedUntil) {
		return true
	}
	// блокировка истекла — сбрасываем
	if !e.lockedUntil.IsZero() {
		delete(l.entries, ip)
	}
	return false
}

// RecordFailure фиксирует неудачную попытку; возвращает true если IP теперь заблокирован
func (l *LoginLimiter) RecordFailure(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		e = &ipEntry{firstFail: time.Now()}
		l.entries[ip] = e
	}
	// если окно истекло — обнуляем счётчик
	if time.Since(e.firstFail) > l.window {
		e.failures = 0
		e.firstFail = time.Now()
	}
	e.failures++
	if e.failures >= l.maxAttempts {
		e.lockedUntil = time.Now().Add(l.lockDuration)
		return true
	}
	return false
}

// RecordSuccess сбрасывает счётчик при успешном входе
func (l *LoginLimiter) RecordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, ip)
}

func (l *LoginLimiter) cleanup() {
	for range time.Tick(10 * time.Minute) {
		l.mu.Lock()
		now := time.Now()
		for ip, e := range l.entries {
			expired := e.lockedUntil.IsZero() && time.Since(e.firstFail) > l.window
			unlocked := !e.lockedUntil.IsZero() && now.After(e.lockedUntil)
			if expired || unlocked {
				delete(l.entries, ip)
			}
		}
		l.mu.Unlock()
	}
}

func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		// берём первый IP из цепочки
		for i := 0; i < len(ip); i++ {
			if ip[i] == ',' {
				return ip[:i]
			}
		}
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
