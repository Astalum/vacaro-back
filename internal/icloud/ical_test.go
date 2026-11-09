package icloud

import (
	"strings"
	"testing"
	"time"
)

func eventData(properties string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VEVENT\r\nUID:test\r\n" + properties + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}
func TestBusyTimes(t *testing.T) {
	for _, tc := range []struct {
		name, props, zone string
		hours             int
		skip, fail        bool
	}{
		{"UTC", "DTSTART:20261115T010000Z\r\nDTEND:20261115T020000Z", "Asia/Tokyo", 1, false, false},
		{"IANA timezone", "DTSTART;TZID=Asia/Tokyo:20261115T100000\r\nDTEND;TZID=Asia/Tokyo:20261115T110000", "UTC", 1, false, false},
		{"floating", "DTSTART:20261115T100000\r\nDTEND:20261115T110000", "Asia/Tokyo", 1, false, false},
		{"DST nominal duration", "DTSTART;VALUE=DATE:20260308\r\nDURATION:P1D", "America/New_York", 23, false, false},
		{"spring DST all day", "DTSTART;VALUE=DATE:20260308", "America/New_York", 23, false, false},
		{"fall DST all day", "DTSTART;VALUE=DATE:20261101", "America/New_York", 25, false, false},
		{"cancelled", "STATUS:CANCELLED", "UTC", 0, true, false},
		{"transparent", "TRANSP:TRANSPARENT", "UTC", 0, true, false},
		{"unexpanded recurrence", "DTSTART:20261115T010000Z\r\nDTEND:20261115T020000Z\r\nRRULE:FREQ=DAILY", "UTC", 0, false, true},
		{"invalid timezone", "DTSTART;TZID=Unknown/Invalid:20261115T010000\r\nDTEND:20261115T020000Z", "UTC", 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			busy, err := parseBusy(eventData(tc.props), "", tc.zone)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
			if tc.fail {
				return
			}
			if tc.skip {
				if len(busy) != 0 {
					t.Fatal("non-busy event counted")
				}
				return
			}
			if len(busy) != 1 || busy[0].EndAt.Sub(busy[0].StartAt) != time.Duration(tc.hours)*time.Hour || busy[0].StartAt.Location() != time.UTC {
				t.Fatalf("busy=%v", busy)
			}
		})
	}
}
func TestEncodeEscapesAndUsesUTC(t *testing.T) {
	// Malformed data must never be interpreted as a completely free calendar.
	if _, err := parseBusy("not an iCalendar", "", "UTC"); err == nil {
		t.Fatal("malformed data accepted")
	}
	if _, err := parseBusy(eventData("DTSTART:20261115T010000Z\r\nDTEND:20261115T020000Z"), strings.Replace(eventData(""), "VEVENT", "VTIMEZONE", 2), "UTC"); err == nil {
		t.Fatal("timezone without TZID accepted")
	}
}

func TestDeclinedInvitationIsNotBusy(t *testing.T) {
	data := eventData("DTSTART:20261115T010000Z\r\nDTEND:20261115T020000Z\r\nATTENDEE;PARTSTAT=DECLINED:mailto:me@example.com")
	busy, err := parseBusy(data, "", "UTC", "me@example.com")
	if err != nil || len(busy) != 0 {
		t.Fatalf("declined invitation counted as busy: %v %v", busy, err)
	}
}
