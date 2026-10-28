package api

import "time"

// Domain represents a registered email domain.
type Domain struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Route represents a routing rule.
type Route struct {
	ID               string    `json:"id"`
	Domain           string    `json:"domain"`
	RecipientPattern string    `json:"recipient_pattern"`
	WebhookURL       string    `json:"webhook_url"`
	WebhookSecret    string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
}

// DLQEntry is a terminal webhook delivery safe for API serialization.
type DLQEntry struct {
	ID               string    `json:"id"`
	MessageID        string    `json:"message_id"`
	RouteID          string    `json:"route_id"`
	WebhookURL       string    `json:"webhook_url"`
	AttemptCount     int       `json:"attempt_count"`
	ReplayGeneration int       `json:"replay_generation"`
	Reason           string    `json:"reason"`
	FailedAt         time.Time `json:"failed_at"`
}

// Message represents a processed email event (summary).
type Message struct {
	ID         string    `json:"id"`
	MessageID  string    `json:"message_id"`
	From       string    `json:"from"`
	To         []string  `json:"to"`
	Subject    string    `json:"subject"`
	ReceivedAt time.Time `json:"received_at"`
	State      string    `json:"state"`
}
