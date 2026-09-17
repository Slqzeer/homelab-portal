// Package auth implements provider login and local portal authorization sessions.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
)

const sessionCookieName = "__Host-portal_session"

// Claims contains only the verified authorization facts needed by the portal.
// OAuth tokens and subject/profile identifiers deliberately have no representation.
type Claims struct{ Groups []string }

var ErrInvalidSession = errors.New("invalid portal session")

type session struct {
	Authenticated bool     `json:"authenticated"`
	Groups        []string `json:"groups"`
	Issued        int64    `json:"issued"`
	LastSeen      int64    `json:"last_seen"`
	Expires       int64    `json:"expires"`
}

type SessionManager struct{ keys []cipher.AEAD }

// NewSessionManager accepts high-entropy key material of at least 32 bytes.
func NewSessionManager(current, previous []byte) (*SessionManager, error) {
	m := &SessionManager{}
	materials := [][]byte{current}
	if len(previous) > 0 {
		materials = append(materials, previous)
	}
	for _, material := range materials {
		if len(material) < 32 {
			return nil, errors.New("session key must contain at least 32 bytes")
		}
		key := sha256.Sum256(material)
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		m.keys = append(m.keys, aead)
	}
	return m, nil
}

func (m *SessionManager) Create(w http.ResponseWriter, claims Claims, now time.Time) error {
	s := session{Authenticated: true, Groups: claims.Groups, Issued: now.Unix(), LastSeen: now.Unix(), Expires: now.Add(4 * time.Hour).Unix()}
	return m.write(w, s)
}

func (m *SessionManager) write(w http.ResponseWriter, s session) error {
	plain, err := json.Marshal(s)
	if err != nil {
		return err
	}
	current := m.keys[0]
	nonce := make([]byte, current.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return errors.New("session encryption failed")
	}
	sealed := current.Seal(nonce, nonce, plain, []byte(sessionCookieName))
	cookie := &http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString(sealed), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Unix(s.Expires, 0)}
	if len(cookie.String()) > 4096 {
		return errors.New("portal session exceeds cookie size limit")
	}
	http.SetCookie(w, cookie)
	return nil
}

func (m *SessionManager) Load(r *http.Request, now time.Time) (*catalog.Identity, error) {
	s, err := m.read(r, now)
	if err != nil || s == nil {
		return nil, err
	}
	identity := &catalog.Identity{Authenticated: s.Authenticated, Groups: make(map[string]struct{}, len(s.Groups))}
	for _, group := range s.Groups {
		identity.Groups[group] = struct{}{}
	}
	_, identity.IsAdmin = identity.Groups["portal-admin"]
	return identity, nil
}

// Refresh renews idle activity and rotates encryption without moving absolute expiry.
// Invalid cookies are cleared; a missing cookie remains anonymous without a write.
func (m *SessionManager) Refresh(w http.ResponseWriter, r *http.Request, now time.Time) error {
	s, err := m.read(r, now)
	if err != nil {
		m.Destroy(w)
		return err
	}
	if s == nil {
		return nil
	}
	s.LastSeen = now.Unix()
	return m.write(w, *s)
}

// Destroy removes the local browser session using its original cookie scope.
func (m *SessionManager) Destroy(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (m *SessionManager) read(r *http.Request, now time.Time) (*session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return nil, nil
	}
	if err != nil || len(cookie.Value) > 4096 {
		return nil, ErrInvalidSession
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(sealed) < m.keys[0].NonceSize() {
		return nil, ErrInvalidSession
	}
	var plain []byte
	for _, key := range m.keys {
		plain, err = key.Open(nil, sealed[:key.NonceSize()], sealed[key.NonceSize():], []byte(sessionCookieName))
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, ErrInvalidSession
	}
	var s session
	if json.Unmarshal(plain, &s) != nil {
		return nil, ErrInvalidSession
	}
	if !s.Authenticated || s.Issued > now.Unix() || s.LastSeen < s.Issued || s.LastSeen > now.Unix() || s.Expires != s.Issued+int64((4*time.Hour)/time.Second) || now.Unix() >= s.Expires || now.Unix()-s.LastSeen >= int64((30*time.Minute)/time.Second) {
		return nil, ErrInvalidSession
	}
	return &s, nil
}
