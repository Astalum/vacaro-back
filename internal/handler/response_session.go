package handler

import (
	"net/http"

	"github.com/astalum/vacaro-back/internal/auth"
)

func (a *API) createResponseSession(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	access, err := a.service.ResolveResponseEditToken(r.Context(), request.Token)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	if err := a.sessions.SetResponse(w, auth.Session{
		UserID: access.UserID, AccountType: "response", EventID: access.EventID,
		ExpiresAt: access.ExpiresAt.Unix(),
	}); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, map[string]string{"event_id": access.EventID})
}

func (a *API) getResponseUser(w http.ResponseWriter, r *http.Request) {
	user, err := a.service.GetUser(r.Context(), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, user)
}

func (a *API) issueResponseEditLink(w http.ResponseWriter, r *http.Request) {
	responseEditURL, err := a.service.IssueResponseEditLink(
		r.Context(), r.PathValue("event_id"), currentUserID(r),
	)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"response_edit_url": responseEditURL})
}
