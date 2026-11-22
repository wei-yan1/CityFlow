package paymentservice

// TaskView is Payment's local projection of the facts it needs from Task.
// Task owns the aggregate; Payment reads it over RPC and never imports Task's
// domain model or database.
type TaskView struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	CreatedBy       string `json:"created_by"`
	WorkerID        string `json:"worker_id,omitempty"`
	QuotedAmountFen *int64 `json:"quoted_amount_fen,omitempty"`
	CancelReason    string `json:"cancel_reason,omitempty"`
}

const (
	TaskStatusOpen      = "OPEN"
	TaskStatusCompleted = "COMPLETED"
	TaskStatusCancelled = "CANCELLED"
)
