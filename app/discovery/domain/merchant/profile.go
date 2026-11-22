package merchant

import "time"

type Profile struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Phone           string    `json:"phone,omitempty"`
	Address         string    `json:"address,omitempty"`
	AMAPPOIID       string    `json:"amap_poi_id,omitempty"`
	Latitude        float64   `json:"latitude"`
	Longitude       float64   `json:"longitude"`
	BusinessStatus  string    `json:"business_status"`
	ServiceTypes    []string  `json:"service_types"`
	CapacityPerHour int       `json:"capacity_per_hour"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Archived        bool      `json:"-"`
}

type UpdateRequest struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Phone           string   `json:"phone"`
	Address         string   `json:"address"`
	AMAPPOIID       string   `json:"amap_poi_id"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	BusinessStatus  string   `json:"business_status"`
	ServiceTypes    []string `json:"service_types"`
	CapacityPerHour int      `json:"capacity_per_hour"`
}
