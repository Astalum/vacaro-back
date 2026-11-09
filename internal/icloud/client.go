package icloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	googleclient "github.com/astalum/vacaro-back/internal/google"
)

const entryURL = "https://caldav.icloud.com/"
const maxResponse = 8 << 20

var partitionHost = regexp.MustCompile(`^p[0-9]+-caldav\.icloud\.com$`)

// Credentials are encrypted at rest and must never be returned to the browser.
type Credentials struct{ Username, Password, HomeURL, CalendarURL string }
type Calendar struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	BackgroundColor string `json:"background_color,omitempty"`
	Dedicated       bool   `json:"is_vacaro"`
	TimeZone        string `json:"-"`
}
type Error struct {
	Kind   string
	Status int
}

func (e *Error) Error() string { return "iCloud Calendar " + e.Kind }
func failure(status int) error {
	kind := "temporarily_unavailable"
	switch status {
	case 401:
		kind = "reconnect_required"
	case 403:
		kind = "permission_required"
	case 404, 410:
		kind = "calendar_unavailable"
	case 409, 412:
		kind = "conflict"
	}
	return &Error{Kind: kind, Status: status}
}

type Client struct {
	http    *http.Client
	allowed func(*url.URL) bool
	entry   string
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, allowed: allowedURL, entry: entryURL}
}
func allowedURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && u.RawQuery == "" && u.Fragment == "" && (u.Hostname() == "caldav.icloud.com" || partitionHost.MatchString(u.Hostname()))
}
func (c *Client) resolve(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", failure(0)
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", failure(0)
	}
	u := b.ResolveReference(r)
	if !c.allowed(u) {
		return "", failure(0)
	}
	u.Path = path.Clean(u.Path)
	if strings.HasSuffix(r.Path, "/") && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawPath = ""
	return u.String(), nil
}
func (c *Client) request(ctx context.Context, cred Credentials, method, target, contentType string, body []byte, headers map[string]string) (int, []byte, http.Header, error) {
	for redirects := 0; redirects < 5; redirects++ {
		u, err := url.Parse(target)
		if err != nil || !c.allowed(u) {
			return 0, nil, nil, failure(0)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
		if err != nil {
			return 0, nil, nil, failure(0)
		}
		req.SetBasicAuth(cred.Username, cred.Password)
		req.Header.Set("User-Agent", "Vacaro/1.0")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		response, err := c.http.Do(req)
		if err != nil {
			return 0, nil, nil, failure(0)
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
		_ = response.Body.Close()
		if readErr != nil || len(data) > maxResponse {
			return 0, nil, nil, failure(0)
		}
		if response.StatusCode == 301 || response.StatusCode == 302 || response.StatusCode == 307 || response.StatusCode == 308 {
			target, err = c.resolve(target, response.Header.Get("Location"))
			if err != nil {
				return 0, nil, nil, err
			}
			continue
		}
		return response.StatusCode, data, response.Header, nil
	}
	return 0, nil, nil, failure(0)
}

type davHref struct {
	Href string `xml:"DAV: href"`
}

type properties struct {
	Principal davHref `xml:"DAV: current-user-principal"`
	Home      davHref `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	Name      string  `xml:"DAV: displayname"`
	Types     struct {
		Calendar   *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
		Subscribed *struct{} `xml:"http://calendarserver.org/ns/ subscribed"`
	} `xml:"DAV: resourcetype"`
	Components struct {
		Items []struct {
			Name string `xml:"name,attr"`
		} `xml:"urn:ietf:params:xml:ns:caldav comp"`
	} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	Color    string `xml:"http://apple.com/ns/ical/ calendar-color"`
	TimeZone string `xml:"urn:ietf:params:xml:ns:caldav calendar-timezone"`
	Data     string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
}
type davResponse struct {
	Href      string `xml:"DAV: href"`
	Status    string `xml:"DAV: status"`
	Propstats []struct {
		Status string     `xml:"DAV: status"`
		Prop   properties `xml:"DAV: prop"`
	} `xml:"DAV: propstat"`
}
type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

func parseMulti(data []byte) ([]davResponse, error) {
	var result multistatus
	if xml.Unmarshal(data, &result) != nil {
		return nil, failure(0)
	}
	return result.Responses, nil
}
func okStatus(status string) bool {
	fields := strings.Fields(status)
	return len(fields) >= 2 && fields[1] == "200"
}
func (c *Client) propfind(ctx context.Context, cred Credentials, target, depth, props string) ([]davResponse, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:a="http://apple.com/ns/ical/"><d:prop>` + props + `</d:prop></d:propfind>`)
	status, data, _, err := c.request(ctx, cred, "PROPFIND", target, "application/xml; charset=utf-8", body, map[string]string{"Depth": depth})
	if err != nil {
		return nil, err
	}
	if status != 207 {
		return nil, failure(status)
	}
	return parseMulti(data)
}
func (c *Client) Discover(ctx context.Context, username, password, userID string) (Credentials, error) {
	cred := Credentials{Username: username, Password: password}
	responses, err := c.propfind(ctx, cred, c.entry, "0", "<d:current-user-principal/>")
	if err != nil {
		return Credentials{}, err
	}
	principal := ""
	for _, r := range responses {
		for _, p := range r.Propstats {
			if okStatus(p.Status) && p.Prop.Principal.Href != "" {
				principal = p.Prop.Principal.Href
			}
		}
	}
	if principal == "" {
		return Credentials{}, failure(0)
	}
	principal, err = c.resolve(c.entry, principal)
	if err != nil {
		return Credentials{}, err
	}
	responses, err = c.propfind(ctx, cred, principal, "0", "<c:calendar-home-set/>")
	if err != nil {
		return Credentials{}, err
	}
	home := ""
	for _, r := range responses {
		for _, p := range r.Propstats {
			if okStatus(p.Status) && p.Prop.Home.Href != "" {
				home = p.Prop.Home.Href
			}
		}
	}
	if home == "" {
		return Credentials{}, failure(0)
	}
	cred.HomeURL, err = c.resolve(principal, strings.TrimRight(home, "/")+"/")
	if err != nil {
		return Credentials{}, err
	}
	digest := sha256.Sum256([]byte("vacaro:icloud:" + userID))
	cred.CalendarURL, err = c.resolve(cred.HomeURL, "vacaro-"+hex.EncodeToString(digest[:16])+"/")
	if err != nil {
		return Credentials{}, err
	}
	if _, err := c.EnsureCalendar(ctx, cred); err != nil {
		return Credentials{}, err
	}
	return cred, nil
}
func (c *Client) calendarURL(cred Credentials, ref string) (string, error) {
	target, err := c.resolve(cred.HomeURL, ref)
	if err != nil {
		return "", err
	}
	home, _ := url.Parse(cred.HomeURL)
	u, _ := url.Parse(target)
	// iCloud may return an unpartitioned href for the same calendar. Keep the
	// discovered partition host, and confine every collection to this home.
	prefix := strings.TrimRight(home.Path, "/") + "/"
	if !strings.HasPrefix(u.Path, prefix) || strings.Trim(u.Path[len(prefix):], "/") == "" || strings.Contains(strings.Trim(u.Path[len(prefix):], "/"), "/") {
		return "", failure(0)
	}
	u.Host = home.Host
	return u.String(), nil
}
func (c *Client) EnsureCalendar(ctx context.Context, cred Credentials) (bool, error) {
	target, err := c.calendarURL(cred, cred.CalendarURL)
	if err != nil {
		return false, err
	}
	responses, err := c.propfind(ctx, cred, target, "0", "<d:resourcetype/>")
	if err == nil {
		for _, r := range responses {
			for _, p := range r.Propstats {
				if okStatus(p.Status) && p.Prop.Types.Calendar != nil {
					return false, nil
				}
			}
		}
		return false, failure(0)
	}
	e, ok := err.(*Error)
	if !ok || (e.Status != 404 && e.Status != 410) {
		return false, err
	}
	body := []byte(`<?xml version="1.0" encoding="utf-8"?><c:mkcalendar xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:set><d:prop><d:displayname>Vacaro</d:displayname><c:calendar-description>Vacaroで確定した予定を登録する専用カレンダーです。</c:calendar-description><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop></d:set></c:mkcalendar>`)
	status, _, _, err := c.request(ctx, cred, "MKCALENDAR", target, "application/xml; charset=utf-8", body, nil)
	if err != nil {
		return false, err
	}
	if status == 201 {
		return true, nil
	}
	if status == 405 || status == 409 {
		responses, err = c.propfind(ctx, cred, target, "0", "<d:resourcetype/>")
		if err != nil {
			return false, err
		}
		for _, r := range responses {
			for _, p := range r.Propstats {
				if okStatus(p.Status) && p.Prop.Types.Calendar != nil {
					return false, nil
				}
			}
		}
	}
	return false, failure(status)
}
func (c *Client) ListCalendars(ctx context.Context, cred Credentials) ([]Calendar, error) {
	responses, err := c.propfind(ctx, cred, cred.HomeURL, "1", "<d:displayname/><d:resourcetype/><c:supported-calendar-component-set/><a:calendar-color/><c:calendar-timezone/>")
	if err != nil {
		return nil, err
	}
	result := make([]Calendar, 0)
	for _, r := range responses {
		props := properties{}
		for _, p := range r.Propstats {
			if okStatus(p.Status) {
				if p.Prop.Types.Calendar != nil {
					props.Types = p.Prop.Types
				}
				if p.Prop.Name != "" {
					props.Name = p.Prop.Name
				}
				if p.Prop.Color != "" {
					props.Color = p.Prop.Color
				}
				if p.Prop.TimeZone != "" {
					props.TimeZone = p.Prop.TimeZone
				}
				if len(p.Prop.Components.Items) > 0 {
					props.Components = p.Prop.Components
				}
			}
		}
		if props.Types.Calendar == nil || props.Types.Subscribed != nil {
			continue
		}
		eventSupported := len(props.Components.Items) == 0
		for _, comp := range props.Components.Items {
			if comp.Name == "VEVENT" {
				eventSupported = true
			}
		}
		if !eventSupported {
			continue
		}
		id, err := c.calendarURL(cred, r.Href)
		if err != nil {
			return nil, err
		}
		result = append(result, Calendar{ID: id, Summary: props.Name, BackgroundColor: props.Color, Dedicated: id == cred.CalendarURL, TimeZone: props.TimeZone})
	}
	return result, nil
}
func (c *Client) Busy(ctx context.Context, cred Credentials, ids []string, start, end time.Time, zone string) ([]googleclient.BusyInterval, error) {
	calendars, err := c.ListCalendars(ctx, cred)
	if err != nil {
		return nil, err
	}
	available := map[string]Calendar{}
	for _, calendar := range calendars {
		available[calendar.ID] = calendar
	}
	if len(ids) == 0 {
		for _, calendar := range calendars {
			if !calendar.Dedicated {
				ids = []string{calendar.ID}
				break
			}
		}
		if len(ids) == 0 && len(calendars) > 0 {
			ids = []string{calendars[0].ID}
		}
	}
	if len(ids) == 0 {
		return nil, failure(404)
	}
	result := make([]googleclient.BusyInterval, 0)
	seen := map[string]bool{}
	for _, id := range ids {
		calendar, ok := available[id]
		if !ok {
			return nil, failure(404)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		body := []byte(`<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data><c:expand start="` + start.UTC().Format("20060102T150405Z") + `" end="` + end.UTC().Format("20060102T150405Z") + `"/></c:calendar-data></d:prop><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="` + start.UTC().Format("20060102T150405Z") + `" end="` + end.UTC().Format("20060102T150405Z") + `"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`)
		status, data, _, err := c.request(ctx, cred, "REPORT", id, "application/xml; charset=utf-8", body, map[string]string{"Depth": "1"})
		if err != nil {
			return nil, err
		}
		if status != 207 {
			return nil, failure(status)
		}
		responses, err := parseMulti(data)
		if err != nil {
			return nil, err
		}
		for _, r := range responses {
			if r.Status != "" && !okStatus(r.Status) {
				return nil, failure(0)
			}
			found := false
			for _, p := range r.Propstats {
				if okStatus(p.Status) && p.Prop.Data != "" {
					found = true
					busy, err := parseBusy(p.Prop.Data, calendar.TimeZone, zone, cred.Username)
					if err != nil {
						return nil, err
					}
					result = append(result, busy...)
				}
			}
			if !found {
				return nil, failure(0)
			}
		}
	}
	return result, nil
}
