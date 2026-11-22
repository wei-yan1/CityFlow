package location

// TrackPoint is a rider observation returned for a task. Samples are stored
// per rider; TaskID is resolved at query time from the task's assignment window.
type TrackPoint struct {
	ID     uint64 `json:"id"`
	TaskID string `json:"task_id"`
	Position
}
