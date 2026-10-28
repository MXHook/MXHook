package state

import (
	"time"

	"github.com/mxhook/mxhook/types"
)

type Delivery struct {
	ID          string
	MessageID   string
	RouteID     string
	WebhookURL  string
	Secret      string
	Event       *types.EmailEvent
	Attempt     int
	Generation  int
	Receipt     string
	LockedUntil time.Time
}

type DeliveryRecord struct {
	ID               string    `json:"id"`
	MessageID        string    `json:"message_id"`
	RouteID          string    `json:"route_id"`
	WebhookURL       string    `json:"webhook_url"`
	State            string    `json:"state"`
	AttemptCount     int       `json:"attempt_count"`
	ReplayGeneration int       `json:"replay_generation"`
	NextAttemptAt    time.Time `json:"next_attempt_at"`
	LastError        string    `json:"last_error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
