package router

// Target defines where a matched email is delivered.
type Target string

const (
	TargetWebhook Target = "webhook"
	TargetQueue   Target = "queue"
	TargetStorage Target = "storage"
)

// Rule defines a routing rule for an email recipient.
type Rule struct {
	ID               string
	Domain           string
	RecipientPattern string // exact, wildcard (*@domain.com), or plus-stripped (base@domain.com)
	Target           Target
	WebhookURL       string
	WebhookSecret    string
	IsCatchAll       bool
	Priority         int
}
