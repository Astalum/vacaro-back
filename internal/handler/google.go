package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/astalum/vacaro-back/internal/auth"
	"github.com/astalum/vacaro-back/internal/domain"
	appmiddleware "github.com/astalum/vacaro-back/internal/middleware"
)

const (
	googleLoginPurpose    = "google_login"
	googleCalendarPurpose = "google_calendar"
)

func (a *API) startGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.service.GoogleEnabled() {
		writeAPIError(w, r, domain.ErrGoogleNotConfigured)
		return
	}
	currentUserID := ""
	if session, err := a.sessions.Read(r); err == nil && session.AccountType == "guest" {
		currentUserID = session.UserID
	}
	state, err := a.oauthStates.Begin(w, googleLoginPurpose, currentUserID)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	authorizationURL, err := a.service.GoogleLoginURL(state.State, state.Nonce)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
}

func (a *API) completeGoogleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := a.consumeGoogleCallback(w, r, googleLoginPurpose)
	if err != nil {
		callbackWasSuccessful := strings.TrimSpace(r.URL.Query().Get("code")) != "" &&
			strings.TrimSpace(r.URL.Query().Get("error")) == ""
		if session, sessionErr := a.sessions.Read(r); callbackWasSuccessful &&
			sessionErr == nil && session.AccountType == "registered" {
			a.redirectGoogleLoginResult(w, r, "success")
			return
		}
		a.redirectGoogleLoginError(w, r, err)
		return
	}
	user, expiresAt, err := a.service.CompleteGoogleLogin(r.Context(), r.URL.Query().Get("code"), state.Nonce, state.UserID)
	if err != nil {
		a.redirectGoogleLoginError(w, r, err)
		return
	}
	if err := a.sessions.Set(w, auth.Session{UserID: user.ID, AccountType: user.AccountType, ExpiresAt: expiresAt.Unix()}); err != nil {
		a.redirectGoogleLoginError(w, r, err)
		return
	}
	a.redirectGoogleLoginResult(w, r, "success")
}

func (a *API) redirectGoogleLoginError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Warn("Google login callback failed", "error", err, "request_id", appmiddleware.RequestIDFromContext(r.Context()))
	a.redirectGoogleLoginResult(w, r, "error")
}

func (a *API) redirectGoogleLoginResult(w http.ResponseWriter, r *http.Request, result string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, appendResult(a.publicAppURL, "google_login", result), http.StatusSeeOther)
}

func (a *API) startGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	if !a.service.GoogleEnabled() {
		writeAPIError(w, r, domain.ErrGoogleNotConfigured)
		return
	}
	session, _ := auth.FromContext(r.Context())
	if session.AccountType != "registered" {
		writeAPIError(w, r, domain.ErrPermissionDenied)
		return
	}
	state, err := a.oauthStates.Begin(w, googleCalendarPurpose, session.UserID)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	authorizationURL, err := a.service.GoogleCalendarURL(state.State)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"authorization_url": authorizationURL})
}

func (a *API) completeGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	state, err := a.consumeGoogleCallback(w, r, googleCalendarPurpose)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	session, err := a.sessions.Read(r)
	if err != nil || session.AccountType != "registered" || session.UserID != state.UserID {
		writeAPIError(w, r, domain.ErrPermissionDenied)
		return
	}
	if err := a.service.CompleteGoogleCalendar(r.Context(), state.UserID, r.URL.Query().Get("code")); err != nil {
		writeAPIError(w, r, err)
		return
	}
	http.Redirect(w, r, appendResult(a.publicAppURL, "google_calendar", "connected"), http.StatusSeeOther)
}

func (a *API) disconnectGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	session, _ := auth.FromContext(r.Context())
	if session.AccountType != "registered" {
		writeAPIError(w, r, domain.ErrPermissionDenied)
		return
	}
	if err := a.service.DisconnectGoogleCalendar(r.Context(), currentUserID(r)); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listGoogleCalendars(w http.ResponseWriter, r *http.Request) {
	session, _ := auth.FromContext(r.Context())
	if session.AccountType != "registered" {
		writeAPIError(w, r, domain.ErrPermissionDenied)
		return
	}
	calendars, err := a.service.ListGoogleCalendars(r.Context(), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	type calendarResponse struct {
		ID              string `json:"id"`
		Summary         string `json:"summary"`
		Primary         bool   `json:"primary"`
		Selected        bool   `json:"selected"`
		BackgroundColor string `json:"background_color,omitempty"`
	}
	items := make([]calendarResponse, len(calendars))
	for i, calendar := range calendars {
		items[i] = calendarResponse{
			ID: calendar.ID, Summary: calendar.Summary, Primary: calendar.Primary,
			Selected: calendar.Selected, BackgroundColor: calendar.BackgroundColor,
		}
	}
	writeData(w, http.StatusOK, map[string]any{"calendars": items})
}

func (a *API) previewGoogleCalendarAvailability(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CalendarIDs []string `json:"calendar_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	intervals, err := a.service.PreviewGoogleCalendarAvailability(r.Context(), r.PathValue("event_id"), currentUserID(r), request.CalendarIDs)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	preview := make([]struct {
		StartAt time.Time `json:"start_at"`
		EndAt   time.Time `json:"end_at"`
		Source  string    `json:"source"`
	}, len(intervals))
	for i, interval := range intervals {
		preview[i].StartAt = interval.StartAt
		preview[i].EndAt = interval.EndAt
		preview[i].Source = interval.Source
	}
	writeData(w, http.StatusOK, map[string]any{"intervals": preview})
}

func (a *API) consumeGoogleCallback(w http.ResponseWriter, r *http.Request, purpose string) (auth.OAuthState, error) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		return auth.OAuthState{}, fmt.Errorf("%w: callback state is missing", domain.ErrOAuthFailed)
	}
	value, err := a.oauthStates.Consume(w, r, state, purpose)
	if err != nil {
		return auth.OAuthState{}, fmt.Errorf("%w: %v", domain.ErrOAuthFailed, err)
	}
	if providerError := strings.TrimSpace(r.URL.Query().Get("error")); providerError != "" {
		return auth.OAuthState{}, fmt.Errorf("%w: provider returned %s", domain.ErrOAuthFailed, providerError)
	}
	if strings.TrimSpace(r.URL.Query().Get("code")) == "" {
		return auth.OAuthState{}, fmt.Errorf("%w: callback code is missing", domain.ErrOAuthFailed)
	}
	return value, nil
}

func appendResult(rawURL, key, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
