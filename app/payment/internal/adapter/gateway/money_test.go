package payment

import "testing"

func TestYuanToFen(t *testing.T) {
	cases := []struct {
		value string
		want  int64
		ok    bool
	}{
		{"0.01", 1, true},
		{"12.34", 1234, true},
		{"12.3", 1230, true},
		{"12", 1200, true},
		{" 100.00 ", 10000, true},
		{"0", 0, true},
		{"1.234", 0, false},
		{"-1.00", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, item := range cases {
		got, err := YuanToFen(item.value)
		if item.ok && (err != nil || got != item.want) {
			t.Errorf("YuanToFen(%q) = (%d, %v), want %d", item.value, got, err, item.want)
		}
		if !item.ok && err == nil {
			t.Errorf("YuanToFen(%q) should fail, got %d", item.value, got)
		}
	}
}

func TestFenToYuan(t *testing.T) {
	cases := map[int64]string{0: "0.00", 1: "0.01", 99: "0.99", 100: "1.00", 1234: "12.34"}
	for fen, want := range cases {
		if got := FenToYuan(fen); got != want {
			t.Errorf("FenToYuan(%d) = %q, want %q", fen, got, want)
		}
	}
}
