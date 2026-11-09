package handler

import (
	"net/http"
)

func (a *API) connectICloud(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Account  string `json:"apple_account"`
		Password string `json:"app_password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_json", "message": "JSONの形式を確認してください"}})
		return
	}
	if err := a.service.ConnectICloud(r.Context(), currentUserID(r), input.Account, input.Password); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, 200, map[string]bool{"connected": true})
}
func (a *API) listICloudCalendars(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	result, err := a.service.ListICloudCalendars(r.Context(), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, 200, result)
}
func (a *API) disconnectICloud(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := a.service.DisconnectICloud(r.Context(), currentUserID(r)); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (a *API) previewICloud(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		CalendarIDs []string `json:"calendar_ids"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_json", "message": "JSONの形式を確認してください"}})
		return
	}
	result, err := a.service.PreviewICloudAvailability(r.Context(), r.PathValue("event_id"), currentUserID(r), input.CalendarIDs)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, 200, map[string]any{"intervals": result})
}
func (a *API) getICloudCalendarStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	result, err := a.service.GetICloudCalendarStatus(r.Context(), r.PathValue("event_id"), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, 200, result)
}
func (a *API) syncICloudCalendar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		GroupID *string `json:"group_id"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_json", "message": "JSONの形式を確認してください"}})
		return
	}
	result, err := a.service.SyncOwnICloudCalendar(r.Context(), r.PathValue("event_id"), input.GroupID, currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, 200, result)
}
