package auth_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/stretchr/testify/require"
)

func sessionRequest(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://portal.example/", nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestSessionCreatesEncryptedCookieAndLoadsExactAuthorizationFacts(t *testing.T) {
	manager, err := auth.NewSessionManager(bytes.Repeat([]byte{1}, 32), nil)
	require.NoError(t, err)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	w := httptest.NewRecorder()
	require.NoError(t, manager.Create(w, auth.Claims{Groups: []string{"portal-admin", "Team A", "team a"}}, now))
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.Equal(t, "/", cookie.Path)
	require.Empty(t, cookie.Domain)
	require.NotContains(t, cookie.Value, "portal-admin")
	require.NotContains(t, cookie.Value, "Team A")
	identity, err := manager.Load(sessionRequest(cookie), now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, identity.Authenticated)
	require.True(t, identity.IsAdmin)
	require.Equal(t, map[string]struct{}{"portal-admin": {}, "Team A": {}, "team a": {}}, identity.Groups)
	// Independent encryptions of the same authorization facts must differ.
	w2 := httptest.NewRecorder()
	require.NoError(t, manager.Create(w2, auth.Claims{Groups: []string{"portal-admin", "Team A", "team a"}}, now))
	require.NotEqual(t, cookie.Value, w2.Result().Cookies()[0].Value)
}

func TestSessionPreservesDisplayNameWithoutChangingAuthorization(t *testing.T) {
	manager, err := auth.NewSessionManager(bytes.Repeat([]byte{1}, 32), nil)
	require.NoError(t, err)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	w := httptest.NewRecorder()
	require.NoError(t, manager.Create(w, auth.Claims{Groups: []string{"portal-admin"}, DisplayName: "Ada Lovelace"}, now))
	cookie := w.Result().Cookies()[0]
	require.NotContains(t, cookie.Value, "Ada Lovelace")
	identity, err := manager.Load(sessionRequest(cookie), now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "Ada Lovelace", identity.DisplayName)
	require.True(t, identity.IsAdmin)
}

func TestSessionRotationReadsPreviousKeyButOnlyWritesCurrent(t *testing.T) {
	oldKey, newKey := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	old, err := auth.NewSessionManager(oldKey, nil)
	require.NoError(t, err)
	rotated, err := auth.NewSessionManager(newKey, oldKey)
	require.NoError(t, err)
	currentOnly, err := auth.NewSessionManager(newKey, nil)
	require.NoError(t, err)
	now := time.Now()
	w := httptest.NewRecorder()
	require.NoError(t, old.Create(w, auth.Claims{Groups: []string{"Portal-Admin", "/portal-admin"}}, now))
	identity, err := rotated.Load(sessionRequest(w.Result().Cookies()[0]), now)
	require.NoError(t, err)
	require.False(t, identity.IsAdmin)
	w = httptest.NewRecorder()
	require.NoError(t, rotated.Create(w, auth.Claims{}, now))
	_, err = currentOnly.Load(sessionRequest(w.Result().Cookies()[0]), now)
	require.NoError(t, err)
	identity, err = old.Load(sessionRequest(w.Result().Cookies()[0]), now)
	require.ErrorIs(t, err, auth.ErrInvalidSession)
	require.Nil(t, identity)
}

func TestSessionRefreshAdvancesIdleTimeoutWithoutExtendingAbsoluteExpiry(t *testing.T) {
	manager, err := auth.NewSessionManager(bytes.Repeat([]byte{1}, 32), nil)
	require.NoError(t, err)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	w := httptest.NewRecorder()
	require.NoError(t, manager.Create(w, auth.Claims{}, now))
	original := w.Result().Cookies()[0]
	identity, err := manager.Load(sessionRequest(original), now.Add(30*time.Minute-time.Second))
	require.NoError(t, err)
	require.NotNil(t, identity)
	identity, err = manager.Load(sessionRequest(original), now.Add(30*time.Minute))
	require.ErrorIs(t, err, auth.ErrInvalidSession)
	require.Nil(t, identity)
	cookie := original
	for elapsed := 20 * time.Minute; elapsed < 4*time.Hour; elapsed += 20 * time.Minute {
		w = httptest.NewRecorder()
		require.NoError(t, manager.Refresh(w, sessionRequest(cookie), now.Add(elapsed)))
		cookie = w.Result().Cookies()[0]
		require.Equal(t, original.Expires, cookie.Expires)
		require.Equal(t, original.Name, cookie.Name)
		require.Equal(t, original.Path, cookie.Path)
		require.Equal(t, original.Domain, cookie.Domain)
	}
	identity, err = manager.Load(sessionRequest(cookie), now.Add(4*time.Hour-time.Second))
	require.NoError(t, err)
	require.NotNil(t, identity)
	identity, err = manager.Load(sessionRequest(cookie), now.Add(4*time.Hour))
	require.ErrorIs(t, err, auth.ErrInvalidSession)
	require.Nil(t, identity)
}

func TestInvalidSessionIsAnonymousAndWriterPathClearsCookie(t *testing.T) {
	manager, err := auth.NewSessionManager(bytes.Repeat([]byte{1}, 32), nil)
	require.NoError(t, err)
	now := time.Now()
	w := httptest.NewRecorder()
	require.NoError(t, manager.Create(w, auth.Claims{}, now))
	original := w.Result().Cookies()[0]
	for _, value := range []string{"!malformed", "", original.Value[:len(original.Value)-4] + "AAAA"} {
		t.Run(value[:min(len(value), 8)], func(t *testing.T) {
			bad := *original
			bad.Value = value
			identity, err := manager.Load(sessionRequest(&bad), now)
			require.Nil(t, identity)
			require.ErrorIs(t, err, auth.ErrInvalidSession)
			w := httptest.NewRecorder()
			require.ErrorIs(t, manager.Refresh(w, sessionRequest(&bad), now), auth.ErrInvalidSession)
			cleared := w.Result().Cookies()[0]
			require.Equal(t, original.Name, cleared.Name)
			require.Equal(t, original.Path, cleared.Path)
			require.Equal(t, original.Domain, cleared.Domain)
			require.Equal(t, -1, cleared.MaxAge)
			require.Empty(t, cleared.Value)
		})
	}
	w = httptest.NewRecorder()
	manager.Destroy(w)
	deleted := w.Result().Cookies()[0]
	require.Equal(t, original.Name, deleted.Name)
	require.Equal(t, original.Path, deleted.Path)
	require.Equal(t, original.Domain, deleted.Domain)
	require.True(t, deleted.Expires.Before(now))
	require.True(t, deleted.Secure && deleted.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, deleted.SameSite)
	identity, err := manager.Load(sessionRequest(nil), now)
	require.NoError(t, err)
	require.Nil(t, identity)
}

func TestSessionRejectsUnusableOversizedCookieWithoutWritingIt(t *testing.T) {
	manager, err := auth.NewSessionManager(bytes.Repeat([]byte{1}, 32), nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	require.Error(t, manager.Create(w, auth.Claims{Groups: []string{strings.Repeat("x", 4096)}}, time.Now()))
	require.Empty(t, w.Result().Cookies())
}
