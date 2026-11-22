package taskmysql

import (
	"encoding/json"
	"time"

	"cityflow/app/task/domain/rider"
	"cityflow/app/task/domain/task"
	"cityflow/internal/common/location"
)

type riderRecord struct {
	ID                 string    `gorm:"column:id;type:char(36);primaryKey"`
	UserID             string    `gorm:"column:user_id;type:char(36);not null;uniqueIndex:rider_profiles_user_unique"`
	DisplayName        string    `gorm:"column:display_name;size:80;not null"`
	VehicleType        string    `gorm:"column:vehicle_type;size:24;not null"`
	Status             string    `gorm:"column:status;size:20;not null"`
	ServiceTypes       string    `gorm:"column:service_types;type:json;not null"`
	ServiceArea        *string   `gorm:"column:service_area;size:160"`
	MaxDistanceMeters  int       `gorm:"column:max_distance_meters;not null"`
	MaxConcurrentTasks int       `gorm:"column:max_concurrent_tasks;not null"`
	Rating             float64   `gorm:"column:rating;type:decimal(3,2);not null"`
	CompletedTasks     int       `gorm:"column:completed_tasks;not null"`
	OnTimeRate         float64   `gorm:"column:on_time_rate;type:decimal(5,2);not null"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
	Archived           bool      `gorm:"column:is_archived;not null"`
}

func (riderRecord) TableName() string { return "rider_profiles" }

type taskRecord struct {
	QuotedAmountFen     *int64            `gorm:"column:quoted_amount_fen"`
	DestinationPOIID    string            `gorm:"column:destination_poi_id"`
	IdempotencyKey      *string           `gorm:"column:idempotency_key"`
	IdempotencyHash     *string           `gorm:"column:idempotency_hash"`
	PickupStops         []task.PickupStop `gorm:"column:pickup_stops;type:json;serializer:json"`
	DeliveryAt          *time.Time        `gorm:"column:delivery_at"`
	PickupLocation      *location.Point   `gorm:"column:pickup_location;type:json;serializer:json"`
	DestinationLocation *location.Point   `gorm:"column:destination_location;type:json;serializer:json"`
	ID                  string            `gorm:"column:id;type:char(36);primaryKey"`
	Title               string            `gorm:"column:title;size:160;not null"`
	Description         *string           `gorm:"column:description;type:text"`
	ContactName         string            `gorm:"column:contact_name;size:80;not null"`
	ContactPhone        string            `gorm:"column:contact_phone;size:32;not null"`
	HandoverNote        *string           `gorm:"column:handover_note;type:text"`
	Type                string            `gorm:"column:task_type;size:40;not null"`
	Priority            int               `gorm:"column:priority;not null"`
	Pickup              string            `gorm:"column:pickup;type:text;not null"`
	Destination         string            `gorm:"column:destination;type:text;not null"`
	Status              string            `gorm:"column:status;size:24;not null"`
	CancelledAt         *time.Time        `gorm:"column:cancelled_at"`
	CompletedAt         *time.Time        `gorm:"column:completed_at"`
	CancelReason        string            `gorm:"column:cancel_reason;size:255;not null;default:''"`
	CreatedBy           string            `gorm:"column:created_by;type:char(36);not null"`
	WorkerID            *string           `gorm:"column:worker_id;type:char(36)"`
	CreatedAt           time.Time         `gorm:"column:created_at"`
	UpdatedAt           time.Time         `gorm:"column:updated_at"`
}

func (taskRecord) TableName() string { return "tasks" }

type taskEventRecord struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TaskID    string    `gorm:"column:task_id;type:char(36);not null"`
	From      *string   `gorm:"column:from_status;size:24"`
	To        string    `gorm:"column:to_status;size:24;not null"`
	ActorID   *string   `gorm:"column:actor_id;type:char(36)"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (taskEventRecord) TableName() string { return "task_events" }

type taskIncidentRecord struct {
	ID           string     `gorm:"column:id;type:char(32);primaryKey"`
	TaskID       string     `gorm:"column:task_id;type:char(36);not null"`
	ReporterID   string     `gorm:"column:reporter_id;type:char(36);not null"`
	ReporterRole string     `gorm:"column:reporter_role;size:16;not null"`
	Kind         string     `gorm:"column:kind;size:32;not null"`
	Note         string     `gorm:"column:note;size:500;not null"`
	ImageURLs    []string   `gorm:"column:image_urls;type:json;serializer:json"`
	Status       string     `gorm:"column:status;size:16;not null"`
	Resolution   string     `gorm:"column:resolution;size:500;not null"`
	ResolvedBy   *string    `gorm:"column:resolved_by;type:char(36)"`
	ResolvedAt   *time.Time `gorm:"column:resolved_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
}

func (taskIncidentRecord) TableName() string { return "task_incidents" }

func toIncident(record taskIncidentRecord) task.Incident {
	return task.Incident{
		ID: record.ID, TaskID: record.TaskID, ReporterID: record.ReporterID, ReporterRole: record.ReporterRole,
		Kind: record.Kind, Note: record.Note, ImageURLs: record.ImageURLs, Status: record.Status,
		Resolution: record.Resolution, ResolvedBy: stringValue(record.ResolvedBy), CreatedAt: record.CreatedAt,
		ResolvedAt: record.ResolvedAt,
	}
}

func toRider(record riderRecord) (*rider.Profile, error) {
	var skills []string
	if err := json.Unmarshal([]byte(record.ServiceTypes), &skills); err != nil {
		return nil, err
	}
	return &rider.Profile{
		ID: record.ID, UserID: record.UserID, DisplayName: record.DisplayName,
		VehicleType: record.VehicleType, Skills: skills, ServiceArea: stringValue(record.ServiceArea),
		MaxDistanceMeters: record.MaxDistanceMeters, MaxConcurrentTasks: record.MaxConcurrentTasks,
		Status: record.Status, Rating: record.Rating, CompletedTasks: record.CompletedTasks,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Archived: record.Archived,
	}, nil
}

func toTask(record taskRecord) *task.Task {
	return &task.Task{
		QuotedAmountFen:  record.QuotedAmountFen,
		DestinationPOIID: record.DestinationPOIID,
		PickupStops:      record.PickupStops, DeliveryAt: record.DeliveryAt,
		PickupLocation: record.PickupLocation, DestinationLocation: record.DestinationLocation,
		ID: record.ID, Title: record.Title, Description: stringValue(record.Description),
		ContactName: record.ContactName, ContactPhone: record.ContactPhone, HandoverNote: stringValue(record.HandoverNote),
		Type: record.Type, Priority: record.Priority, Pickup: record.Pickup,
		Destination: record.Destination, Status: record.Status, CancelReason: record.CancelReason, CreatedBy: record.CreatedBy,
		WorkerID: stringValue(record.WorkerID), CancelledAt: record.CancelledAt, CompletedAt: record.CompletedAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func toTaskEvent(record taskEventRecord) task.TaskEvent {
	return task.TaskEvent{
		From: stringValue(record.From), To: record.To,
		ActorID: stringValue(record.ActorID), CreatedAt: record.CreatedAt,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func serviceTypesJSON(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
