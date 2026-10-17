package handler

import (
	"net/http"

	"github.com/astalum/vacaro-back/internal/auth"
)

func (a *API) createGuestSession(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DisplayName string `json:"display_name"`
		TimeZone    string `json:"time_zone"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	user, err := a.service.CreateGuest(r.Context(), request.DisplayName, request.TimeZone)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	if user.GuestExpiresAt == nil {
		writeAPIError(w, r, auth.ErrInvalidSession)
		return
	}
	if err := a.sessions.Set(w, auth.Session{UserID: user.ID, AccountType: user.AccountType, ExpiresAt: user.GuestExpiresAt.Unix()}); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, user)
}

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	user, err := a.service.GetUser(r.Context(), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, user)
}

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DisplayName string `json:"display_name"`
		TimeZone    string `json:"time_zone"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	user, err := a.service.UpdateUser(r.Context(), currentUserID(r), request.DisplayName, request.TimeZone)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, user)
}

func (a *API) deleteSession(w http.ResponseWriter, _ *http.Request) {
	a.sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}
