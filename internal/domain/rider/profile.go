package rider

import "time"

type Profile struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"user_id"`
	DisplayName        string    `json:"display_name"`
	VehicleType        string    `json:"vehicle_type"`
	Skills             []string  `json:"skills"`
	ServiceArea        string    `json:"service_area,omitempty"`
	MaxDistanceMeters  int       `json:"max_distance_meters"`
	MaxConcurrentTasks int       `json:"max_concurrent_tasks"`
	Status             string    `json:"status"`
	Rating             float64   `json:"rating"`
	CompletedTasks     int       `json:"completed_tasks"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Archived           bool      `json:"-"`
}

type UpdateRequest struct {
	DisplayName        string   `json:"display_name"`
	VehicleType        string   `json:"vehicle_type"`
	Skills             []string `json:"skills"`
	ServiceArea        string   `json:"service_area"`
	MaxDistanceMeters  int      `json:"max_distance_meters"`
	MaxConcurrentTasks int      `json:"max_concurrent_tasks"`
}

type AvailabilityRequest struct {
	Status string `json:"status"`
}
