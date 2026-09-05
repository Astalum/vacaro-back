package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/auth"
)

func TestRequestID(t *testing.T) {
	var contextID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "client-request-id")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if contextID != "client-request-id" || response.Header().Get("X-Request-ID") != contextID {
		t.Fatalf("request IDs = context:%q header:%q", contextID, response.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDGeneratesValue(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if len(response.Header().Get("X-Request-ID")) != 32 {
		t.Fatalf("generated request ID = %q", response.Header().Get("X-Request-ID"))
	}
}

func TestRecover(t *testing.T) {
	handler := RequestID(Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body.Error.Code != "internal_error" {
		t.Errorf("error code = %q", body.Error.Code)
	}
}

func TestAuthenticate(t *testing.T) {
	manager := testSessionManager(t)
	handler := Authenticate(manager, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.FromContext(r.Context())
		if !ok || session.UserID != "user-id" {
			t.Errorf("session = %#v, %v", session, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("missing cookie", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid cookie", func(t *testing.T) {
		cookieResponse := httptest.NewRecorder()
		_ = manager.Set(cookieResponse, auth.Session{UserID: "user-id", AccountType: "guest", ExpiresAt: time.Now().Add(time.Hour).Unix()})
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(cookieResponse.Result().Cookies()[0])
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	})
}

func TestAccessLogPreservesStatus(t *testing.T) {
	handler := AccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/items", nil))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
}

func testSessionManager(t *testing.T) *auth.Manager {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, err := auth.NewManager(key, false)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
