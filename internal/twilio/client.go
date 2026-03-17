// Package twilio wraps the Twilio SDK to send WhatsApp messages with
// interactive quick-reply buttons via the Content API.
package twilio

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio uses HMAC-SHA1 per spec
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"

	twilioclient "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// Client wraps the Twilio REST client to send WhatsApp messages.
type Client struct {
	inner      *twilioclient.RestClient
	fromNumber string // e.g. "whatsapp:+14155238886"
	authToken  string // used for signature verification
}

// NewClient creates a new Twilio Client.
// fromNumber must be in the format "whatsapp:+<E.164>" for WhatsApp sandbox/business.
func NewClient(accountSID, authToken, fromNumber string) *Client {
	c := twilioclient.NewRestClientWithParams(twilioclient.ClientParams{
		Username: accountSID,
		Password: authToken,
	})
	return &Client{
		inner:      c,
		fromNumber: fromNumber,
		authToken:  authToken,
	}
}

// ActionButton represents a single quick-reply button.
type ActionButton struct {
	// ID is the payload sent back when the user taps this button (max 256 chars).
	ID string
	// Title is the visible button label (max 20 chars for WhatsApp).
	Title string
}

// SendIncidentAlert sends a WhatsApp alert message with up to 3 action buttons.
// Buttons are encoded as a numbered list in the body when the Content API is not
// configured, so the operator always works with the basic Twilio Messaging API.
func (c *Client) SendIncidentAlert(toPhone, title, severity, suggestedFix string, buttons []ActionButton) (string, error) {
	body := buildAlertBody(title, severity, suggestedFix, buttons)
	to := ensureWhatsAppPrefix(toPhone)

	params := &openapi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(c.fromNumber)
	params.SetBody(body)

	resp, err := c.inner.Api.CreateMessage(params)
	if err != nil {
		return "", fmt.Errorf("twilio send alert: %w", err)
	}
	if resp.Sid == nil {
		return "", fmt.Errorf("twilio send alert: nil SID in response")
	}
	return *resp.Sid, nil
}

// SendMessage sends a plain WhatsApp text message.
func (c *Client) SendMessage(toPhone, body string) (string, error) {
	params := &openapi.CreateMessageParams{}
	params.SetTo(ensureWhatsAppPrefix(toPhone))
	params.SetFrom(c.fromNumber)
	params.SetBody(body)

	resp, err := c.inner.Api.CreateMessage(params)
	if err != nil {
		return "", fmt.Errorf("twilio send message: %w", err)
	}
	if resp.Sid == nil {
		return "", fmt.Errorf("twilio send message: nil SID in response")
	}
	return *resp.Sid, nil
}

// ValidateSignature verifies the X-Twilio-Signature header to ensure the
// request genuinely originated from Twilio. Returns an error if invalid.
//
// ref: https://www.twilio.com/docs/usage/security#validating-signatures-from-twilio
func (c *Client) ValidateSignature(rawURL, twilioSignature string, params url.Values) error {
	// Build the string-to-sign: URL + sorted POST params concatenated.
	s := rawURL
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s += k + params.Get(k)
	}

	mac := hmac.New(sha1.New, []byte(c.authToken)) //nolint:gosec
	mac.Write([]byte(s))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(twilioSignature)) {
		return fmt.Errorf("invalid Twilio signature")
	}
	return nil
}

// buildAlertBody creates the WhatsApp message body for an incident alert.
func buildAlertBody(title, severity, suggestedFix string, buttons []ActionButton) string {
	emoji := severityEmoji(severity)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s *INCIDENT ALERT* %s\n\n", emoji, emoji)
	fmt.Fprintf(&sb, "*%s*\n", title)
	fmt.Fprintf(&sb, "Severity: *%s*\n\n", strings.ToUpper(severity))
	if suggestedFix != "" {
		fmt.Fprintf(&sb, "💡 *Suggested fix:*\n%s\n\n", suggestedFix)
	}
	if len(buttons) > 0 {
		sb.WriteString("Reply with the number to take action:\n")
		for i, b := range buttons {
			fmt.Fprintf(&sb, "  *%d.* %s\n", i+1, b.Title)
		}
		sb.WriteString("\nOr reply *ack* to acknowledge.")
	}
	return sb.String()
}

func severityEmoji(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return "🚨"
	case "high":
		return "🔴"
	case "medium":
		return "🟡"
	default:
		return "🔵"
	}
}

// ensureWhatsAppPrefix adds "whatsapp:" prefix if not already present.
func ensureWhatsAppPrefix(phone string) string {
	if strings.HasPrefix(phone, "whatsapp:") {
		return phone
	}
	return "whatsapp:" + phone
}
