package handler

import (
	"net/http"

	"github.com/astalum/vacaro-back/internal/auth"
	appmiddleware "github.com/astalum/vacaro-back/internal/middleware"
	"github.com/astalum/vacaro-back/internal/service"
)

type API struct {
	service      *service.Service
	sessions     *auth.Manager
	oauthStates  *auth.OAuthStateManager
	publicAppURL string
}

func NewAPI(service *service.Service, sessions *auth.Manager, oauthStates *auth.OAuthStateManager, publicAppURL string) *API {
	return &API{service: service, sessions: sessions, oauthStates: oauthStates, publicAppURL: publicAppURL}
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/guest-sessions", a.createGuestSession)
	mux.HandleFunc("GET /api/v1/invitations/{invite_token}", a.getInvitation)
	mux.HandleFunc("GET /api/v1/auth/google/start", a.startGoogleLogin)
	mux.HandleFunc("GET /api/v1/auth/google/callback", a.completeGoogleLogin)
	mux.HandleFunc("GET /api/v1/integrations/google-calendar/callback", a.completeGoogleCalendar)

	authenticated := func(handler http.HandlerFunc) http.Handler {
		return appmiddleware.Authenticate(a.sessions, handler)
	}
	mux.Handle("GET /api/v1/me", authenticated(a.getMe))
	mux.Handle("PATCH /api/v1/me", authenticated(a.updateMe))
	mux.Handle("DELETE /api/v1/session", authenticated(a.deleteSession))
	mux.Handle("POST /api/v1/events", authenticated(a.createEvent))
	mux.Handle("GET /api/v1/events", authenticated(a.listEvents))
	mux.Handle("GET /api/v1/events/{event_id}", authenticated(a.getEvent))
	mux.Handle("PATCH /api/v1/events/{event_id}", authenticated(a.updateEvent))
	mux.Handle("PATCH /api/v1/events/{event_id}/results-visibility", authenticated(a.updateResultsVisibility))
	mux.Handle("DELETE /api/v1/events/{event_id}", authenticated(a.cancelEvent))
	mux.Handle("POST /api/v1/events/{event_id}/invite-token", authenticated(a.rotateInviteToken))
	mux.Handle("PUT /api/v1/events/{event_id}/confirmation", authenticated(a.confirmEvent))
	mux.Handle("DELETE /api/v1/events/{event_id}/confirmation", authenticated(a.reopenEvent))
	mux.Handle("GET /api/v1/events/{event_id}/groups", authenticated(a.listGroups))
	mux.Handle("POST /api/v1/events/{event_id}/groups", authenticated(a.createGroup))
	mux.Handle("GET /api/v1/events/{event_id}/groups/{group_id}", authenticated(a.getGroup))
	mux.Handle("PATCH /api/v1/events/{event_id}/groups/{group_id}", authenticated(a.updateGroup))
	mux.Handle("DELETE /api/v1/events/{event_id}/groups/{group_id}", authenticated(a.deleteGroup))
	mux.Handle("PUT /api/v1/events/{event_id}/groups/{group_id}/members", authenticated(a.replaceGroupMembers))
	mux.Handle("GET /api/v1/events/{event_id}/groups/{group_id}/availability", authenticated(a.listGroupAvailability))
	mux.Handle("GET /api/v1/events/{event_id}/groups/{group_id}/candidates", authenticated(a.listGroupCandidates))
	mux.Handle("PUT /api/v1/events/{event_id}/groups/{group_id}/confirmation", authenticated(a.confirmGroup))
	mux.Handle("DELETE /api/v1/events/{event_id}/groups/{group_id}/confirmation", authenticated(a.reopenGroup))
	mux.Handle("POST /api/v1/invitations/{invite_token}/participants", authenticated(a.joinEvent))
	mux.Handle("GET /api/v1/events/{event_id}/participants", authenticated(a.listParticipants))
	mux.Handle("DELETE /api/v1/events/{event_id}/participants/me", authenticated(a.leaveEvent))
	mux.Handle("GET /api/v1/events/{event_id}/my-availability", authenticated(a.getOwnAvailability))
	mux.Handle("PUT /api/v1/events/{event_id}/my-availability", authenticated(a.replaceAvailability))
	mux.Handle("POST /api/v1/events/{event_id}/my-availability/copy", authenticated(a.copyOwnAvailability))
	mux.Handle("GET /api/v1/events/{event_id}/availability", authenticated(a.listAvailability))
	mux.Handle("GET /api/v1/events/{event_id}/candidates", authenticated(a.listCandidates))
	mux.Handle("POST /api/v1/integrations/google-calendar", authenticated(a.startGoogleCalendar))
	mux.Handle("GET /api/v1/integrations/google-calendar/calendars", authenticated(a.listGoogleCalendars))
	mux.Handle("DELETE /api/v1/integrations/google-calendar", authenticated(a.disconnectGoogleCalendar))
	mux.Handle("POST /api/v1/events/{event_id}/google-calendar-availability/preview", authenticated(a.previewGoogleCalendarAvailability))
}

func currentUserID(r *http.Request) string {
	session, _ := auth.FromContext(r.Context())
	return session.UserID
}
