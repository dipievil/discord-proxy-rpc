package server

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
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
	rejected atomic.Uint64
}

type visitor struct {
	count    int
	lastSeen time.Time
	// warned records that a rejection in this window was already logged, so
	// the server warns once per IP per window instead of once per request.
	warned bool
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

// Enabled reports whether the limiter enforces a limit. A nil limiter and a
// non-positive rate or window all disable limiting: with no window an entry
// would expire immediately, and with a non-positive rate the limiter would
// reject every request after the first instead of allowing a budget. This
// matches the pre-9.2 WebSocket behaviour and keeps a disabled limiter from
// rejecting anything.
func (rl *RateLimiter) Enabled() bool {
	return rl != nil && rl.rate > 0 && rl.window > 0
}

func (rl *RateLimiter) Allow(ip string) bool {
	if !rl.Enabled() {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists || time.Since(v.lastSeen) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, lastSeen: time.Now()}
		return true
	}

	if v.count >= rl.rate {
		rl.rejected.Add(1)
		return false
	}

	v.count++
	v.lastSeen = time.Now()
	return true
}

// RetryAfter reports how long the IP must wait before its current window
// expires and requests are allowed again. It returns zero when the limiter is
// disabled, when the IP has no active window (Allow would let it through), or
// when the window has already elapsed. The contract matches Allow: an entry
// that is missing or older than the window is not an active limit.
func (rl *RateLimiter) RetryAfter(ip string) time.Duration {
	if !rl.Enabled() {
		return 0
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[ip]
	if !ok || time.Since(v.lastSeen) > rl.window {
		return 0
	}

	if remaining := rl.window - time.Since(v.lastSeen); remaining > 0 {
		return remaining
	}
	return 0
}

// MarkWarned reports whether a rejection for ip should be logged at warning
// level: true on the first rejection in the current window, false afterwards.
// The caller can log later rejections at debug level, keeping the warning
// signal without flooding the log; the rejection metric carries the volume.
// An entry that is missing or expired has no rejection to suppress yet and
// reports true without keeping state.
func (rl *RateLimiter) MarkWarned(ip string) bool {
	if !rl.Enabled() {
		return false
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[ip]
	if !ok || time.Since(v.lastSeen) > rl.window {
		return true
	}
	if v.warned {
		return false
	}
	v.warned = true
	return true
}

// Rejections reports how many requests Allow has denied since creation. It
// feeds the Prometheus rate-limit rejection counter without duplicating the
// limiter's counting logic.
func (rl *RateLimiter) Rejections() uint64 {
	return rl.rejected.Load()
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

// TryAdd atomically reserves a connection slot when the configured limit has
// not been reached, reporting whether the reservation succeeded. Checking and
// incrementing in a single critical section prevents concurrent upgrades from
// all passing the limit check before any of them increments the count. Callers
// that fail to establish the connection must call Remove to release the slot.
func (ct *ConnectionTracker) TryAdd() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.count >= ct.maxConn {
		return false
	}
	ct.count++
	return true
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
