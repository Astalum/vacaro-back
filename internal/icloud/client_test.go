package icloud

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	googleclient "github.com/astalum/vacaro-back/internal/google"
)

func multi(href, props string) string {
	props = strings.ReplaceAll(props, `\r\n`, "\r\n")
	return `<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:href>` + href + `</d:href><d:propstat><d:prop>` + props + `</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
}
func TestCalDAVLifecycle(t *testing.T) {
	calendars := map[string]bool{}
	objects := map[string][]byte{}
	creates := 0
	puts := 0
	deletes := 0
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "me@example.com" || password != "aaaa-bbbb-cccc-dddd" {
			w.WriteHeader(401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(multi("/", `<d:current-user-principal><d:href>/principal/</d:href></d:current-user-principal>`)))
		case r.Method == "PROPFIND" && r.URL.Path == "/principal/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(multi("/principal/", `<c:calendar-home-set><d:href>`+server.URL+`/home/</d:href></c:calendar-home-set>`)))
		case r.Method == "PROPFIND" && r.URL.Path == "/home/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(multi("/home/personal/", `<d:displayname>Personal</d:displayname><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set>`)))
		case r.Method == "PROPFIND":
			if !calendars[r.URL.Path] {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(207)
			_, _ = w.Write([]byte(multi(r.URL.Path, `<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>`)))
		case r.Method == "MKCALENDAR":
			if !strings.Contains(string(body), "<d:displayname>Vacaro</d:displayname>") {
				t.Error("missing dedicated calendar name")
			}
			if calendars[r.URL.Path] {
				w.WriteHeader(405)
				return
			}
			calendars[r.URL.Path] = true
			creates++
			w.WriteHeader(201)
		case r.Method == "REPORT":
			if !strings.Contains(string(body), `<c:expand start="20261115T000000Z"`) {
				t.Error("server-side recurrence expansion is required")
			}
			w.WriteHeader(207)
			_, _ = w.Write([]byte(multi("/home/personal/event.ics", `<c:calendar-data><![CDATA[BEGIN:VCALENDAR
VERSION:2.0
PRODID:test
BEGIN:VEVENT
UID:busy
DTSTART:20261115T010000Z
DTEND:20261115T020000Z
END:VEVENT
END:VCALENDAR
]]></c:calendar-data>`)))
		case r.Method == http.MethodGet:
			if data, ok := objects[r.URL.Path]; ok {
				w.Header().Set("ETag", `"v1"`)
				_, _ = w.Write(data)
			} else {
				w.WriteHeader(404)
			}
		case r.Method == http.MethodPut:
			if _, exists := objects[r.URL.Path]; exists {
				if r.Header.Get("If-Match") != `"v1"` {
					t.Error("update must use ETag")
				}
			} else {
				if r.Header.Get("If-None-Match") != "*" {
					t.Error("create must not overwrite an existing event")
				}
			}
			objects[r.URL.Path] = body
			puts++
			w.WriteHeader(201)
		case r.Method == http.MethodDelete:
			if r.Header.Get("If-Match") != `"v1"` {
				t.Error("delete must use ETag")
			}
			delete(objects, r.URL.Path)
			deletes++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	client := New()
	client.entry = server.URL + "/"
	client.http = server.Client()
	client.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.allowed = func(u *url.URL) bool {
		return u.Scheme == "https" && u.Host == strings.TrimPrefix(server.URL, "https://")
	}
	ctx := context.Background()
	cred, err := client.Discover(ctx, "me@example.com", "aaaa-bbbb-cccc-dddd", "user1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := client.Discover(ctx, "me@example.com", "aaaa-bbbb-cccc-dddd", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if creates != 1 || again.CalendarURL != cred.CalendarURL || !strings.Contains(cred.CalendarURL, "/home/vacaro-") {
		t.Fatalf("destination not reused: %#v, creates=%d", again, creates)
	}
	listed, err := client.ListCalendars(ctx, cred)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %v %v", listed, err)
	}
	start := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	busy, err := client.Busy(ctx, cred, []string{server.URL + "/home/personal/"}, start, start.Add(24*time.Hour), "Asia/Tokyo")
	if err != nil || len(busy) != 1 || !busy[0].StartAt.Equal(start.Add(time.Hour)) {
		t.Fatalf("busy: %v %v", busy, err)
	}
	if _, err := client.Busy(ctx, cred, []string{"https://evil.example/steal"}, start, start.Add(time.Hour), "Asia/Tokyo"); err == nil {
		t.Fatal("unlisted calendar accepted")
	}
	event := googleclient.CalendarEvent{ID: "vacaro123", Summary: "会議,;\nBEGIN:VEVENT", Description: "改行\n説明", StartAt: start, EndAt: start.Add(time.Hour)}
	if _, err := client.UpsertEvent(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	event.Summary = "更新"
	if _, err := client.UpsertEvent(ctx, cred, event); err != nil {
		t.Fatal(err)
	}
	if puts != 2 {
		t.Fatal("event was not updated")
	}
	if err := client.DeleteEvent(ctx, cred, event.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteEvent(ctx, cred, event.ID); err != nil {
		t.Fatal(err)
	}
	if deletes != 1 || creates != 1 {
		t.Fatalf("delete created a calendar or repeated writes: %d %d", deletes, creates)
	}
}
func TestOnlyAppleHTTPSURLsAreAllowed(t *testing.T) {
	for _, tc := range []struct {
		url     string
		allowed bool
	}{
		{"https://caldav.icloud.com/", true}, {"https://p49-caldav.icloud.com:443/path/", true},
		{"http://caldav.icloud.com/", false}, {"https://caldav.icloud.com.evil.example/", false}, {"https://evil@caldav.icloud.com/", false}, {"https://caldav.icloud.com:1234/", false}, {"https://127.0.0.1/", false}, {"https://caldav.icloud.com/?token=leak", false},
	} {
		u, _ := url.Parse(tc.url)
		if allowedURL(u) != tc.allowed {
			t.Errorf("URL policy failed for %s", tc.url)
		}
	}
	c := New()
	cred := Credentials{HomeURL: "https://p49-caldav.icloud.com/123/calendars/"}
	target, err := c.calendarURL(cred, "https://caldav.icloud.com:443/123/calendars/work/")
	if err != nil || target != "https://p49-caldav.icloud.com/123/calendars/work/" {
		t.Fatalf("partition alias: %q,%v", target, err)
	}
	for _, id := range []string{"../other/", "https://p49-caldav.icloud.com/other/home/", "https://evil.example/123/calendars/work/"} {
		if _, err := c.calendarURL(cred, id); err == nil {
			t.Errorf("out-of-home calendar accepted: %s", id)
		}
	}
}
func TestNeverOverwriteUnrelatedOrInvitationEvents(t *testing.T) {
	event := googleclient.CalendarEvent{ID: "vacaro123", Summary: "会議", StartAt: time.Now().UTC(), EndAt: time.Now().UTC().Add(time.Hour)}
	data, err := encodeEvent(event)
	if err != nil || !safeOwnedEvent(data, event.ID) {
		t.Fatalf("owned event rejected: %v", err)
	}
	for _, extra := range []string{"ORGANIZER:mailto:someone@example.com\r\n", "ATTENDEE:mailto:someone@example.com\r\n"} {
		modified := strings.Replace(string(data), "END:VEVENT", extra+"END:VEVENT", 1)
		if safeOwnedEvent([]byte(modified), event.ID) {
			t.Fatal("invitation event was accepted for overwrite")
		}
	}
	if safeOwnedEvent(data, "different") {
		t.Fatal("unrelated UID accepted")
	}
}
func TestSafeProviderErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 412, 500} {
		err := failure(status)
		if strings.Contains(err.Error(), "password") {
			t.Fatal(err)
		}
		_ = fmt.Sprint(err)
	}
}
