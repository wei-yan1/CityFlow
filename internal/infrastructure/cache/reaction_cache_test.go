package cache

import (
	"encoding/json"
	"testing"
)

func TestProjectionMessageIsAbsoluteCounts(t *testing.T) {
	payload, err := ProjectionMessage("PLACE_REVIEW", []Projection{
		{TargetID: "r1", Count: 7},
		{TargetID: "r2", Count: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Type  string `json:"type"`
		Items []struct {
			TargetID string `json:"target_id"`
			Count    int64  `json:"count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "PLACE_REVIEW" || len(decoded.Items) != 2 {
		t.Fatalf("payload = %s", payload)
	}
	if decoded.Items[0].TargetID != "r1" || decoded.Items[0].Count != 7 {
		t.Fatalf("first item = %+v", decoded.Items[0])
	}
	if decoded.Items[1].TargetID != "r2" || decoded.Items[1].Count != 0 {
		t.Fatalf("second item = %+v", decoded.Items[1])
	}
}

func TestStringFieldDecodesRedisValues(t *testing.T) {
	values := map[string]any{"type": "PLACE_REVIEW", "id": []byte("r1"), "count": int64(3)}
	if got := stringField(values, "type"); got != "PLACE_REVIEW" {
		t.Fatalf("string = %q", got)
	}
	if got := stringField(values, "id"); got != "r1" {
		t.Fatalf("bytes = %q", got)
	}
	if got := stringField(values, "missing"); got != "" {
		t.Fatalf("missing = %q", got)
	}
}
