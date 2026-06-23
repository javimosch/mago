package main

import (
	"testing"
	"time"
)

func TestUntilDuration(t *testing.T) {
	// Valid HH:MM → a duration in (0, 24h].
	d, err := untilDuration("09:00")
	if err != nil {
		t.Fatalf("untilDuration(09:00): %v", err)
	}
	if d <= 0 || d > 24*time.Hour {
		t.Errorf("duration %s out of (0,24h]", d)
	}
	// A time one minute from now is ~today (well under 24h), not pushed to tomorrow.
	soon := time.Now().Add(time.Minute).Format("15:04")
	if d, _ := untilDuration(soon); d > 23*time.Hour {
		t.Errorf("near-future %s should be ~today, got %s", soon, d)
	}
	// Invalid input errors.
	for _, bad := range []string{"25:00", "9am", "", "09:99"} {
		if _, err := untilDuration(bad); err == nil {
			t.Errorf("untilDuration(%q) should error", bad)
		}
	}
}
