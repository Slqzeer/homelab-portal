package auth

import (
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// ClientIP trusts only the socket peer. No forwarding headers are trusted in
// this deployment; proxy support requires an explicit trusted-CIDR policy.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

type loginAttempts struct {
	times        []time.Time
	blockedUntil time.Time
}

type LoginLimiter struct {
	mu      sync.Mutex
	clients map[string]loginAttempts
}

// NewLoginLimiter tracks at most 4096 clients. When full, new clients are
// temporarily denied rather than evicting an active limit. Expired entries are
// reclaimed on capacity pressure; attempts for each client are also bounded.
func NewLoginLimiter() *LoginLimiter { return &LoginLimiter{clients: make(map[string]loginAttempts)} }

func (l *LoginLimiter) Allow(clientIP string, now time.Time) (bool, time.Duration) {
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		return false, 10 * time.Minute
	}
	clientIP = addr.Unmap().String()
	l.mu.Lock()
	defer l.mu.Unlock()
	attempts, exists := l.clients[clientIP]
	if !exists && len(l.clients) >= 4096 {
		retry := 10 * time.Minute
		for client, entry := range l.clients {
			expires := entry.blockedUntil
			if expires.IsZero() {
				expires = entry.times[len(entry.times)-1].Add(5 * time.Minute)
			}
			if !now.Before(expires) {
				delete(l.clients, client)
			} else if remaining := expires.Sub(now); remaining < retry {
				retry = remaining
			}
		}
		if len(l.clients) >= 4096 {
			return false, retry
		}
	}
	if now.Before(attempts.blockedUntil) {
		return false, attempts.blockedUntil.Sub(now)
	}
	if !attempts.blockedUntil.IsZero() {
		attempts = loginAttempts{}
	}
	for len(attempts.times) > 0 && !now.Before(attempts.times[0].Add(5*time.Minute)) {
		attempts.times = attempts.times[1:]
	}
	if len(attempts.times) >= 10 {
		attempts.blockedUntil = now.Add(10 * time.Minute)
		l.clients[clientIP] = attempts
		return false, 10 * time.Minute
	}
	attempts.times = append(attempts.times, now)
	l.clients[clientIP] = attempts
	return true, 0
}
