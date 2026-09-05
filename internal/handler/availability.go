package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/astalum/vacaro-back/internal/service"
)

func (a *API) replaceAvailability(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Intervals []struct {
			StartAt time.Time `json:"start_at"`
			EndAt   time.Time `json:"end_at"`
		} `json:"intervals"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	inputs := make([]service.IntervalInput, len(request.Intervals))
	for i, interval := range request.Intervals {
		inputs[i] = service.IntervalInput{StartAt: interval.StartAt, EndAt: interval.EndAt}
	}
	intervals, err := a.service.ReplaceAvailability(r.Context(), r.PathValue("event_id"), currentUserID(r), inputs)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"response_status": "submitted", "intervals": intervals})
}

func (a *API) getOwnAvailability(w http.ResponseWriter, r *http.Request) {
	intervals, status, err := a.service.GetOwnAvailability(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"response_status": status, "intervals": intervals})
}

func (a *API) copyOwnAvailability(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SourceEventID string `json:"source_event_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	intervals, err := a.service.CopyOwnAvailability(r.Context(), r.PathValue("event_id"), request.SourceEventID, currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"response_status": "submitted", "intervals": intervals})
}

func (a *API) listAvailability(w http.ResponseWriter, r *http.Request) {
	availability, err := a.service.ListAvailability(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, availability)
}

func (a *API) listCandidates(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	candidates, err := a.service.Candidates(r.Context(), r.PathValue("event_id"), currentUserID(r), limit)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": candidates, "meta": map[string]int{"limit": limit}})
}
