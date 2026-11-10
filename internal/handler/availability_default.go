package handler

import "net/http"

func (a *API) setAvailabilityDefault(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AvailabilityDefault string `json:"availability_default"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	user, err := a.service.SetAvailabilityDefault(r.Context(), currentUserID(r), request.AvailabilityDefault)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, user)
}
