package queue

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrIDCollision = errors.New("queue: message ID collision")
	ErrLeaseLost   = errors.New("queue: lease lost")
)

const MessageSchemaV1 = "mxhook.message.v1"

// Message is the versioned ingress envelope shared by every queue backend.
type Message struct {
	SchemaVersion string    `json:"schema_version"`
	ID            string    `json:"id"`
	TraceID       string    `json:"trace_id"`
	ReceivedAt    time.Time `json:"received_at"`
	Source        string    `json:"source"`
	SourceID      string    `json:"source_id,omitempty"`
	EnvelopeFrom  string    `json:"envelope_from"`
	EnvelopeTo    []string  `json:"envelope_to"`
	RawSHA256     string    `json:"raw_sha256"`
	RawSize       int64     `json:"raw_size"`
	Body          []byte    `json:"raw"`
}

func NewMessage(id, source, sourceID, envelopeFrom string, envelopeTo []string, receivedAt time.Time, body []byte) Message {
	digest := sha256.Sum256(body)
	return Message{
		SchemaVersion: MessageSchemaV1,
		ID:            id,
		TraceID:       id,
		ReceivedAt:    receivedAt.UTC(),
		Source:        source,
		SourceID:      sourceID,
		EnvelopeFrom:  envelopeFrom,
		EnvelopeTo:    append([]string(nil), envelopeTo...),
		RawSHA256:     hex.EncodeToString(digest[:]),
		RawSize:       int64(len(body)),
		Body:          append([]byte(nil), body...),
	}
}

func (m Message) Validate() error {
	switch {
	case m.SchemaVersion != MessageSchemaV1:
		return fmt.Errorf("queue: unsupported message schema %q", m.SchemaVersion)
	case m.ID == "":
		return errors.New("queue: message ID is required")
	case m.TraceID == "":
		return errors.New("queue: trace ID is required")
	case m.ReceivedAt.IsZero():
		return errors.New("queue: received time is required")
	case m.Source == "":
		return errors.New("queue: source is required")
	case len(m.EnvelopeTo) == 0:
		return errors.New("queue: at least one envelope recipient is required")
	case len(m.Body) == 0:
		return errors.New("queue: raw message is required")
	case m.RawSize != int64(len(m.Body)):
		return errors.New("queue: raw message size mismatch")
	}
	digest := sha256.Sum256(m.Body)
	if m.RawSHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("queue: raw message digest mismatch")
	}
	for _, recipient := range m.EnvelopeTo {
		if recipient == "" {
			return errors.New("queue: envelope recipient is empty")
		}
	}
	return nil
}

func EncodeMessage(m Message) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("queue: encode message: %w", err)
	}
	return encoded, nil
}

// SameContent reports whether two messages carry the same ingress content:
// identical raw digest and size plus envelope metadata. Encoded-byte equality
// is deliberately not required, so encoder or storage representation changes
// never masquerade as ID collisions.
func (m Message) SameContent(other Message) bool {
	if len(m.EnvelopeTo) != len(other.EnvelopeTo) {
		return false
	}
	for i := range m.EnvelopeTo {
		if m.EnvelopeTo[i] != other.EnvelopeTo[i] {
			return false
		}
	}
	return m.SchemaVersion == other.SchemaVersion &&
		m.ID == other.ID &&
		m.TraceID == other.TraceID &&
		m.ReceivedAt.Equal(other.ReceivedAt) &&
		m.Source == other.Source &&
		m.SourceID == other.SourceID &&
		m.EnvelopeFrom == other.EnvelopeFrom &&
		m.RawSHA256 == other.RawSHA256 &&
		m.RawSize == other.RawSize
}

// MatchesEncoded reports whether stored, a previously persisted encoded
// payload, represents the same message content as msg. An undecodable or
// invalid stored payload never matches.
func MatchesEncoded(stored []byte, msg Message) bool {
	existing, err := DecodeMessage(stored)
	if err != nil {
		return false
	}
	return existing.SameContent(msg)
}

func DecodeMessage(encoded []byte) (Message, error) {
	var message Message
	if err := json.Unmarshal(encoded, &message); err != nil {
		return Message{}, fmt.Errorf("queue: decode message: %w", err)
	}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

// Enqueuer is the interface for adding messages to the queue.
type Enqueuer interface {
	Enqueue(ctx context.Context, msg Message) error
}

// Delivery is one leased attempt to process a message. Receipt is opaque and
// fences state changes from stale workers after a redelivery.
type Delivery struct {
	Message     Message
	Receipt     string
	LockedUntil time.Time
	Attempt     int
}

// Dequeuer is the production consumption contract. Claim is pull-based so
// a lease does not begin before a worker is ready to process it.
type Dequeuer interface {
	Claim(ctx context.Context) (Delivery, error)
	Ack(ctx context.Context, id, receipt string) error
	Nack(ctx context.Context, id, receipt, reason string) error
	Extend(ctx context.Context, id, receipt string, extension time.Duration) (time.Time, error)
}

// NewReceiptHandle returns an opaque 128-bit lease receipt.
func NewReceiptHandle() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("queue: generate receipt handle: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

// NoopEnqueuer discards all messages. Useful in tests.
type NoopEnqueuer struct{}

func (n *NoopEnqueuer) Enqueue(_ context.Context, _ Message) error {
	return nil
}
