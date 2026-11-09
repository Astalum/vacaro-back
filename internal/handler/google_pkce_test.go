package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/auth"
	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/service"
	"golang.org/x/oauth2"
)

type pkceProvider struct {
	service.GoogleProvider
	client            *googleclient.Client
	exchangedVerifier string
}

func (p *pkceProvider) LoginAuthURL(state, nonce, verifier string) string {
	return p.client.LoginAuthURL(state, nonce, verifier)
}
func (p *pkceProvider) CalendarAuthURL(state, verifier string) string {
	return p.client.CalendarAuthURL(state, verifier)
}
func (p *pkceProvider) ExchangeIdentity(_ context.Context, _, _, verifier string) (googleclient.Identity, error) {
	p.exchangedVerifier = verifier
	return googleclient.Identity{}, errors.New("fixture exchange failure")
}
func (p *pkceProvider) ExchangeCalendar(_ context.Context, _, verifier string) (string, []string, error) {
	p.exchangedVerifier = verifier
	return "", nil, errors.New("fixture exchange failure")
}

func TestOAuthRoutesKeepVerifierInSealedCookie(t *testing.T) {
	for _, calendar := range []bool{false, true} {
		name := "login"
		if calendar {
			name = "calendar"
		}
		t.Run(name, func(t *testing.T) {
			sessions, _ := auth.NewManager(googleHandlerTestKey, false)
			states, _ := auth.NewOAuthStateManager(googleHandlerTestKey, false)
			box, _ := auth.NewBox(googleHandlerTestKey)
			provider := &pkceProvider{client: googleclient.New(googleclient.Config{ClientID: "test", ClientSecret: "secret", LoginRedirectURL: "https://app.example/api/v1/auth/google/callback", CalendarRedirectURL: "https://app.example/api/v1/integrations/google-calendar/callback"})}
			application := service.New(pkceCalendarStore{}, time.Hour, "https://app.example")
			application.ConfigureGoogle(provider, box, time.Hour)
			api := NewAPI(application, sessions, states, "https://app.example")
			mux := http.NewServeMux()
			api.Register(mux)
			method, path, purpose := http.MethodGet, "/api/v1/auth/google/start", googleLoginPurpose
			var sessionCookie *http.Cookie
			if calendar {
				method, path, purpose = http.MethodPost, "/api/v1/integrations/google-calendar", googleCalendarPurpose
				cookieResponse := httptest.NewRecorder()
				_ = sessions.Set(cookieResponse, auth.Session{UserID: "user", AccountType: "registered", ExpiresAt: time.Now().Add(time.Hour).Unix()})
				sessionCookie = cookieResponse.Result().Cookies()[0]
			}
			request := httptest.NewRequest(method, path, nil)
			if sessionCookie != nil {
				request.AddCookie(sessionCookie)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			authorizationURL := response.Header().Get("Location")
			if calendar {
				if response.Code != http.StatusOK {
					t.Fatalf("start status=%d", response.Code)
				}
				var body struct {
					Data struct {
						URL string `json:"authorization_url"`
					} `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				authorizationURL = body.Data.URL
			} else if response.Code != http.StatusSeeOther {
				t.Fatalf("start status=%d", response.Code)
			}
			u, err := url.Parse(authorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			if q.Get("state") == "" || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
				t.Fatal("secure authorization parameters missing")
			}
			if q.Has("code_verifier") || q.Has("client_secret") {
				t.Fatal("secrets exposed in authorization URL")
			}
			callbackPath := "/api/v1/auth/google/callback"
			if calendar {
				callbackPath = "/api/v1/integrations/google-calendar/callback"
			}
			callback := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+url.QueryEscape(q.Get("state"))+"&code=one-use-code&code_verifier=attacker-controlled", nil)
			for _, cookie := range response.Result().Cookies() {
				callback.AddCookie(cookie)
			}
			if sessionCookie != nil {
				callback.AddCookie(sessionCookie)
			}
			state, err := states.Consume(httptest.NewRecorder(), callback, q.Get("state"), purpose)
			if err != nil {
				t.Fatal(err)
			}
			if state.CodeVerifier == state.Nonce || state.CodeVerifier == state.State || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(state.CodeVerifier) {
				t.Fatal("challenge is not bound to the independently generated cookie verifier")
			}
			mux.ServeHTTP(httptest.NewRecorder(), callback)
			if provider.exchangedVerifier != state.CodeVerifier {
				t.Fatal("callback did not use verifier from the sealed cookie")
			}
		})
	}
}

type pkceCalendarStore struct{ service.Store }

func (pkceCalendarStore) GetCalendarProvider(context.Context, string) (string, error) {
	return "google", nil
}
func (pkceCalendarStore) WithCalendarLock(_ context.Context, _ string, fn func() error) error {
	return fn()
}
func (pkceCalendarStore) GetUser(context.Context, string) (domain.User, error) {
	return domain.User{ID: "user", AccountType: "registered", TimeZone: "Asia/Tokyo"}, nil
}
