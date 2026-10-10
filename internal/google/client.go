package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

const (
	calendarFreeBusyScope = "https://www.googleapis.com/auth/calendar.freebusy"
	calendarListScope     = "https://www.googleapis.com/auth/calendar.calendarlist.readonly"
	CalendarEventsScope   = "https://www.googleapis.com/auth/calendar.events"
	calendarAPIBaseURL    = "https://www.googleapis.com/calendar/v3"
)

type Config struct {
	ClientID            string
	ClientSecret        string
	LoginRedirectURL    string
	CalendarRedirectURL string
}

type Identity struct {
	Subject string
	Email   string
	Name    string
}

type BusyInterval struct {
	StartAt time.Time
	EndAt   time.Time
}

type Calendar struct {
	ID              string
	Summary         string
	Primary         bool
	Selected        bool
	BackgroundColor string
}

type CalendarEvent struct {
	ID          string
	Summary     string
	Description string
	StartAt     time.Time
	EndAt       time.Time
	TimeZone    string
}

type Client struct {
	login              oauth2.Config
	calendar           oauth2.Config
	clientID           string
	calendarAPIBaseURL string
}

func New(config Config) *Client {
	return &Client{
		clientID:           config.ClientID,
		calendarAPIBaseURL: calendarAPIBaseURL,
		login: oauth2.Config{
			ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURL: config.LoginRedirectURL,
			Endpoint: googleoauth.Endpoint, Scopes: []string{"openid", "email", "profile"},
		},
		calendar: oauth2.Config{
			ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURL: config.CalendarRedirectURL,
			Endpoint: googleoauth.Endpoint, Scopes: []string{calendarFreeBusyScope, calendarListScope, CalendarEventsScope},
		},
	}
}

func (c *Client) LoginAuthURL(state, nonce string) string {
	return c.login.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce))
}

func (c *Client) ExchangeIdentity(ctx context.Context, code, expectedNonce string) (Identity, error) {
	token, err := c.login.Exchange(ctx, code)
	if err != nil {
		return Identity{}, fmt.Errorf("exchange Google login code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Identity{}, errors.New("Google response did not contain an ID token")
	}
	payload, err := idtoken.Validate(ctx, rawIDToken, c.clientID)
	if err != nil {
		return Identity{}, fmt.Errorf("validate Google ID token: %w", err)
	}
	nonce, _ := payload.Claims["nonce"].(string)
	if nonce == "" || nonce != expectedNonce {
		return Identity{}, errors.New("Google ID token nonce did not match")
	}
	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	if email == "" || !verified || payload.Subject == "" {
		return Identity{}, errors.New("Google account did not provide a verified identity")
	}
	name, _ := payload.Claims["name"].(string)
	if strings.TrimSpace(name) == "" {
		name = email
	}
	return Identity{Subject: payload.Subject, Email: email, Name: name}, nil
}

func (c *Client) CalendarAuthURL(state string) string {
	return c.calendar.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
}

func (c *Client) ExchangeCalendar(ctx context.Context, code string) (string, []string, error) {
	token, err := c.calendar.Exchange(ctx, code)
	if err != nil {
		return "", nil, fmt.Errorf("exchange Google Calendar code: %w", err)
	}
	if token.RefreshToken == "" {
		return "", nil, errors.New("Google response did not contain a refresh token")
	}
	scopes := []string{calendarFreeBusyScope, calendarListScope, CalendarEventsScope}
	if grantedScopes, ok := token.Extra("scope").(string); ok && strings.TrimSpace(grantedScopes) != "" {
		scopes = strings.Fields(grantedScopes)
	}
	return token.RefreshToken, scopes, nil
}

func (c *Client) ListCalendars(ctx context.Context, refreshToken string) ([]Calendar, error) {
	client := c.calendar.Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
	calendars := make([]Calendar, 0)
	pageToken := ""
	for {
		endpoint, _ := url.Parse("https://www.googleapis.com/calendar/v3/users/me/calendarList")
		query := endpoint.Query()
		query.Set("minAccessRole", "reader")
		query.Set("showDeleted", "false")
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("list Google calendars: %w", err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			return nil, fmt.Errorf("Google Calendar list returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
		}
		var result struct {
			Items []struct {
				ID              string `json:"id"`
				Summary         string `json:"summary"`
				Primary         bool   `json:"primary"`
				Selected        bool   `json:"selected"`
				BackgroundColor string `json:"backgroundColor"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			response.Body.Close()
			return nil, err
		}
		response.Body.Close()
		for _, item := range result.Items {
			calendars = append(calendars, Calendar{
				ID: item.ID, Summary: item.Summary, Primary: item.Primary,
				Selected: item.Selected, BackgroundColor: item.BackgroundColor,
			})
		}
		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return calendars, nil
}

func (c *Client) FreeBusy(ctx context.Context, refreshToken string, calendarIDs []string, startAt, endAt time.Time, timeZone string) ([]BusyInterval, error) {
	items := make([]map[string]string, len(calendarIDs))
	for i, id := range calendarIDs {
		items[i] = map[string]string{"id": id}
	}
	payload, err := json.Marshal(map[string]any{
		"timeMin": startAt.Format(time.RFC3339), "timeMax": endAt.Format(time.RFC3339),
		"timeZone": timeZone, "items": items,
	})
	if err != nil {
		return nil, err
	}
	client := c.calendar.Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/calendar/v3/freeBusy", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("query Google Calendar FreeBusy: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Google Calendar FreeBusy returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Calendars map[string]struct {
			Busy []struct {
				Start time.Time `json:"start"`
				End   time.Time `json:"end"`
			} `json:"busy"`
			Errors []any `json:"errors"`
		} `json:"calendars"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	busy := make([]BusyInterval, 0)
	for _, calendarID := range calendarIDs {
		calendar, ok := result.Calendars[calendarID]
		if !ok || len(calendar.Errors) > 0 {
			return nil, fmt.Errorf("Google Calendar did not return free/busy data for %q", calendarID)
		}
		for _, interval := range calendar.Busy {
			busy = append(busy, BusyInterval{StartAt: interval.Start.UTC(), EndAt: interval.End.UTC()})
		}
	}
	return busy, nil
}

func (c *Client) UpsertCalendarEvent(ctx context.Context, refreshToken string, event CalendarEvent) error {
	payload, err := json.Marshal(map[string]any{
		"id":          event.ID,
		"summary":     event.Summary,
		"description": event.Description,
		"start": map[string]string{
			"dateTime": event.StartAt.Format(time.RFC3339),
			"timeZone": event.TimeZone,
		},
		"end": map[string]string{
			"dateTime": event.EndAt.Format(time.RFC3339),
			"timeZone": event.TimeZone,
		},
	})
	if err != nil {
		return err
	}
	client := c.calendar.Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
	endpoint := c.calendarAPIBaseURL + "/calendars/primary/events?sendUpdates=none"
	status, responseBody, err := doCalendarRequest(ctx, client, http.MethodPost, endpoint, payload)
	if err != nil {
		return fmt.Errorf("create Google Calendar event: %w", err)
	}
	if status == http.StatusConflict {
		endpoint = c.calendarAPIBaseURL + "/calendars/primary/events/" + url.PathEscape(event.ID) + "?sendUpdates=none"
		status, responseBody, err = doCalendarRequest(ctx, client, http.MethodPut, endpoint, payload)
		if err != nil {
			return fmt.Errorf("update Google Calendar event: %w", err)
		}
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("Google Calendar event upsert returned %d: %s", status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (c *Client) DeleteCalendarEvent(ctx context.Context, refreshToken, eventID string) error {
	client := c.calendar.Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
	endpoint := c.calendarAPIBaseURL + "/calendars/primary/events/" + url.PathEscape(eventID) + "?sendUpdates=none"
	status, responseBody, err := doCalendarRequest(ctx, client, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("delete Google Calendar event: %w", err)
	}
	if status == http.StatusNotFound || status == http.StatusGone {
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("Google Calendar event deletion returned %d: %s", status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func doCalendarRequest(ctx context.Context, client *http.Client, method, endpoint string, payload []byte) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, responseBody, nil
}

func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	form := url.Values{"token": []string{refreshToken}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Google token revocation returned %d", response.StatusCode)
	}
	return nil
}
