// Package session implements minimal signed session cookies without any
// third-party dependencies, so the backend builds with Go's standard
// library alone (see /docs/adr-001-no-deps-phase1.md for why that matters
// in this build environment).
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const cookieName = "fragvault_session"

// Claims is the payload stored (signed, not encrypted) in the session cookie.
type Claims struct {
	SteamID   string `json:"steam_id"`
	Persona   string `json:"persona"`
	AvatarURL string `json:"avatar_url"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// Manager issues and verifies session cookies using HMAC-SHA256.
type Manager struct {
	secret   []byte
	ttl      time.Duration
	secure   bool // set false only for local http development
	sameSite http.SameSite
}

func NewManager(secret []byte, ttl time.Duration, secure bool) *Manager {
	return &Manager{secret: secret, ttl: ttl, secure: secure, sameSite: http.SameSiteLaxMode}
}

func (m *Manager) sign(payload []byte) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Issue writes a signed session cookie for the given claims onto the response.
func (m *Manager) Issue(w http.ResponseWriter, steamID, persona, avatarURL string) error {
	now := time.Now()
	claims := Claims{
		SteamID:   steamID,
		Persona:   persona,
		AvatarURL: avatarURL,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	sig := m.sign([]byte(encodedPayload))
	value := encodedPayload + "." + sig

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: m.sameSite,
		Expires:  claims.expiresTime(),
	})
	return nil
}

func (c Claims) expiresTime() time.Time {
	return time.Unix(c.ExpiresAt, 0)
}

// Verify reads and validates the session cookie from the request, returning
// the claims if present, unexpired, and correctly signed.
func (m *Manager) Verify(r *http.Request) (*Claims, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, errors.New("no session cookie")
	}

	dot := -1
	for i := len(cookie.Value) - 1; i >= 0; i-- {
		if cookie.Value[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return nil, errors.New("malformed session cookie")
	}
	encodedPayload, gotSig := cookie.Value[:dot], cookie.Value[dot+1:]

	wantSig := m.sign([]byte(encodedPayload))
	if !hmac.Equal([]byte(gotSig), []byte(wantSig)) {
		return nil, errors.New("invalid session signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, errors.New("session expired")
	}
	return &claims, nil
}

// Clear removes the session cookie (used for logout).
func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: m.sameSite,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}
