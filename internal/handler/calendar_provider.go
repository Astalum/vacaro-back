package handler

import "net/http"

func (a *API) getCalendarPreference(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.GetCalendarPreference(r.Context(), currentUserID(r))
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}
func (a *API) setCalendarPreference(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Provider string `json:"provider"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	result, err := a.service.SetCalendarPreference(r.Context(), currentUserID(r), request.Provider)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}
