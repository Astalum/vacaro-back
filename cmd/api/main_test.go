package main

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
	"time"
)

func TestTimeZoneDataIsEmbedded(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil && path == "time/tzdata" && spec.Name != nil && spec.Name.Name == "_" {
			return
		}
	}

	t.Fatal("main.go must blank-import time/tzdata so production binaries include the IANA database")
}

func TestEmbeddedTimeZoneRules(t *testing.T) {
	tests := []struct {
		name       string
		zone       string
		instant    time.Time
		wantOffset int
	}{
		{name: "Tokyo", zone: "Asia/Tokyo", instant: time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), wantOffset: 9 * 60 * 60},
		{name: "New York standard time", zone: "America/New_York", instant: time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), wantOffset: -5 * 60 * 60},
		{name: "New York daylight time", zone: "America/New_York", instant: time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC), wantOffset: -4 * 60 * 60},
		{name: "London standard time", zone: "Europe/London", instant: time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), wantOffset: 0},
		{name: "London daylight time", zone: "Europe/London", instant: time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC), wantOffset: 60 * 60},
		{name: "Sydney daylight time", zone: "Australia/Sydney", instant: time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), wantOffset: 11 * 60 * 60},
		{name: "Sydney standard time", zone: "Australia/Sydney", instant: time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC), wantOffset: 10 * 60 * 60},
		{name: "Kathmandu fractional offset", zone: "Asia/Kathmandu", instant: time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), wantOffset: 5*60*60 + 45*60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatalf("time.LoadLocation(%q): %v", tt.zone, err)
			}

			_, offset := tt.instant.In(location).Zone()
			if offset != tt.wantOffset {
				t.Errorf("UTC offset = %d, want %d", offset, tt.wantOffset)
			}
		})
	}
}
