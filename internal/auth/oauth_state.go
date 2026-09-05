package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const oauthStateCookie = "vacaro_oauth_state"

type OAuthState struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Purpose   string `json:"purpose"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

type OAuthStateManager struct {
	box    *Box
	secure bool
}

func NewOAuthStateManager(encodedKey string, secure bool) (*OAuthStateManager, error) {
	box, err := NewBox(encodedKey)
	if err != nil {
		return nil, err
	}
	return &OAuthStateManager{box: box, secure: secure}, nil
}

func (m *OAuthStateManager) Begin(w http.ResponseWriter, purpose, userID string) (OAuthState, error) {
	state, err := randomURLToken(32)
	if err != nil {
		return OAuthState{}, err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return OAuthState{}, err
	}
	value := OAuthState{State: state, Nonce: nonce, Purpose: purpose, UserID: userID, ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	plain, _ := json.Marshal(value)
	sealed, err := m.box.Seal(plain)
	if err != nil {
		return OAuthState{}, err
	}
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: base64.RawURLEncoding.EncodeToString(sealed), Path: "/api/v1/", MaxAge: 600, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode})
	return value, nil
}

func (m *OAuthStateManager) Consume(w http.ResponseWriter, r *http.Request, expectedState, purpose string) (OAuthState, error) {
	m.clear(w)
	cookie, err := r.Cookie(oauthStateCookie)
	if err != nil {
		return OAuthState{}, errors.New("OAuth state cookie is missing")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return OAuthState{}, errors.New("invalid OAuth state")
	}
	plain, err := m.box.Open(sealed)
	if err != nil {
		return OAuthState{}, errors.New("invalid OAuth state")
	}
	var value OAuthState
	if err := json.Unmarshal(plain, &value); err != nil || time.Now().Unix() >= value.ExpiresAt || value.Purpose != purpose || subtle.ConstantTimeCompare([]byte(value.State), []byte(expectedState)) != 1 {
		return OAuthState{}, errors.New("invalid OAuth state")
	}
	return value, nil
}

func (m *OAuthStateManager) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Path: "/api/v1/", MaxAge: -1, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode})
}

func randomURLToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
