package merchant

import "time"

type Review struct {
	ID          string    `json:"id"`
	OrderID     string    `json:"order_id,omitempty"`
	PlaceSource string    `json:"-"`
	PlaceID     string    `json:"-"`
	UserID      string    `json:"-"`
	Anonymous   bool      `json:"-"`
	AuthorLabel string    `json:"-"`
	Rating      int       `json:"rating"`
	Comment     string    `json:"comment"`
	CreatedAt   time.Time `json:"created_at"`
	Likes       int       `json:"likes"`
	Liked       bool      `json:"liked"`
	Mine        bool      `json:"mine"`
}
type ReviewRequest struct {
	Rating    int    `json:"rating"`
	Comment   string `json:"comment"`
	OrderID   string `json:"order_id"`
	Anonymous bool   `json:"anonymous,omitempty"`
}
