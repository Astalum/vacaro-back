package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/astalum/vacaro-back/internal/service"
)

func (a *API) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := a.service.ListGroups(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, groups)
}

func (a *API) createGroup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name            string   `json:"name"`
		DurationMinutes *int     `json:"duration_minutes"`
		ParticipantIDs  []string `json:"participant_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	group, err := a.service.CreateGroup(r.Context(), r.PathValue("event_id"), currentUserID(r), service.CreateGroupInput{
		Name: request.Name, DurationMinutes: request.DurationMinutes, ParticipantIDs: request.ParticipantIDs,
	})
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, group)
}

func (a *API) getGroup(w http.ResponseWriter, r *http.Request) {
	group, err := a.service.GetGroup(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, group)
}

func (a *API) updateGroup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name            *string `json:"name"`
		DurationMinutes *int    `json:"duration_minutes"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	group, err := a.service.UpdateGroup(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r), service.UpdateGroupInput{
		Name: request.Name, DurationMinutes: request.DurationMinutes,
	})
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, group)
}

func (a *API) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := a.service.DeleteGroup(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r)); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) replaceGroupMembers(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ParticipantIDs []string `json:"participant_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	group, err := a.service.ReplaceGroupMembers(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r), request.ParticipantIDs)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, group)
}

func (a *API) listGroupAvailability(w http.ResponseWriter, r *http.Request) {
	availability, err := a.service.ListGroupAvailability(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, availability)
}

func (a *API) listGroupCandidates(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	candidates, err := a.service.GroupCandidates(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r), limit)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": candidates, "meta": map[string]int{"limit": limit}})
}

func (a *API) confirmGroup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		StartAt time.Time `json:"start_at"`
		EndAt   time.Time `json:"end_at"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	group, err := a.service.ConfirmGroup(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r), request.StartAt, request.EndAt)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, group)
}

func (a *API) reopenGroup(w http.ResponseWriter, r *http.Request) {
	group, err := a.service.ReopenGroup(r.Context(), r.PathValue("event_id"), r.PathValue("group_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, group)
}
