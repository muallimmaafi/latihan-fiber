package service

import "testing"

func TestWithinCreditLimit(t *testing.T) {
	cases := []struct {
		taken, adding int
		want          bool
	}{
		{0, 3, true},
		{21, 3, true}, // tepat 24 masih boleh
		{22, 3, false},
		{24, 0, true},
		{24, 1, false},
		{0, 25, false},
	}
	for _, tc := range cases {
		if got := WithinCreditLimit(tc.taken, tc.adding); got != tc.want {
			t.Errorf("taken=%d adding=%d: harap %v, dapat %v",
				tc.taken, tc.adding, tc.want, got)
		}
	}
}

func TestCreditsAfterSwap(t *testing.T) {
	if got := CreditsAfterSwap(24, 3, 3); got != 24 {
		t.Errorf("tukar kelas sama SKS: harap 24, dapat %d", got)
	}
	if got := CreditsAfterSwap(22, 2, 4); got != 24 {
		t.Errorf("harap 24, dapat %d", got)
	}
}