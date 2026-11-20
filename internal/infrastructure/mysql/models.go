package mysqlstore

import (
	"encoding/json"
	"time"

	"cityflow/internal/domain/location"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
)

// Persistence records keep GORM-specific schema details out of the domain models.
type userRecord struct {
	ID           string     `gorm:"column:id;type:char(36);primaryKey"`
	Email        string     `gorm:"column:email;size:254;not null;uniqueIndex:users_email_unique"`
	Phone        *string    `gorm:"column:phone;size:32;uniqueIndex:users_phone_unique"`
	PasswordHash string     `gorm:"column:password_hash;size:255;not null"`
	DisplayName  string     `gorm:"column:display_name;size:80;not null"`
	AvatarURL    *string    `gorm:"column:avatar_url;type:text"`
	Role         string     `gorm:"column:role;size:20;not null"`
	Status       string     `gorm:"column:status;size:20;not null"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

func (userRecord) TableName() string { return "users" }

type passwordResetRecord struct {
	TokenHash string    `gorm:"column:token_hash;type:char(64);primaryKey"`
	UserID    string    `gorm:"column:user_id;type:char(36);not null"`
	Email     string    `gorm:"column:email;size:254;not null"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (passwordResetRecord) TableName() string { return "password_reset_tokens" }

type merchantRecord struct {
	ID              string    `gorm:"column:id;type:char(36);primaryKey"`
	UserID          string    `gorm:"column:user_id;type:char(36);not null;uniqueIndex:merchant_profiles_user_unique"`
	Name            string    `gorm:"column:store_name;size:120;not null"`
	Description     *string   `gorm:"column:description;type:text"`
	Phone           *string   `gorm:"column:contact_phone;size:32"`
	Address         string    `gorm:"column:address;type:text;not null"`
	AMAPPOIID       *string   `gorm:"column:amap_poi_id;size:128"`
	Latitude        float64   `gorm:"column:latitude;type:decimal(10,7);not null"`
	Longitude       float64   `gorm:"column:longitude;type:decimal(10,7);not null"`
	BusinessStatus  string    `gorm:"column:business_status;size:20;not null"`
	ServiceTypes    string    `gorm:"column:service_types;type:json;not null"`
	CapacityPerHour int       `gorm:"column:capacity_per_hour;not null"`
	PreparationMins int       `gorm:"column:preparation_minutes;not null"`
	BusinessHours   string    `gorm:"column:business_hours;type:json;not null"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
	Archived        bool      `gorm:"column:is_archived;not null"`
}

func (merchantRecord) TableName() string { return "merchant_profiles" }

type riderRecord struct {
	ID                 string     `gorm:"column:id;type:char(36);primaryKey"`
	UserID             string     `gorm:"column:user_id;type:char(36);not null;uniqueIndex:rider_profiles_user_unique"`
	DisplayName        string     `gorm:"column:display_name;size:80;not null"`
	VehicleType        string     `gorm:"column:vehicle_type;size:24;not null"`
	Status             string     `gorm:"column:status;size:20;not null"`
	ServiceTypes       string     `gorm:"column:service_types;type:json;not null"`
	ServiceArea        *string    `gorm:"column:service_area;size:160"`
	MaxDistanceMeters  int        `gorm:"column:max_distance_meters;not null"`
	MaxConcurrentTasks int        `gorm:"column:max_concurrent_tasks;not null"`
	Rating             float64    `gorm:"column:rating;type:decimal(3,2);not null"`
	CompletedTasks     int        `gorm:"column:completed_tasks;not null"`
	OnTimeRate         float64    `gorm:"column:on_time_rate;type:decimal(5,2);not null"`
	CurrentLatitude    *float64   `gorm:"column:current_latitude;type:decimal(10,7)"`
	CurrentLongitude   *float64   `gorm:"column:current_longitude;type:decimal(10,7)"`
	LocationUpdatedAt  *time.Time `gorm:"column:location_updated_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
	Archived           bool       `gorm:"column:is_archived;not null"`
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

func toUser(record userRecord) *user.User {
	return &user.User{
		ID: record.ID, Email: record.Email, Name: record.DisplayName,
		Phone: stringValue(record.Phone), AvatarURL: stringValue(record.AvatarURL),
		Role: record.Role, Status: record.Status, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt, PasswordHash: record.PasswordHash,
	}
}

func toMerchant(record merchantRecord) (*merchant.Profile, error) {
	var serviceTypes []string
	if err := json.Unmarshal([]byte(record.ServiceTypes), &serviceTypes); err != nil {
		return nil, err
	}
	return &merchant.Profile{
		ID: record.ID, UserID: record.UserID, Name: record.Name,
		Description: stringValue(record.Description), Phone: stringValue(record.Phone),
		Address: record.Address, AMAPPOIID: stringValue(record.AMAPPOIID), Latitude: record.Latitude, Longitude: record.Longitude,
		BusinessStatus: record.BusinessStatus, ServiceTypes: serviceTypes,
		CapacityPerHour: record.CapacityPerHour, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt, Archived: record.Archived,
	}, nil
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
		Destination: record.Destination, Status: record.Status, CreatedBy: record.CreatedBy,
		WorkerID: stringValue(record.WorkerID), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
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
	encoded, err := json.Marshal(values)
	return string(encoded), err
}
