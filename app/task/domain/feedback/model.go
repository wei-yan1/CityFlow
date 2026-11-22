package feedback

import "time"

const (
	KindReview = "REVIEW"
	KindTip    = "TIP"
	KindReport = "REPORT"
	KindCall   = "CALL"
)

const (
	StatusPublished = "PUBLISHED"
	StatusRecorded  = "RECORDED"
	StatusOpen      = "OPEN"
	StatusResolved  = "RESOLVED"
	StatusDismissed = "DISMISSED"
)

// Record is one atomic user-to-rider feedback event. RiderID, TaskID, UserID,
// CreatedAt and the generated ID are stored together so a review, tip, call or
// report can never lose the user/task/time it belongs to.
type Record struct {
	ID             string     `json:"id"`
	RiderID        string     `json:"rider_id"`
	TaskID         string     `json:"task_id"`
	UserID         string     `json:"user_id"`
	Kind           string     `json:"kind"`
	Rating         int        `json:"rating,omitempty"`
	AmountFen      int64      `json:"amount_fen,omitempty"`
	Comment        string     `json:"comment,omitempty"`
	LikeCount      int        `json:"like_count,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	Details        string     `json:"details,omitempty"`
	Images         []string   `json:"images,omitempty"`
	Status         string     `json:"status"`
	ResolutionNote string     `json:"resolution_note,omitempty"`
	ResolvedBy     string     `json:"resolved_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	RiderName      string     `json:"rider_name,omitempty"`
	UserName       string     `json:"user_name,omitempty"`
	AvatarURL      string     `json:"avatar_url,omitempty"`
	TaskTitle      string     `json:"task_title,omitempty"`
}

type ReviewRequest struct {
	Rating  int    `json:"rating"`
	Comment string `json:"comment"`
}

type TipRequest struct {
	AmountFen int64  `json:"amount_fen"`
	Message   string `json:"message"`
}

type ReportRequest struct {
	Reason  string   `json:"reason"`
	Details string   `json:"details"`
	Images  []string `json:"images"`
}

