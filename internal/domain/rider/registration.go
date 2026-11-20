package rider

import "time"

const (
	RegistrationPending  = "PENDING"
	RegistrationApproved = "APPROVED"
	RegistrationRejected = "REJECTED"

	// RegistrationMinimumFen is the minimum rider onboarding payment: one cent.
	RegistrationMinimumFen int64 = 1
)

// Registration is a paid rider application waiting for an administrator to
// verify the payment receipt and create the actual rider account.
type Registration struct {
	ID                 string     `json:"id"`
	Email              string     `json:"email"`
	Phone              string     `json:"phone,omitempty"`
	DisplayName        string     `json:"display_name"`
	VehicleType        string     `json:"vehicle_type"`
	Skills             []string   `json:"skills"`
	ServiceArea        string     `json:"service_area,omitempty"`
	MaxDistanceMeters  int        `json:"max_distance_meters"`
	MaxConcurrentTasks int        `json:"max_concurrent_tasks"`
	PaidAmountFen      int64      `json:"paid_amount_fen"`
	PaymentReference   string     `json:"payment_reference"`
	PaymentNote        string     `json:"payment_note,omitempty"`
	Status             string     `json:"status"`
	ReviewNote         string     `json:"review_note,omitempty"`
	ReviewedBy         string     `json:"reviewed_by,omitempty"`
	ReviewedAt         *time.Time `json:"reviewed_at,omitempty"`
	UserID             string     `json:"user_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	PasswordHash       string     `json:"-"`
}

type RegistrationRequest struct {
	Email              string   `json:"email"`
	Password           string   `json:"password"`
	DisplayName        string   `json:"display_name"`
	Phone              string   `json:"phone"`
	VehicleType        string   `json:"vehicle_type"`
	Skills             []string `json:"skills"`
	ServiceArea        string   `json:"service_area"`
	MaxDistanceMeters  int      `json:"max_distance_meters"`
	MaxConcurrentTasks int      `json:"max_concurrent_tasks"`
	PaidAmountFen      int64    `json:"paid_amount_fen"`
	PaymentReference   string   `json:"payment_reference"`
	PaymentNote        string   `json:"payment_note"`
}
