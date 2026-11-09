package icloud

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/emersion/go-ical"
)

func parseBusy(data, calendarZone, fallbackZone string, accounts ...string) ([]googleclient.BusyInterval, error) {
	zone := fallbackZone
	if calendarZone != "" {
		tz, err := decodeCalendar(calendarZone)
		if err != nil {
			return nil, failure(0)
		}
		found := false
		for _, child := range tz.Children {
			if child.Name == ical.CompTimezone {
				name, err := child.Props.Text(ical.PropTimezoneID)
				if err != nil || name == "" {
					return nil, failure(0)
				}
				zone = name
				found = true
				break
			}
		}
		if !found {
			return nil, failure(0)
		}
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, failure(0)
	}
	cal, err := decodeCalendar(data)
	if err != nil {
		return nil, failure(0)
	}
	result := make([]googleclient.BusyInterval, 0)
	for _, event := range cal.Events() {
		status, err := event.Status()
		if err != nil {
			return nil, failure(0)
		}
		transparency, err := event.Props.Text(ical.PropTransparency)
		if err != nil {
			return nil, failure(0)
		}
		declined := false
		if len(accounts) > 0 {
			for _, attendee := range event.Props.Values(ical.PropAttendee) {
				address := strings.TrimPrefix(strings.ToLower(attendee.Value), "mailto:")
				if strings.EqualFold(address, accounts[0]) && strings.EqualFold(attendee.Params.Get("PARTSTAT"), "DECLINED") {
					declined = true
				}
			}
		}
		if declined || status == ical.EventCancelled || strings.EqualFold(transparency, "TRANSPARENT") {
			continue
		}
		// iCloud expands recurrence on the server, including overrides and EXDATEs.
		// Never interpret an unexpanded recurring series as only its first instance.
		if event.Props.Get(ical.PropRecurrenceRule) != nil || event.Props.Get(ical.PropRecurrenceDates) != nil || event.Props.Get(ical.PropExceptionDates) != nil {
			return nil, &Error{Kind: "recurrence_not_expanded"}
		}
		start, err := event.DateTimeStart(loc)
		if err != nil || start.IsZero() {
			return nil, failure(0)
		}
		end, err := event.DateTimeEnd(loc)
		if err != nil {
			return nil, failure(0)
		}
		if duration := event.Props.Get(ical.PropDuration); duration != nil {
			match := nominalDays.FindStringSubmatch(duration.Value)
			if match != nil {
				days := 0
				for i := 1; i <= 2; i++ {
					if match[i] != "" {
						value, err := strconv.Atoi(match[i])
						if err != nil || value > 10000 {
							return nil, failure(0)
						}
						if i == 1 {
							value *= 7
						}
						days += value
					}
				}
				end = start.AddDate(0, 0, days).Add(end.Sub(start) - time.Duration(days)*24*time.Hour)
			}
		}
		startProp := event.Props.Get(ical.PropDateTimeStart)
		if event.Props.Get(ical.PropDateTimeEnd) == nil && event.Props.Get(ical.PropDuration) == nil && startProp.ValueType() == ical.ValueDate {
			end = start.AddDate(0, 0, 1)
		}
		if end.Before(start) {
			return nil, failure(0)
		}
		if start.Before(end) {
			result = append(result, googleclient.BusyInterval{StartAt: start.UTC(), EndAt: end.UTC()})
		}
	}
	return result, nil
}

func encodeEvent(event googleclient.CalendarEvent) ([]byte, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//Vacaro//iCloud Calendar//JA")
	cal.Props.SetText(ical.PropVersion, "2.0")
	e := ical.NewEvent()
	e.Props.SetText(ical.PropUID, event.ID+"@vacaro")
	e.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	e.Props.SetDateTime(ical.PropDateTimeStart, event.StartAt.UTC())
	e.Props.SetDateTime(ical.PropDateTimeEnd, event.EndAt.UTC())
	e.Props.SetText(ical.PropSummary, event.Summary)
	e.Props.SetText(ical.PropDescription, event.Description)
	e.SetStatus(ical.EventConfirmed)
	cal.Children = append(cal.Children, e.Component)
	var out bytes.Buffer
	if ical.NewEncoder(&out).Encode(cal) != nil {
		return nil, failure(0)
	}
	return out.Bytes(), nil
}
func safeOwnedEvent(data []byte, id string) bool {
	cal, err := decodeCalendar(string(data))
	if err != nil {
		return false
	}
	events := cal.Events()
	if len(events) != 1 {
		return false
	}
	uid, err := events[0].Props.Text(ical.PropUID)
	return err == nil && uid == id+"@vacaro" && events[0].Props.Get(ical.PropOrganizer) == nil && events[0].Props.Get(ical.PropAttendee) == nil
}
func (c *Client) eventURL(cred Credentials, id string) (string, error) {
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			if ch < 'a' || ch > 'z' {
				return "", failure(0)
			}
		}
	}
	if len(id) == 0 || len(id) > 128 {
		return "", failure(0)
	}
	if _, err := c.calendarURL(cred, cred.CalendarURL); err != nil {
		return "", err
	}
	return c.resolve(cred.CalendarURL, id+".ics")
}
func (c *Client) UpsertEvent(ctx context.Context, cred Credentials, event googleclient.CalendarEvent) (bool, error) {
	created, err := c.EnsureCalendar(ctx, cred)
	if err != nil {
		return created, err
	}
	target, err := c.eventURL(cred, event.ID)
	if err != nil {
		return created, err
	}
	status, data, headers, err := c.request(ctx, cred, http.MethodGet, target, "", nil, nil)
	if err != nil {
		return created, err
	}
	conditions := map[string]string{"If-None-Match": "*"}
	if status == 200 {
		if !safeOwnedEvent(data, event.ID) || headers.Get("ETag") == "" {
			return created, failure(409)
		}
		conditions = map[string]string{"If-Match": headers.Get("ETag")}
	} else if status != 404 && status != 410 {
		return created, failure(status)
	}
	payload, err := encodeEvent(event)
	if err != nil {
		return created, err
	}
	status, _, _, err = c.request(ctx, cred, http.MethodPut, target, "text/calendar; charset=utf-8", payload, conditions)
	if err != nil {
		return created, err
	}
	if status != 201 && status != 204 && status != 200 {
		return created, failure(status)
	}
	return created, nil
}
func (c *Client) DeleteEvent(ctx context.Context, cred Credentials, id string) error {
	target, err := c.eventURL(cred, id)
	if err != nil {
		return err
	}
	status, data, headers, err := c.request(ctx, cred, http.MethodGet, target, "", nil, nil)
	if err != nil {
		return err
	}
	if status == 404 || status == 410 {
		return nil
	}
	if status != 200 {
		return failure(status)
	}
	if !safeOwnedEvent(data, id) || headers.Get("ETag") == "" {
		return failure(409)
	}
	status, _, _, err = c.request(ctx, cred, http.MethodDelete, target, "", nil, map[string]string{"If-Match": headers.Get("ETag")})
	if err != nil {
		return err
	}
	if status != 204 && status != 200 && status != 404 && status != 410 {
		return failure(status)
	}
	return nil
}

var nominalDays = regexp.MustCompile(`^\+?P(?:(\d+)W)?(?:(\d+)D)?(?:T.*)?$`)

func decodeCalendar(data string) (cal *ical.Calendar, err error) {
	defer func() {
		if recover() != nil {
			cal = nil
			err = failure(0)
		}
	}()
	decoder := ical.NewDecoder(strings.NewReader(data))
	cal, err = decoder.Decode()
	if err != nil {
		return nil, failure(0)
	}
	if _, err = decoder.Decode(); err != io.EOF {
		return nil, failure(0)
	}
	return cal, nil
}
