package domain

import "time"

type User struct {
	ID             string     `json:"id"`
	AccountType    string     `json:"account_type"`
	DisplayName    string     `json:"display_name"`
	Email          *string    `json:"email"`
	GuestExpiresAt *time.Time `json:"guest_expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Event struct {
	ID                 string     `json:"id"`
	CreatedByUserID    string     `json:"created_by_user_id,omitempty"`
	Title              string     `json:"title"`
	Description        *string    `json:"description"`
	TimeZone           string     `json:"time_zone"`
	WindowStartAt      time.Time  `json:"window_start_at"`
	WindowEndAt        time.Time  `json:"window_end_at"`
	DurationMinutes    int        `json:"duration_minutes"`
	Status             string     `json:"status"`
	ConfirmedStartAt   *time.Time `json:"confirmed_start_at"`
	ConfirmedEndAt     *time.Time `json:"confirmed_end_at"`
	ResponseDeadlineAt *time.Time `json:"response_deadline_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type Participant struct {
	ID             string    `json:"id"`
	EventID        string    `json:"event_id,omitempty"`
	UserID         string    `json:"user_id"`
	DisplayName    string    `json:"display_name"`
	Role           string    `json:"role"`
	ResponseStatus string    `json:"response_status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AvailabilityInterval struct {
	ID            string    `json:"id"`
	ParticipantID string    `json:"participant_id,omitempty"`
	StartAt       time.Time `json:"start_at"`
	EndAt         time.Time `json:"end_at"`
	Source        string    `json:"source"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ParticipantAvailability struct {
	Participant Participant            `json:"participant"`
	Intervals   []AvailabilityInterval `json:"intervals"`
}

type Candidate struct {
	StartAt                   time.Time `json:"start_at"`
	EndAt                     time.Time `json:"end_at"`
	AvailableParticipantCount int       `json:"available_participant_count"`
	TotalParticipantCount     int       `json:"total_participant_count"`
	AvailableParticipantIDs   []string  `json:"available_participant_ids"`
}
