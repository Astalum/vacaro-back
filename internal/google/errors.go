package google

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

// CalendarError deliberately omits provider response bodies, tokens and calendar IDs.
// Its bounded fields are safe to include in diagnostic logs.
type CalendarError struct {
	Kind   string
	Status int
}

func (e *CalendarError) Error() string {
	return fmt.Sprintf("Google Calendar %s (status %d)", e.Kind, e.Status)
}

func calendarTransportError(err error) error {
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) && retrieve.ErrorCode == "invalid_grant" {
		return &CalendarError{Kind: "reauthorization_required", Status: http.StatusUnauthorized}
	}
	return &CalendarError{Kind: "temporarily_unavailable"}
}

func calendarResponseError(status int, body []byte) error {
	var result struct {
		Error struct {
			Status string `json:"status"`
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &result)
	kind := "temporarily_unavailable"
	switch status {
	case http.StatusUnauthorized:
		kind = "reauthorization_required"
	case http.StatusForbidden:
		kind = "permission_required"
		for _, item := range result.Error.Errors {
			if item.Reason == "accessNotConfigured" {
				kind = "api_disabled"
			}
			if item.Reason == "rateLimitExceeded" || item.Reason == "userRateLimitExceeded" {
				kind = "temporarily_unavailable"
			}
		}
		for _, item := range result.Error.Details {
			if item.Reason == "SERVICE_DISABLED" {
				kind = "api_disabled"
			}
		}
	case http.StatusNotFound:
		kind = "calendar_unavailable"
	}
	return &CalendarError{Kind: kind, Status: status}
}
