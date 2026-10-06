package server

import (
	"net"
	"strings"
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int
	window   time.Duration
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

type visitor struct {
	count    int
	lastSeen time.Time
}

// NewRateLimiter creates an IP-based rate limiter allowing rate requests per
// window. A background goroutine periodically evicts expired entries; it is
// only started when window is positive and must be released with Stop.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if window > 0 {
		go rl.cleanup()
	} else {
		close(rl.done)
	}
	return rl
}

// Stop terminates the background cleanup goroutine and waits for it to exit.
// It is idempotent and safe to call concurrently, so callers can defer it or
// invoke it on shutdown.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stop) })
	<-rl.done
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists || time.Since(v.lastSeen) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, lastSeen: time.Now()}
		return true
	}

	if v.count >= rl.rate {
		return false
	}

	v.count++
	v.lastSeen = time.Now()
	return true
}

func (rl *RateLimiter) cleanup() {
	defer close(rl.done)

	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.evictExpired()
		case <-rl.stop:
			return
		}
	}
}

func (rl *RateLimiter) evictExpired() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, v := range rl.visitors {
		if time.Since(v.lastSeen) > rl.window {
			delete(rl.visitors, ip)
		}
	}
}

type ConnectionTracker struct {
	mu      sync.Mutex
	count   int
	maxConn int
}

func NewConnectionTracker(maxConn int) *ConnectionTracker {
	return &ConnectionTracker{maxConn: maxConn}
}

func (ct *ConnectionTracker) CanConnect() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.count < ct.maxConn
}

func (ct *ConnectionTracker) Add() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.count++
}

func (ct *ConnectionTracker) Remove() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.count > 0 {
		ct.count--
	}
}

func (ct *ConnectionTracker) Count() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.count
}

func extractIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	// No port present: a bare IPv6 literal may still carry brackets.
	return strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]")
}
