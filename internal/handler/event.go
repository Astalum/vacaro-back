package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/astalum/vacaro-back/internal/service"
)

type eventRequest struct {
	Title                        string     `json:"title"`
	Description                  *string    `json:"description"`
	WindowStartAt                time.Time  `json:"window_start_at"`
	WindowEndAt                  time.Time  `json:"window_end_at"`
	DurationMinutes              int        `json:"duration_minutes"`
	SchedulingMode               string     `json:"scheduling_mode"`
	ResponseDeadlineAt           *time.Time `json:"response_deadline_at"`
	ResultsVisibleToParticipants *bool      `json:"results_visible_to_participants"`
}

func (a *API) createEvent(w http.ResponseWriter, r *http.Request) {
	var request eventRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	result, err := a.service.CreateEvent(r.Context(), currentUserID(r), service.CreateEventInput{
		Title: request.Title, Description: request.Description,
		WindowStartAt: request.WindowStartAt, WindowEndAt: request.WindowEndAt,
		DurationMinutes: request.DurationMinutes, SchedulingMode: request.SchedulingMode,
		ResponseDeadlineAt:           request.ResponseDeadlineAt,
		ResultsVisibleToParticipants: request.ResultsVisibleToParticipants,
	})
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := a.service.ListEvents(r.Context(), currentUserID(r), r.URL.Query().Get("status"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": page.Events, "meta": map[string]any{"next_cursor": page.NextCursor}})
}

func (a *API) getEvent(w http.ResponseWriter, r *http.Request) {
	event, err := a.service.GetEvent(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, event)
}

func (a *API) getInvitation(w http.ResponseWriter, r *http.Request) {
	event, err := a.service.GetInvitation(r.Context(), r.PathValue("invite_token"))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	event.CreatedByUserID = ""
	writeData(w, http.StatusOK, event)
}

func (a *API) updateEvent(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title              *string             `json:"title"`
		Description        optional[string]    `json:"description"`
		WindowStartAt      *time.Time          `json:"window_start_at"`
		WindowEndAt        *time.Time          `json:"window_end_at"`
		DurationMinutes    *int                `json:"duration_minutes"`
		SchedulingMode     *string             `json:"scheduling_mode"`
		ResponseDeadlineAt optional[time.Time] `json:"response_deadline_at"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	var description *string
	if request.Description.Set && !request.Description.Null {
		description = &request.Description.Value
	}
	var deadline *time.Time
	if request.ResponseDeadlineAt.Set && !request.ResponseDeadlineAt.Null {
		deadline = &request.ResponseDeadlineAt.Value
	}
	event, err := a.service.UpdateEvent(r.Context(), r.PathValue("event_id"), currentUserID(r), service.UpdateEventInput{
		Title: request.Title, Description: description, DescriptionSet: request.Description.Set,
		WindowStartAt: request.WindowStartAt, WindowEndAt: request.WindowEndAt,
		DurationMinutes: request.DurationMinutes, SchedulingMode: request.SchedulingMode,
		ResponseDeadlineAt: deadline,
		DeadlineSet:        request.ResponseDeadlineAt.Set,
	})
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, event)
}

func (a *API) updateResultsVisibility(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Visible *bool `json:"visible"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	if request.Visible == nil {
		writeAPIError(w, r, &service.ValidationError{Fields: map[string]string{
			"visible": "公開するかどうかを指定してください",
		}})
		return
	}
	event, err := a.service.UpdateResultsVisibility(
		r.Context(), r.PathValue("event_id"), currentUserID(r), *request.Visible,
	)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, event)
}

func (a *API) cancelEvent(w http.ResponseWriter, r *http.Request) {
	if err := a.service.CancelEvent(r.Context(), r.PathValue("event_id"), currentUserID(r)); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) rotateInviteToken(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.RotateInviteToken(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (a *API) joinEvent(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	participant, err := a.service.JoinEvent(r.Context(), r.PathValue("invite_token"), currentUserID(r), request.DisplayName)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, participant)
}

func (a *API) listParticipants(w http.ResponseWriter, r *http.Request) {
	participants, err := a.service.ListParticipants(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, participants)
}

func (a *API) leaveEvent(w http.ResponseWriter, r *http.Request) {
	if err := a.service.LeaveEvent(r.Context(), r.PathValue("event_id"), currentUserID(r)); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) confirmEvent(w http.ResponseWriter, r *http.Request) {
	var request struct {
		StartAt time.Time `json:"start_at"`
		EndAt   time.Time `json:"end_at"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	event, err := a.service.ConfirmEvent(r.Context(), r.PathValue("event_id"), currentUserID(r), request.StartAt, request.EndAt)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, event)
}

func (a *API) reopenEvent(w http.ResponseWriter, r *http.Request) {
	event, err := a.service.ReopenEvent(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, event)
}

func (a *API) getConfirmationCalendar(w http.ResponseWriter, r *http.Request) {
	status, err := a.service.GetConfirmationCalendarStatus(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, status)
}

func (a *API) syncConfirmationCalendar(w http.ResponseWriter, r *http.Request) {
	var request struct {
		GroupID *string `json:"group_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	result, err := a.service.SyncOwnConfirmationCalendar(
		r.Context(), r.PathValue("event_id"), request.GroupID, currentUserID(r),
	)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}
