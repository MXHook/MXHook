package parser

import (
	"fmt"
	"io"
	"time"

	"github.com/jhillyerd/enmime"
	"github.com/mxhook/mxhook/types"
)

// Parser converts raw RFC 5322 email into an EmailEvent.
type Parser struct{}

// New creates a Parser.
func New() *Parser { return &Parser{} }

// Parse reads a raw email from r and returns a structured EmailEvent.
func (p *Parser) Parse(id, envelopeFrom string, envelopeTo []string, r io.Reader) (*types.EmailEvent, error) {
	env, err := enmime.ReadEnvelope(r)
	if err != nil {
		return nil, fmt.Errorf("parser: read envelope: %w", err)
	}

	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1,
		ID:            id,
		TraceID:       id,
		MessageID:     env.GetHeader("Message-ID"),
		ReceivedAt:    time.Now().UTC(),
		EnvelopeFrom:  envelopeFrom,
		EnvelopeTo:    envelopeTo,
		Subject:       env.GetHeader("Subject"),
		TextBody:      env.Text,
		HTMLBody:      env.HTML,
		Headers:       headersToMap(env),
	}

	for _, att := range env.Attachments {
		event.Attachments = append(event.Attachments, types.Attachment{
			Filename:    att.FileName,
			ContentType: att.ContentType,
			Size:        int64(len(att.Content)),
		})
	}

	return event, nil
}

func headersToMap(env *enmime.Envelope) map[string][]string {
	h := make(map[string][]string)
	for _, k := range []string{"From", "To", "CC", "Reply-To", "Date", "References", "In-Reply-To"} {
		if v := env.GetHeader(k); v != "" {
			h[k] = []string{v}
		}
	}
	return h
}
