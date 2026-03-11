package types

import "time"

// EmailEvent is the structured representation of a received email.
type EmailEvent struct {
	ID           string
	MessageID    string
	ReceivedAt   time.Time
	EnvelopeFrom string
	EnvelopeTo   []string
	Headers      map[string][]string
	Subject      string
	TextBody     string
	HTMLBody     string
	RawPath      string // path/key to raw .eml
	Attachments  []Attachment
}

// Attachment holds metadata for a parsed MIME attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	StoragePath string
}
