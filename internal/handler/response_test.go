package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/astalum/vacaro-back/internal/service"
)

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid", body: `{"name":"vacaro"}`},
		{name: "unknown field", body: `{"name":"vacaro","extra":true}`, wantErr: true},
		{name: "multiple values", body: `{"name":"vacaro"} {"name":"second"}`, wantErr: true},
		{name: "malformed", body: `{"name":`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			var destination struct {
				Name string `json:"name"`
			}
			err := decodeJSON(response, request, &destination)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeJSONRejectsLargeBody(t *testing.T) {
	body := `{"name":"` + strings.Repeat("a", maxRequestBody) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	response := httptest.NewRecorder()
	var destination struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(response, request, &destination); err == nil {
		t.Fatal("decodeJSON() error = nil, want error")
	}
}

func TestWriteAPIError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "validation", err: &service.ValidationError{Fields: map[string]string{"title": "required"}}, wantStatus: 422, wantCode: "validation_failed"},
		{name: "not found", err: domain.ErrNotFound, wantStatus: 404, wantCode: "resource_not_found"},
		{name: "permission", err: domain.ErrPermissionDenied, wantStatus: 403, wantCode: "permission_denied"},
		{name: "already joined", err: domain.ErrAlreadyJoined, wantStatus: 409, wantCode: "already_joined"},
		{name: "closed", err: domain.ErrEventNotOpen, wantStatus: 409, wantCode: "event_not_open"},
		{name: "response deadline passed", err: domain.ErrResponseDeadlinePassed, wantStatus: 409, wantCode: "response_deadline_passed"},
		{name: "organizer leave", err: domain.ErrOrganizerCannotLeave, wantStatus: 409, wantCode: "organizer_cannot_leave"},
		{name: "transition", err: domain.ErrInvalidTransition, wantStatus: 409, wantCode: "invalid_event_transition"},
		{name: "group not editable", err: domain.ErrGroupNotEditable, wantStatus: 409, wantCode: "group_not_editable"},
		{name: "group confirmation exists", err: domain.ErrGroupConfirmationExists, wantStatus: 409, wantCode: "group_confirmation_exists"},
		{name: "duplicate group name", err: domain.ErrDuplicateGroupName, wantStatus: 422, wantCode: "validation_failed"},
		{name: "google disabled", err: domain.ErrGoogleNotConfigured, wantStatus: 503, wantCode: "google_not_configured"},
		{name: "calendar disconnected", err: domain.ErrCalendarNotConnected, wantStatus: 409, wantCode: "calendar_not_connected"},
		{name: "calendar write permission", err: domain.ErrCalendarWritePermission, wantStatus: 409, wantCode: "calendar_write_permission_required"},
		{name: "calendar sync failed", err: domain.ErrCalendarSyncFailed, wantStatus: 502, wantCode: "calendar_sync_failed"},
		{name: "oauth failed", err: domain.ErrOAuthFailed, wantStatus: 400, wantCode: "oauth_failed"},
		{name: "Google authentication required", err: domain.ErrGoogleAuthRequired, wantStatus: 403, wantCode: "google_authentication_required"},
		{name: "internal", err: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			writeAPIError(response, request, tt.err)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", contentType)
			}
			if !strings.Contains(response.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Errorf("body = %s, want code %q", response.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestOptional(t *testing.T) {
	var request struct {
		Description optional[string] `json:"description"`
	}
	response := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"description":null}`))
	if err := decodeJSON(response, httpRequest, &request); err != nil {
		t.Fatal(err)
	}
	if !request.Description.Set || !request.Description.Null {
		t.Fatalf("optional = %#v", request.Description)
	}
}
