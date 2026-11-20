// Package comment models review comments and replies. Comments belong to the
// service that owns the review (Discovery for place reviews, Task for rider
// feedback), never to the Reaction service.
package comment

import "time"

const (
	MaxDepth   = 3
	MaxLength  = 500
	MaxPage    = 50
	ThreadPage = 100
)

type Record struct {
	ID             string
	ReviewID       string
	UserID         string
	Anonymous      bool
	AuthorLabel    string
	ReplyToUserID  string
	ReplyToLabel   string
	RootID         string
	ParentID       string
	Depth          int
	Content        string
	Hidden         bool
	CreatedAt      time.Time
	ReplyCount     int
	ReplyToDisplay string
}

type CreateRequest struct {
	ParentID  string `json:"parent_id"`
	Content   string `json:"content"`
	Anonymous bool   `json:"anonymous"`
}

// Public is the only comment shape allowed to reach an untrusted browser.
// It deliberately contains no user IDs: reply targeting is represented by the
// immutable snapshot labels, not by an account identifier.
type Public struct {
	ID           string `json:"id"`
	AuthorLabel  string `json:"author_label"`
	Anonymous    bool   `json:"anonymous"`
	Content      string `json:"content"`
	Date         string `json:"date"`
	RootID       string `json:"root_id"`
	ParentID     string `json:"parent_id,omitempty"`
	Depth        int    `json:"depth"`
	ReplyToLabel string `json:"reply_to_label,omitempty"`
	ReplyCount   int    `json:"reply_count,omitempty"`
	Likes        int    `json:"likes"`
	Liked        bool   `json:"liked"`
}

type List struct {
	Items      []Public `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}
