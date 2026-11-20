package task

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/domain/location"
)

const (
	StatusOpen       = "OPEN"
	StatusClaimed    = "CLAIMED"
	StatusArrived    = "ARRIVED"
	StatusProcessing = "PROCESSING"
	StatusCompleted  = "COMPLETED"
	StatusCancelled  = "CANCELLED"
)

type PickupStop struct {
	POIID    string          `json:"poi_id,omitempty"`
	Address  string          `json:"address"`
	Location *location.Point `json:"location,omitempty"`
	PickupAt *time.Time      `json:"pickup_at,omitempty"`
	Status   string          `json:"status,omitempty"`
}

func AdvancePickup(stops []PickupStop, index int, next string) error {
	if index < 0 || index >= len(stops) {
		return errors.New("invalid pickup index")
	}
	for i := 0; i < index; i++ {
		if stops[i].Status != "PICKED_UP" {
			return errors.New("请先完成前面的取件点")
		}
	}
	current := stops[index].Status
	if current == "" {
		current = "PENDING"
	}
	if !(current == "PENDING" && next == "ARRIVED" || current == "ARRIVED" && next == "PICKED_UP") {
		return errors.New("取件状态已改变，请刷新")
	}
	stops[index].Status = next
	return nil
}

func PickupsComplete(stops []PickupStop) bool {
	if len(stops) < 2 {
		return true
	}
	for _, stop := range stops {
		if stop.Status != "PICKED_UP" {
			return false
		}
	}
	return true
}

type Task struct {
	QuotedAmountFen     *int64          `json:"quoted_amount_fen,omitempty"`
	DestinationPOIID    string          `json:"destination_poi_id,omitempty"`
	PickupStops         []PickupStop    `json:"pickup_stops,omitempty"`
	DeliveryAt          *time.Time      `json:"delivery_at,omitempty"`
	PickupLocation      *location.Point `json:"pickup_location,omitempty"`
	DestinationLocation *location.Point `json:"destination_location,omitempty"`
	ID                  string          `json:"id"`
	Title               string          `json:"title"`
	Description         string          `json:"description,omitempty"`
	ContactName         string          `json:"contact_name,omitempty"`
	ContactPhone        string          `json:"contact_phone,omitempty"`
	HandoverNote        string          `json:"handover_note,omitempty"`
	Type                string          `json:"type"`
	Priority            int             `json:"priority"`
	Pickup              string          `json:"pickup"`
	Destination         string          `json:"destination"`
	Status              string          `json:"status"`
	CreatedBy           string          `json:"created_by"`
	WorkerID            string          `json:"worker_id,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	Events              []TaskEvent     `json:"events"`
	Incidents           []Incident      `json:"incidents,omitempty"`
}

type TaskEvent struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	ActorID   string    `json:"actor_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateRequest struct {
	QuotedAmountFen     *int64          `json:"quoted_amount_fen,omitempty"`
	DestinationPOIID    string          `json:"destination_poi_id,omitempty"`
	IdempotencyKey      string          `json:"-"`
	IdempotencyHash     string          `json:"-"`
	PickupStops         []PickupStop    `json:"pickup_stops,omitempty"`
	DeliveryAt          *time.Time      `json:"delivery_at,omitempty"`
	PickupLocation      *location.Point `json:"pickup_location,omitempty"`
	DestinationLocation *location.Point `json:"destination_location,omitempty"`
	Title               string          `json:"title"`
	Description         string          `json:"description"`
	ContactName         string          `json:"contact_name"`
	ContactPhone        string          `json:"contact_phone"`
	HandoverNote        string          `json:"handover_note"`
	Type                string          `json:"type"`
	Priority            int             `json:"priority"`
	Pickup              string          `json:"pickup"`
	Destination         string          `json:"destination"`
}

type UpdateRequest struct {
	QuotedAmountFen     *int64          `json:"quoted_amount_fen,omitempty"`
	DestinationPOIID    string          `json:"destination_poi_id,omitempty"`
	PickupStops         []PickupStop    `json:"pickup_stops,omitempty"`
	DeliveryAt          *time.Time      `json:"delivery_at,omitempty"`
	PickupLocation      *location.Point `json:"pickup_location,omitempty"`
	DestinationLocation *location.Point `json:"destination_location,omitempty"`
	Title               string          `json:"title"`
	Description         string          `json:"description"`
	ContactName         string          `json:"contact_name"`
	ContactPhone        string          `json:"contact_phone"`
	HandoverNote        string          `json:"handover_note"`
	Type                string          `json:"type"`
	Priority            int             `json:"priority"`
	Pickup              string          `json:"pickup"`
	Destination         string          `json:"destination"`
}

// Incident is a rider or creator report that needs attention without changing
// the task's normal status until an operator resolves it.
type Incident struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	ReporterID   string     `json:"reporter_id"`
	ReporterRole string     `json:"reporter_role"`
	Kind         string     `json:"kind"`
	Note         string     `json:"note"`
	ImageURLs    []string   `json:"image_urls,omitempty"`
	Status       string     `json:"status"`
	Resolution   string     `json:"resolution,omitempty"`
	ResolvedBy   string     `json:"resolved_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
}

type IncidentRequest struct {
	Kind      string   `json:"kind"`
	Note      string   `json:"note"`
	ImageURLs []string `json:"image_urls"`
}

const (
	IncidentOpen     = "OPEN"
	IncidentResolved = "RESOLVED"

	IncidentContactFailed = "CONTACT_FAILED"
	IncidentPickupFailed  = "PICKUP_FAILED"
	IncidentAddressWrong  = "ADDRESS_WRONG"
	IncidentRiderCancel   = "RIDER_CANCEL"
	IncidentUserCancel    = "USER_CANCEL"
	IncidentPlatformHelp  = "PLATFORM_HELP"
	IncidentOther         = "OTHER"
)

func ValidIncidentKind(kind string) bool {
	switch kind {
	case IncidentContactFailed, IncidentPickupFailed, IncidentAddressWrong, IncidentRiderCancel, IncidentUserCancel, IncidentPlatformHelp, IncidentOther:
		return true
	default:
		return false
	}
}

type UpdateStatusRequest struct {
	Status string `json:"status"`
}

func ValidTransition(from, to string) bool {
	if from == StatusOpen {
		return to == StatusCancelled
	}
	switch from {
	case StatusClaimed:
		return to == StatusArrived || to == StatusCancelled
	case StatusArrived:
		return to == StatusProcessing || to == StatusCancelled
	case StatusProcessing:
		return to == StatusCompleted
	default:
		return false
	}
}

func NormalizeCreateRequest(req CreateRequest) CreateRequest {
	for i := range req.PickupStops {
		req.PickupStops[i].Address = strings.TrimSpace(req.PickupStops[i].Address)
	}
	if len(req.PickupStops) > 0 {
		req.Pickup = req.PickupStops[0].Address
		req.PickupLocation = req.PickupStops[0].Location
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.ContactName = strings.TrimSpace(req.ContactName)
	req.ContactPhone = strings.TrimSpace(req.ContactPhone)
	req.HandoverNote = strings.TrimSpace(req.HandoverNote)
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	req.Pickup = strings.TrimSpace(req.Pickup)
	req.Destination = strings.TrimSpace(req.Destination)
	return req
}

func ValidateCreateRequest(req CreateRequest) error {
	if req.QuotedAmountFen != nil && (*req.QuotedAmountFen <= 0 || *req.QuotedAmountFen > 9007199254740991) {
		return errors.New("task amount must be positive integer fen within the supported range")
	}
	if len(req.PickupStops) > 50 {
		return errors.New("too many pickup stops")
	}
	for _, stop := range req.PickupStops {
		if stop.Address == "" || len(stop.Address) > 1000 || (stop.Location != nil && !stop.Location.Valid()) || (stop.Status != "" && stop.Status != "PENDING") {
			return errors.New("invalid pickup stop")
		}
		if stop.PickupAt != nil && req.DeliveryAt != nil && req.DeliveryAt.Before(*stop.PickupAt) {
			return errors.New("delivery time must be after pickup time")
		}
	}
	if (req.PickupLocation != nil && !req.PickupLocation.Valid()) || (req.DestinationLocation != nil && !req.DestinationLocation.Valid()) {
		return errors.New("invalid task coordinates")
	}
	if req.Title == "" || req.Type == "" || req.Pickup == "" || req.Destination == "" {
		return errors.New("title, type, pickup and destination are required")
	}
	if req.Priority < 0 || req.Priority > 10 {
		return fmt.Errorf("priority must be between 0 and 10")
	}
	if len(req.ContactName) > 80 || len(req.ContactPhone) > 32 || len(req.HandoverNote) > 1000 {
		return errors.New("contact fields are too long")
	}
	return nil
}

func NormalizeUpdateRequest(req UpdateRequest) UpdateRequest {
	for i := range req.PickupStops {
		req.PickupStops[i].Address = strings.TrimSpace(req.PickupStops[i].Address)
	}
	if len(req.PickupStops) > 0 {
		req.Pickup = req.PickupStops[0].Address
		req.PickupLocation = req.PickupStops[0].Location
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.ContactName = strings.TrimSpace(req.ContactName)
	req.ContactPhone = strings.TrimSpace(req.ContactPhone)
	req.HandoverNote = strings.TrimSpace(req.HandoverNote)
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	req.Pickup = strings.TrimSpace(req.Pickup)
	req.Destination = strings.TrimSpace(req.Destination)
	return req
}

func ValidateUpdateRequest(req UpdateRequest) error {
	return ValidateCreateRequest(CreateRequest{
		QuotedAmountFen: req.QuotedAmountFen,
		PickupStops:     req.PickupStops, DeliveryAt: req.DeliveryAt, Title: req.Title, Description: req.Description, Type: req.Type,
		ContactName: req.ContactName, ContactPhone: req.ContactPhone, HandoverNote: req.HandoverNote,
		PickupLocation: req.PickupLocation, DestinationLocation: req.DestinationLocation,
		Priority: req.Priority, Pickup: req.Pickup, Destination: req.Destination,
	})
}
