package twilio

import (
"crypto/hmac"
"crypto/sha1" //nolint:gosec
"encoding/base64"
"net/url"
"strings"
"testing"
)

func TestBuildAlertBody_WithButtons(t *testing.T) {
buttons := []ActionButton{
{ID: "rollback", Title: "Rollback deployment"},
{ID: "scale", Title: "Scale down to 1"},
{ID: "ack", Title: "Acknowledge only"},
}
body := buildAlertBody("API latency > 5s", "critical", "Try rolling back to v1.2.3", buttons)

for _, want := range []string{
"INCIDENT ALERT",
"API latency > 5s",
"CRITICAL",
"Try rolling back",
"1.",
"Rollback deployment",
"2.",
"Scale down to 1",
} {
if !strings.Contains(body, want) {
t.Errorf("alert body missing %q\nbody:\n%s", want, body)
}
}
}

func TestBuildAlertBody_NoButtons(t *testing.T) {
body := buildAlertBody("Pod crash loop", "high", "", nil)
if !strings.Contains(body, "INCIDENT ALERT") {
t.Errorf("expected INCIDENT ALERT in body")
}
if strings.Contains(body, "Reply with the number") {
t.Errorf("should not contain button instructions when no buttons")
}
}

func TestSeverityEmoji(t *testing.T) {
cases := map[string]string{
"critical": "🚨",
"high":     "🔴",
"medium":   "🟡",
"low":      "🔵",
"unknown":  "🔵",
}
for sev, want := range cases {
got := severityEmoji(sev)
if got != want {
t.Errorf("severityEmoji(%q) = %q, want %q", sev, got, want)
}
}
}

func TestEnsureWhatsAppPrefix(t *testing.T) {
cases := []struct {
in, want string
}{
{"+14155238886", "whatsapp:+14155238886"},
{"whatsapp:+14155238886", "whatsapp:+14155238886"},
}
for _, c := range cases {
got := ensureWhatsAppPrefix(c.in)
if got != c.want {
t.Errorf("ensureWhatsAppPrefix(%q) = %q, want %q", c.in, got, c.want)
}
}
}

func TestValidateSignature_InvalidSignature(t *testing.T) {
c := &Client{authToken: "mysecret"}
err := c.ValidateSignature("https://example.com/webhook", "badsig", url.Values{})
if err == nil {
t.Error("expected error for invalid signature, got nil")
}
}

func TestValidateSignature_ValidSignature(t *testing.T) {
authToken := "test123"
rawURL := "https://example.com/webhook"

// Compute the expected HMAC the same way as ValidateSignature.
mac := hmac.New(sha1.New, []byte(authToken)) //nolint:gosec
mac.Write([]byte(rawURL))
sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

c := &Client{authToken: authToken}
err := c.ValidateSignature(rawURL, sig, url.Values{})
if err != nil {
t.Errorf("expected valid signature, got error: %v", err)
}
}

func TestValidateSignature_WithPostParams(t *testing.T) {
authToken := "test123"
rawURL := "https://example.com/twilio-incoming"
params := url.Values{
"Body": []string{"rollback"},
"From": []string{"whatsapp:+5511999999999"},
}

// Reproduce the string-to-sign: URL + sorted params.
s := rawURL + "Bodyrollback" + "Fromwhatsapp:+5511999999999"
mac := hmac.New(sha1.New, []byte(authToken)) //nolint:gosec
mac.Write([]byte(s))
sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

c := &Client{authToken: authToken}
err := c.ValidateSignature(rawURL, sig, params)
if err != nil {
t.Errorf("expected valid signature, got error: %v", err)
}
}
