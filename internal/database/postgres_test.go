package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestOpenRejectsInvalidURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Open(ctx, "://invalid"); err == nil {
		t.Fatal("Open() error = nil, want error")
	}
}

func TestOpenIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var zone string
	if err := pool.QueryRow(ctx, "SHOW timezone").Scan(&zone); err != nil || zone != "UTC" {
		t.Fatalf("database time zone = %q, %v", zone, err)
	}
	var instant time.Time
	if err := pool.QueryRow(ctx, "SELECT '2026-11-05T09:30:00+09:00'::timestamptz").Scan(&instant); err != nil {
		t.Fatal(err)
	}
	if got := instant.Format(time.RFC3339); got != "2026-11-05T00:30:00Z" {
		t.Fatalf("database timestamp = %q", got)
	}
}

func TestPoolConfigUsesUTC(t *testing.T) {
	config, err := parsePoolConfig("postgres://user:password@localhost/vacaro?sslmode=disable&timezone=Asia%2FTokyo")
	if err != nil {
		t.Fatal(err)
	}
	if got := config.ConnConfig.RuntimeParams["timezone"]; got != "UTC" {
		t.Fatalf("database time zone = %q, want UTC", got)
	}
}

func TestTimestampDecodingUsesUTC(t *testing.T) {
	typeMap := pgtype.NewMap()
	registerUTCTimestamps(typeMap)
	instant := time.Date(2026, time.November, 5, 9, 30, 0, 0, time.FixedZone("JST", 9*60*60))
	for _, format := range []int16{pgtype.BinaryFormatCode, pgtype.TextFormatCode} {
		encoded, err := typeMap.Encode(pgtype.TimestamptzOID, format, instant, nil)
		if err != nil {
			t.Fatal(err)
		}
		var decoded time.Time
		if err := typeMap.Scan(pgtype.TimestamptzOID, format, encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if !decoded.Equal(instant) || decoded.Location() != time.UTC {
			t.Fatalf("format %d decoded = %s (%s), want same instant in UTC", format, decoded, decoded.Location())
		}
		if got := decoded.Format(time.RFC3339); got != "2026-11-05T00:30:00Z" {
			t.Fatalf("format %d RFC3339 = %q", format, got)
		}
	}
}
