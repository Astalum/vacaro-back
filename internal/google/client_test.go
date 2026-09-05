package google

import (
	"net/url"
	"strings"
	"testing"
)

func TestAuthorizationURLs(t *testing.T) {
	client := New(Config{
		ClientID: "client-id", ClientSecret: "client-secret",
		LoginRedirectURL: "http://localhost/login/callback", CalendarRedirectURL: "http://localhost/calendar/callback",
	})

	loginURL, err := url.Parse(client.LoginAuthURL("login-state", "login-nonce"))
	if err != nil {
		t.Fatal(err)
	}
	loginQuery := loginURL.Query()
	if loginQuery.Get("state") != "login-state" || loginQuery.Get("nonce") != "login-nonce" || loginQuery.Get("redirect_uri") != "http://localhost/login/callback" {
		t.Fatalf("login query = %v", loginQuery)
	}
	loginScopes := strings.Fields(loginQuery.Get("scope"))
	for _, want := range []string{"openid", "email", "profile"} {
		if !contains(loginScopes, want) {
			t.Errorf("login scopes = %#v, missing %q", loginScopes, want)
		}
	}

	calendarURL, err := url.Parse(client.CalendarAuthURL("calendar-state"))
	if err != nil {
		t.Fatal(err)
	}
	calendarQuery := calendarURL.Query()
	calendarScopes := strings.Fields(calendarQuery.Get("scope"))
	if calendarQuery.Get("state") != "calendar-state" || calendarQuery.Get("access_type") != "offline" || calendarQuery.Get("prompt") != "consent" || !contains(calendarScopes, calendarFreeBusyScope) || !contains(calendarScopes, calendarListScope) {
		t.Fatalf("calendar query = %v", calendarQuery)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
