package types

import "time"

const EmailEventSchemaV1 = "mxhook.email.v1"

// EmailEvent is the structured representation of a received email.
type EmailEvent struct {
	SchemaVersion string              `json:"schema_version"`
	ID            string              `json:"id"`
	TraceID       string              `json:"trace_id"`
	MessageID     string              `json:"message_id,omitempty"`
	ReceivedAt    time.Time           `json:"received_at"`
	EnvelopeFrom  string              `json:"envelope_from"`
	EnvelopeTo    []string            `json:"envelope_to"`
	Headers       map[string][]string `json:"headers"`
	Subject       string              `json:"subject"`
	TextBody      string              `json:"text_body,omitempty"`
	HTMLBody      string              `json:"html_body,omitempty"`
	RawPath       string              `json:"-"`
	Attachments   []Attachment        `json:"attachments"`
}

// Attachment holds metadata for a parsed MIME attachment.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	StoragePath string `json:"-"`
}
