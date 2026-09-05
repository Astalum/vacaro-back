package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const CookieName = "vacaro_session"

var ErrInvalidSession = errors.New("invalid session")

type Session struct {
	UserID      string `json:"user_id"`
	AccountType string `json:"account_type"`
	ExpiresAt   int64  `json:"expires_at"`
}

type contextKey struct{}

type Manager struct {
	aead   cipher.AEAD
	secure bool
}

func NewManager(encodedKey string, secure bool) (*Manager, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode session key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("session key must decode to 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create session cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create session AEAD: %w", err)
	}

	return &Manager{aead: aead, secure: secure}, nil
}

func (m *Manager) Set(w http.ResponseWriter, session Session) error {
	plaintext, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}

	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate session nonce: %w", err)
	}

	value := m.aead.Seal(nonce, nonce, plaintext, nil)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    base64.RawURLEncoding.EncodeToString(value),
		Path:     "/",
		Expires:  time.Unix(session.ExpiresAt, 0),
		MaxAge:   int(time.Until(time.Unix(session.ExpiresAt, 0)).Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) Read(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, ErrInvalidSession
	}

	sealed, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(sealed) < m.aead.NonceSize() {
		return Session{}, ErrInvalidSession
	}

	nonce, ciphertext := sealed[:m.aead.NonceSize()], sealed[m.aead.NonceSize():]
	plaintext, err := m.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return Session{}, ErrInvalidSession
	}

	var session Session
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return Session{}, ErrInvalidSession
	}
	if session.UserID == "" || session.AccountType == "" || time.Now().Unix() >= session.ExpiresAt {
		return Session{}, ErrInvalidSession
	}

	return session, nil
}

func WithSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, contextKey{}, session)
}

func FromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(contextKey{}).(Session)
	return session, ok
}
