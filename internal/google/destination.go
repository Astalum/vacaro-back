package google

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

func validDestinationCalendarID(id string) bool {
	return strings.TrimSpace(id) != "" && !strings.EqualFold(strings.TrimSpace(id), "primary")
}

// EnsureCalendar reuses only the ID stored for this user. Names are user editable
// and are never used to adopt an existing calendar as the write destination.
func (c *Client) EnsureCalendar(ctx context.Context, refreshToken, calendarID, timeZone string) (string, error) {
	client := c.calendar.Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
	if calendarID != "" {
		if !validDestinationCalendarID(calendarID) {
			return "", errors.New("invalid dedicated calendar ID")
		}
		status, body, err := doCalendarRequest(ctx, client, http.MethodGet, c.calendarAPIBaseURL+"/calendars/"+url.PathEscape(calendarID), nil)
		if err != nil {
			return "", calendarTransportError(err)
		}
		if status >= 200 && status < 300 {
			return calendarID, nil
		}
		// Missing/deleted calendars can be replaced. Permission and temporary errors
		// must not create duplicate calendars or switch to the primary calendar.
		if status != http.StatusNotFound && status != http.StatusGone {
			return "", calendarResponseError(status, body)
		}
	}
	payload, err := json.Marshal(map[string]string{
		"summary": "Vacaro", "description": "Vacaroで確定した予定を登録する専用カレンダーです。", "timeZone": timeZone,
	})
	if err != nil {
		return "", err
	}
	status, body, err := doCalendarRequest(ctx, client, http.MethodPost, c.calendarAPIBaseURL+"/calendars", payload)
	if err != nil {
		return "", calendarTransportError(err)
	}
	if status < 200 || status >= 300 {
		return "", calendarResponseError(status, body)
	}
	var calendar struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &calendar); err != nil {
		return "", errors.New("invalid calendar creation response")
	}
	if !validDestinationCalendarID(calendar.ID) {
		return "", errors.New("calendar creation response has no dedicated ID")
	}
	return calendar.ID, nil
}
