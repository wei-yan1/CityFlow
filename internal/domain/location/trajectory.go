package location

// TrackPoint is a rider observation returned for a task. Samples are stored
// per rider; TaskID is resolved at query time from the task's assignment window.
type TrackPoint struct {
	ID     uint64 `json:"id"`
	TaskID string `json:"task_id"`
	Position
}

type TrackSegment struct {
	Coordinates []Point `json:"coordinates"`
	Mode        string  `json:"mode"`
	Confidence  float64 `json:"confidence,omitempty"`
}
type TrackDisplay struct {
	CoordinateSystem string         `json:"coordinate_system"`
	Segments         []TrackSegment `json:"segments"`
	RejectedPoints   int            `json:"rejected_points"`
	Gaps             int            `json:"gaps"`
	Notice           string         `json:"notice"`
	Attribution      string         `json:"attribution,omitempty"`
}
