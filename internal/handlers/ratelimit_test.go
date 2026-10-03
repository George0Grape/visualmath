package handlers

import (
	"sync"
	"testing"
	"time"
)

func resetLimiter() {
	registerLimiter.mu.Lock()
	registerLimiter.buckets = make(map[string][]time.Time)
	registerLimiter.mu.Unlock()
}

func TestCheckRegisterLimit_AllowsFirstFive(t *testing.T) {
	resetLimiter()
	ip := "1.2.3.4"
	for i := 1; i <= regMaxAttempts; i++ {
		if !checkRegisterLimit(ip) {
			t.Errorf("attempt %d should be allowed", i)
		}
	}
}

func TestCheckRegisterLimit_BlocksSixth(t *testing.T) {
	resetLimiter()
	ip := "1.2.3.5"
	for i := 0; i < regMaxAttempts; i++ {
		checkRegisterLimit(ip)
	}
	if checkRegisterLimit(ip) {
		t.Error("6th attempt should be blocked")
	}
}

func TestCheckRegisterLimit_DifferentIPsIndependent(t *testing.T) {
	resetLimiter()
	ip1, ip2 := "10.0.0.1", "10.0.0.2"

	for i := 0; i < regMaxAttempts; i++ {
		checkRegisterLimit(ip1)
	}
	// ip1 исчерпал лимит — ip2 должен быть свободен
	if !checkRegisterLimit(ip2) {
		t.Error("ip2 should not be affected by ip1 rate limit")
	}
	// ip1 должен быть заблокирован
	if checkRegisterLimit(ip1) {
		t.Error("ip1 should be blocked")
	}
}

func TestCheckRegisterLimit_ExpiredWindowResets(t *testing.T) {
	resetLimiter()
	ip := "9.9.9.9"

	// набиваем bucket старыми (вне окна) временными метками
	registerLimiter.mu.Lock()
	old := time.Now().Add(-(regWindow + time.Second))
	for i := 0; i < regMaxAttempts+10; i++ {
		registerLimiter.buckets[ip] = append(registerLimiter.buckets[ip], old)
	}
	registerLimiter.mu.Unlock()

	// все метки старые → должно пропустить
	if !checkRegisterLimit(ip) {
		t.Error("expired entries should be cleaned up — request should be allowed")
	}
}

func TestCheckRegisterLimit_Concurrent(t *testing.T) {
	resetLimiter()
	ip := "5.5.5.5"

	allowed := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := checkRegisterLimit(ip)
			mu.Lock()
			if ok {
				allowed++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if allowed > regMaxAttempts {
		t.Errorf("concurrent: allowed %d requests, want ≤ %d", allowed, regMaxAttempts)
	}
}

func TestCheckRegisterLimit_ZeroAfterBlock(t *testing.T) {
	resetLimiter()
	ip := "7.7.7.7"

	for i := 0; i < regMaxAttempts; i++ {
		checkRegisterLimit(ip)
	}
	// пять раз — должен быть заблокирован на всех следующих
	for i := 0; i < 5; i++ {
		if checkRegisterLimit(ip) {
			t.Errorf("should stay blocked on attempt %d after limit reached", i+regMaxAttempts+1)
		}
	}
}
