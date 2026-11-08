package google

import "strings"

// IsHolidayCalendarID identifies Google's built-in holiday calendars by their
// reserved ID suffix, independently of their localized or overridden name.
func IsHolidayCalendarID(calendarID string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(calendarID)), "#holiday@group.v.calendar.google.com")
}
