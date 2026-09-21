package server

import (
	"net"
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int
	window   time.Duration
}

type visitor struct {
	count    int
	lastSeen time.Time
}

func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
	}
	go rl.cleanup()
	return rl
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
	for {
		time.Sleep(rl.window)
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
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
	if err != nil {
		return addr
	}
	return host
}
