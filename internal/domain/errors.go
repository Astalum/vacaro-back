package domain

import "errors"

var (
	ErrNotFound                = errors.New("resource not found")
	ErrPermissionDenied        = errors.New("permission denied")
	ErrAlreadyJoined           = errors.New("already joined")
	ErrEventNotOpen            = errors.New("event is not open")
	ErrOrganizerCannotLeave    = errors.New("organizer cannot leave")
	ErrInvalidTransition       = errors.New("invalid event transition")
	ErrGroupNotEditable        = errors.New("group is not editable")
	ErrGroupConfirmationExists = errors.New("group confirmation exists")
	ErrDuplicateGroupName      = errors.New("duplicate group name")
	ErrGoogleNotConfigured     = errors.New("Google integration is not configured")
	ErrCalendarNotConnected    = errors.New("Google Calendar is not connected")
	ErrOAuthFailed             = errors.New("OAuth flow failed")
	ErrGoogleAuthRequired      = errors.New("Google authentication is required")
)
