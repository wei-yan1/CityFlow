package logic

import "testing"

func TestUniqueIDsDeduplicatesBoundsAndRejectsEmpty(t *testing.T) {
	ids := uniqueIDs([]string{" r1 ", "r1", "", "r2", "r3", "r4"}, 3)
	if len(ids) != 3 || ids[0] != "r1" || ids[1] != "r2" || ids[2] != "r3" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestUniqueIDsFallsBackToFifty(t *testing.T) {
	raw := make([]string, 80)
	for i := range raw {
		raw[i] = "id-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if got := uniqueIDs(raw, 0); len(got) != 50 {
		t.Fatalf("len = %d", len(got))
	}
}
