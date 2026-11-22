package feedbackservice

import "testing"

func TestPublicRiderNameFallsBackToAnonymisedLabel(t *testing.T) {
	cases := []struct {
		name    string
		display string
		id      string
		want    string
	}{
		{"display name wins", "闪送小王", "rider-0007", "闪送小王"},
		{"blank display name", "   ", "00000000-0000-4000-8000-000000000003", "骑手·0003"},
		{"empty display name", "", "rider-0012", "骑手·0012"},
		{"no profile id", "", "", "骑手"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := publicRiderName(c.display, c.id); got != c.want {
				t.Fatalf("publicRiderName(%q,%q) = %q, want %q", c.display, c.id, got, c.want)
			}
		})
	}
}
