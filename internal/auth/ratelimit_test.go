package auth_test

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestLoginLimiterAllowsTenStartsThenBlocksForTenMinutes(t *testing.T) {
	limiter := auth.NewLoginLimiter()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i := range 10 {
		allowed, retry := limiter.Allow("192.0.2.1", now.Add(time.Duration(i)*time.Second))
		require.True(t, allowed)
		require.Zero(t, retry)
	}
	blockedAt := now.Add(10 * time.Second)
	allowed, retry := limiter.Allow("192.0.2.1", blockedAt)
	require.False(t, allowed)
	require.Equal(t, 10*time.Minute, retry)
	allowed, retry = limiter.Allow("192.0.2.1", blockedAt.Add(time.Minute))
	require.False(t, allowed)
	require.Equal(t, 9*time.Minute, retry, "blocked requests do not extend the block")
	allowed, retry = limiter.Allow("192.0.2.2", blockedAt)
	require.True(t, allowed)
	require.Zero(t, retry)
	allowed, retry = limiter.Allow("192.0.2.1", blockedAt.Add(10*time.Minute))
	require.True(t, allowed)
	require.Zero(t, retry)
}

func TestLoginLimiterUsesRollingFiveMinuteWindow(t *testing.T) {
	limiter := auth.NewLoginLimiter()
	now := time.Now()
	allowed, _ := limiter.Allow("192.0.2.1", now)
	require.True(t, allowed)
	for range 9 {
		allowed, _ = limiter.Allow("192.0.2.1", now.Add(4*time.Minute))
		require.True(t, allowed)
	}
	allowed, _ = limiter.Allow("192.0.2.1", now.Add(5*time.Minute))
	require.True(t, allowed, "the first attempt has left the rolling window")
	allowed, retry := limiter.Allow("192.0.2.1", now.Add(5*time.Minute+time.Second))
	require.False(t, allowed, "ten recent attempts remain within five minutes")
	require.Equal(t, 10*time.Minute, retry)
}

func TestLoginLimiterCapacityDoesNotEvictActiveBlocksAndReclaimsExpiredClients(t *testing.T) {
	limiter := auth.NewLoginLimiter()
	now := time.Now()
	for range 11 {
		limiter.Allow("192.0.2.1", now)
	}
	// Capacity is documented as 4096 clients; active entries cannot be evicted
	// by rotating source IPs to bypass a block.
	for i := range 4095 {
		allowed, _ := limiter.Allow(fmt.Sprintf("2001:db8::%x", i+1), now)
		require.True(t, allowed)
	}
	for range 50 {
		allowed, retry := limiter.Allow("192.0.2.2", now)
		require.False(t, allowed)
		require.Equal(t, 5*time.Minute, retry)
	}
	allowed, retry := limiter.Allow("192.0.2.1", now.Add(5*time.Minute))
	require.False(t, allowed)
	require.Equal(t, 5*time.Minute, retry)
	allowed, _ = limiter.Allow("192.0.2.2", now.Add(5*time.Minute))
	require.True(t, allowed)
}

func TestLoginLimiterUsesRemoteAddressAndIgnoresForwardedHeaders(t *testing.T) {
	limiter := auth.NewLoginLimiter()
	now := time.Now()
	r := httptest.NewRequest("GET", "https://portal.example/auth/login", nil)
	r.RemoteAddr = "[::ffff:192.0.2.1]:49152"
	for i := range 10 {
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		r.Header.Set("Forwarded", fmt.Sprintf("for=198.51.100.%d", i))
		allowed, _ := limiter.Allow(auth.ClientIP(r), now)
		require.True(t, allowed)
	}
	allowed, _ := limiter.Allow("192.0.2.1", now)
	require.False(t, allowed)
	r.RemoteAddr = "not-an-address"
	allowed, _ = limiter.Allow(auth.ClientIP(r), now)
	require.False(t, allowed)
}
