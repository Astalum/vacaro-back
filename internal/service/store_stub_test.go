package service

import (
	"context"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/astalum/vacaro-back/internal/repository"
)

type storeStub struct {
	createGuest           func(context.Context, string, time.Time) (domain.User, error)
	getUser               func(context.Context, string) (domain.User, error)
	updateUserName        func(context.Context, string, string) (domain.User, error)
	createEvent           func(context.Context, repository.CreateEventParams) (domain.Event, error)
	listEvents            func(context.Context, string, string, *time.Time, *string, int) ([]domain.Event, error)
	getEventForUser       func(context.Context, string, string) (domain.Event, error)
	getEventByInvite      func(context.Context, string) (domain.Event, error)
	updateEvent           func(context.Context, repository.UpdateEventParams) (domain.Event, error)
	setResultsVisibility  func(context.Context, string, string, bool) (domain.Event, error)
	rotateInviteToken     func(context.Context, string, string, string) error
	joinEvent             func(context.Context, string, string, string) (domain.Participant, error)
	listParticipants      func(context.Context, string, string) ([]domain.Participant, error)
	leaveEvent            func(context.Context, string, string) error
	replaceAvailability   func(context.Context, string, string, []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error)
	getOwnAvailability    func(context.Context, string, string) ([]domain.AvailabilityInterval, string, error)
	listAvailability      func(context.Context, string, string) ([]domain.ParticipantAvailability, error)
	confirmEvent          func(context.Context, string, string, time.Time, time.Time) (domain.Event, error)
	reopenEvent           func(context.Context, string, string) (domain.Event, error)
	cancelEvent           func(context.Context, string, string) error
	upsertGoogleUser      func(context.Context, string, string, string, string) (domain.User, error)
	saveGoogleCalendar    func(context.Context, string, []byte, []string) error
	getGoogleCalendar     func(context.Context, string) ([]byte, []string, error)
	deleteGoogleCalendar  func(context.Context, string) ([]byte, error)
	hasGoogleAccount      func(context.Context, string) (bool, error)
	listGroups            func(context.Context, string, string) ([]domain.SchedulingGroup, error)
	getGroup              func(context.Context, string, string, string) (domain.SchedulingGroup, error)
	createGroup           func(context.Context, repository.CreateGroupParams) (domain.SchedulingGroup, error)
	updateGroup           func(context.Context, repository.UpdateGroupParams) (domain.SchedulingGroup, error)
	deleteGroup           func(context.Context, string, string, string) error
	replaceGroupMembers   func(context.Context, string, string, string, []string) (domain.SchedulingGroup, error)
	listGroupAvailability func(context.Context, string, string, string) ([]domain.ParticipantAvailability, error)
	confirmGroup          func(context.Context, string, string, string, time.Time, time.Time) (domain.SchedulingGroup, error)
	reopenGroup           func(context.Context, string, string, string) (domain.SchedulingGroup, error)
}

func (s storeStub) CreateGuest(ctx context.Context, name string, expires time.Time) (domain.User, error) {
	return s.createGuest(ctx, name, expires)
}
func (s storeStub) GetUser(ctx context.Context, id string) (domain.User, error) {
	return s.getUser(ctx, id)
}
func (s storeStub) UpdateUserName(ctx context.Context, id, name string) (domain.User, error) {
	return s.updateUserName(ctx, id, name)
}
func (s storeStub) CreateEvent(ctx context.Context, params repository.CreateEventParams) (domain.Event, error) {
	return s.createEvent(ctx, params)
}
func (s storeStub) ListEvents(ctx context.Context, userID, status string, cursorTime *time.Time, cursorID *string, limit int) ([]domain.Event, error) {
	return s.listEvents(ctx, userID, status, cursorTime, cursorID, limit)
}
func (s storeStub) GetEventForUser(ctx context.Context, eventID, userID string) (domain.Event, error) {
	return s.getEventForUser(ctx, eventID, userID)
}
func (s storeStub) GetEventByInviteHash(ctx context.Context, hash string) (domain.Event, error) {
	return s.getEventByInvite(ctx, hash)
}
func (s storeStub) UpdateEvent(ctx context.Context, params repository.UpdateEventParams) (domain.Event, error) {
	return s.updateEvent(ctx, params)
}
func (s storeStub) SetResultsVisibility(ctx context.Context, eventID, userID string, visible bool) (domain.Event, error) {
	return s.setResultsVisibility(ctx, eventID, userID, visible)
}
func (s storeStub) RotateInviteToken(ctx context.Context, eventID, userID, hash string) error {
	return s.rotateInviteToken(ctx, eventID, userID, hash)
}
func (s storeStub) JoinEvent(ctx context.Context, hash, userID, name string) (domain.Participant, error) {
	return s.joinEvent(ctx, hash, userID, name)
}
func (s storeStub) ListParticipants(ctx context.Context, eventID, userID string) ([]domain.Participant, error) {
	return s.listParticipants(ctx, eventID, userID)
}
func (s storeStub) LeaveEvent(ctx context.Context, eventID, userID string) error {
	return s.leaveEvent(ctx, eventID, userID)
}
func (s storeStub) ReplaceAvailability(ctx context.Context, eventID, userID string, intervals []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error) {
	return s.replaceAvailability(ctx, eventID, userID, intervals)
}
func (s storeStub) GetOwnAvailability(ctx context.Context, eventID, userID string) ([]domain.AvailabilityInterval, string, error) {
	return s.getOwnAvailability(ctx, eventID, userID)
}
func (s storeStub) ListAvailability(ctx context.Context, eventID, userID string) ([]domain.ParticipantAvailability, error) {
	return s.listAvailability(ctx, eventID, userID)
}
func (s storeStub) ConfirmEvent(ctx context.Context, eventID, userID string, start, end time.Time) (domain.Event, error) {
	return s.confirmEvent(ctx, eventID, userID, start, end)
}
func (s storeStub) ReopenEvent(ctx context.Context, eventID, userID string) (domain.Event, error) {
	return s.reopenEvent(ctx, eventID, userID)
}
func (s storeStub) CancelEvent(ctx context.Context, eventID, userID string) error {
	return s.cancelEvent(ctx, eventID, userID)
}
func (s storeStub) UpsertGoogleUser(ctx context.Context, currentUserID, subject, email, name string) (domain.User, error) {
	return s.upsertGoogleUser(ctx, currentUserID, subject, email, name)
}
func (s storeStub) SaveGoogleCalendarConnection(ctx context.Context, userID string, ciphertext []byte, scopes []string) error {
	return s.saveGoogleCalendar(ctx, userID, ciphertext, scopes)
}
func (s storeStub) GetGoogleCalendarConnection(ctx context.Context, userID string) ([]byte, []string, error) {
	return s.getGoogleCalendar(ctx, userID)
}
func (s storeStub) DeleteGoogleCalendarConnection(ctx context.Context, userID string) ([]byte, error) {
	return s.deleteGoogleCalendar(ctx, userID)
}
func (s storeStub) HasGoogleAccount(ctx context.Context, userID string) (bool, error) {
	return s.hasGoogleAccount(ctx, userID)
}
func (s storeStub) ListGroups(ctx context.Context, eventID, userID string) ([]domain.SchedulingGroup, error) {
	return s.listGroups(ctx, eventID, userID)
}
func (s storeStub) GetGroup(ctx context.Context, eventID, groupID, userID string) (domain.SchedulingGroup, error) {
	return s.getGroup(ctx, eventID, groupID, userID)
}
func (s storeStub) CreateGroup(ctx context.Context, params repository.CreateGroupParams) (domain.SchedulingGroup, error) {
	return s.createGroup(ctx, params)
}
func (s storeStub) UpdateGroup(ctx context.Context, params repository.UpdateGroupParams) (domain.SchedulingGroup, error) {
	return s.updateGroup(ctx, params)
}
func (s storeStub) DeleteGroup(ctx context.Context, eventID, groupID, userID string) error {
	return s.deleteGroup(ctx, eventID, groupID, userID)
}
func (s storeStub) ReplaceGroupMembers(ctx context.Context, eventID, groupID, userID string, participantIDs []string) (domain.SchedulingGroup, error) {
	return s.replaceGroupMembers(ctx, eventID, groupID, userID, participantIDs)
}
func (s storeStub) ListGroupAvailability(ctx context.Context, eventID, groupID, userID string) ([]domain.ParticipantAvailability, error) {
	return s.listGroupAvailability(ctx, eventID, groupID, userID)
}
func (s storeStub) ConfirmGroup(ctx context.Context, eventID, groupID, userID string, startAt, endAt time.Time) (domain.SchedulingGroup, error) {
	return s.confirmGroup(ctx, eventID, groupID, userID, startAt, endAt)
}
func (s storeStub) ReopenGroup(ctx context.Context, eventID, groupID, userID string) (domain.SchedulingGroup, error) {
	return s.reopenGroup(ctx, eventID, groupID, userID)
}
