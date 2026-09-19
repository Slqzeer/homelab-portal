package portalhttp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/auth"
)

const loginCookieName = "__Host-portal_login"
const loginTTL = 5 * time.Minute

// The encrypted browser cookie holds correlation secrets; bounded process-local
// state tracks pending transactions so replay cannot reuse a stolen old cookie.
type loginTransactions struct {
	key     cipher.AEAD
	mu      sync.Mutex
	pending map[string]time.Time
}
type loginTransaction struct {
	State, Nonce, Verifier string
	ReturnTo               string
	Expires                time.Time
}

func newLoginTransactions() (*loginTransactions, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.New("login initialization failed")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &loginTransactions{key: aead, pending: make(map[string]time.Time)}, nil
}

func randomToken() string {
	value := make([]byte, 32)
	_, _ = rand.Read(value) // crypto/rand.Read terminates the process on entropy failure.
	return base64.RawURLEncoding.EncodeToString(value)
}

func (t *loginTransactions) begin(w http.ResponseWriter, now time.Time, returnTo string) (loginTransaction, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for state, expiry := range t.pending {
		if !now.Before(expiry) {
			delete(t.pending, state)
		}
	}
	if len(t.pending) >= 4096 {
		return loginTransaction{}, errors.New("login capacity reached")
	}
	tx := loginTransaction{State: randomToken(), Nonce: randomToken(), Verifier: randomToken(), ReturnTo: returnTo, Expires: now.Add(loginTTL)}
	plain, err := json.Marshal(tx)
	if err != nil {
		return loginTransaction{}, err
	}
	nonce := make([]byte, t.key.NonceSize())
	_, _ = rand.Read(nonce)
	sealed := t.key.Seal(nonce, nonce, plain, []byte(loginCookieName))
	t.pending[tx.State] = tx.Expires
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Value: base64.RawURLEncoding.EncodeToString(sealed), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(loginTTL / time.Second), Expires: tx.Expires})
	return tx, nil
}

func (t *loginTransactions) consume(r *http.Request, now time.Time) (loginTransaction, error) {
	cookie, err := r.Cookie(loginCookieName)
	if err != nil || len(cookie.Value) > 4096 {
		return loginTransaction{}, auth.ErrLoginFailed
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(sealed) < t.key.NonceSize() {
		return loginTransaction{}, auth.ErrLoginFailed
	}
	plain, err := t.key.Open(nil, sealed[:t.key.NonceSize()], sealed[t.key.NonceSize():], []byte(loginCookieName))
	if err != nil {
		return loginTransaction{}, auth.ErrLoginFailed
	}
	var tx loginTransaction
	if json.Unmarshal(plain, &tx) != nil {
		return loginTransaction{}, auth.ErrLoginFailed
	}
	t.mu.Lock()
	expiry, exists := t.pending[tx.State]
	delete(t.pending, tx.State)
	t.mu.Unlock()
	if !exists || !expiry.Equal(tx.Expires) || !now.Before(expiry) {
		return loginTransaction{}, auth.ErrLoginFailed
	}
	return tx, nil
}

func clearLogin(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	s.beginLogin(w, r, "/")
}

func (s *server) beginLogin(w http.ResponseWriter, r *http.Request, returnTo string) {
	if allowed, retry := s.Limiter.Allow(auth.ClientIP(r), s.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.FormatInt(int64((retry+time.Second-1)/time.Second), 10))
		http.Error(w, "Too many sign-in attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}
	if s.OIDC == nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	tx, err := s.transactions.begin(w, s.Now(), returnTo)
	if err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, s.OIDC.LoginURL(tx.State, tx.Nonce, tx.Verifier), http.StatusFound)
}

func (s *server) callback(w http.ResponseWriter, r *http.Request) {
	clearLogin(w)
	tx, err := s.transactions.consume(r, s.Now())
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if err != nil || queryErr != nil || s.OIDC == nil || len(query["error"]) != 0 || len(query["state"]) != 1 || len(query["code"]) != 1 {
		http.Error(w, "Sign-in failed. Please start again.", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	claims, err := s.OIDC.Callback(ctx, auth.CallbackParams{Code: query.Get("code"), State: query.Get("state"), ExpectedState: tx.State, Nonce: tx.Nonce, Verifier: tx.Verifier})
	if err != nil {
		http.Error(w, "Sign-in failed. Please start again.", http.StatusBadRequest)
		return
	}
	if err = s.Sessions.Create(w, claims, s.Now()); err != nil {
		http.Error(w, "Sign-in failed. Please start again.", http.StatusBadRequest)
		return
	}
	for _, cookie := range (&http.Response{Header: w.Header()}).Cookies() {
		if cookie.Name == "__Host-portal_session" {
			setCSRF(w, cookie.Expires)
		}
	}
	http.Redirect(w, r, tx.ReturnTo, http.StatusSeeOther)
}
