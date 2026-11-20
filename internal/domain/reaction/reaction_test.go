package reaction

import "testing"

func TestCanonicalTargetIDKeepsPlainIDs(t *testing.T) {
	for _, targetType := range []string{TargetPlaceReview, TargetRiderReview, TargetMerchant, TargetPlaceComment, TargetRiderComment} {
		id, err := CanonicalTargetID(targetType, "amap", "abc")
		if err != nil || id != "abc" {
			t.Fatalf("%s id=%q err=%v", targetType, id, err)
		}
	}
}

func TestCanonicalTargetIDPrefixesPlaceSource(t *testing.T) {
	id, err := CanonicalTargetID(TargetPlace, "amap", "B0FF123")
	if err != nil || id != "amap:B0FF123" {
		t.Fatalf("place id=%q err=%v", id, err)
	}
	if _, err := CanonicalTargetID(TargetPlace, "", "B0FF123"); err == nil {
		t.Fatal("place target accepted an empty source")
	}
	if _, err := CanonicalTargetID(TargetPlace, "amap", "amap:B0FF123"); err != nil {
		t.Fatalf("already canonical id rejected: %v", err)
	}
}

func TestReactionWhitelist(t *testing.T) {
	if ValidTargetType("UNKNOWN") || !ValidTargetType(TargetPlaceComment) {
		t.Fatal("target type whitelist is wrong")
	}
	if ValidKind("LOVE") || !ValidKind(KindLike) || !ValidKind(KindFavorite) {
		t.Fatal("kind whitelist is wrong")
	}
}
