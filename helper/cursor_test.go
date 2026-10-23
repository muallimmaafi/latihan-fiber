package helper

import (
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 13, 14, 12, 40, 123456000, time.UTC)

	got, err := DecodeCursor(EncodeCursor(at, 8))
	if err != nil {
		t.Fatalf("tidak seharusnya error: %v", err)
	}
	if !got.CreatedAt.Equal(at) || got.ID != 8 {
		t.Errorf("round trip salah: dapat %v id=%d", got.CreatedAt, got.ID)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "   ", "bukanbase64!!", "YWJj", "MXwwfDA"} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("%q seharusnya ditolak", bad)
		}
	}
}