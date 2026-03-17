// Package ai provides a client for the Ollama sidecar and Grok API to
// generate incident remediation suggestions and parse natural-language commands.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOllamaURL = "http://localhost:11434"
	defaultModel     = "llama3"
	requestTimeout   = 30 * time.Second
)

// Client talks to an Ollama instance (or compatible API) running as a sidecar.
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewClient creates an AI client pointing at the given Ollama base URL.
// If baseURL is empty, it defaults to http://localhost:11434.
func NewClient(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = defaultOllamaURL
	}
	if model == "" {
		model = defaultModel
	}
	return &Client{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// ollamaGenerateRequest is the payload for the Ollama /api/generate endpoint.
type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaGenerateResponse is the response from the Ollama /api/generate endpoint.
type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// SuggestFix calls the LLM to generate a concise remediation suggestion for the
// given incident. The returned string is suitable for embedding in a WhatsApp message.
func (c *Client) SuggestFix(ctx context.Context, incidentTitle, severity, namespace string) (string, error) {
	prompt := buildSuggestPrompt(incidentTitle, severity, namespace)
	return c.generate(ctx, prompt)
}

// ParseCommand converts a natural-language WhatsApp message into a structured
// action keyword that the incident controller can act upon.
// Returns a normalised action string such as "rollback", "scale:2", "restart", or "ack".
func (c *Client) ParseCommand(ctx context.Context, message, incidentTitle string) (string, error) {
	prompt := buildParsePrompt(message, incidentTitle)
	raw, err := c.generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	return normaliseAction(raw), nil
}

// generate calls the Ollama /api/generate endpoint and returns the response text.
func (c *Client) generate(ctx context.Context, prompt string) (string, error) {
	reqBody, err := json.Marshal(ollamaGenerateRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("ai marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/generate", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("ai new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai http do: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16)) // 64 KB max
	if err != nil {
		return "", fmt.Errorf("ai read body: %w", err)
	}

	var genResp ollamaGenerateResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return "", fmt.Errorf("ai unmarshal response: %w", err)
	}
	return strings.TrimSpace(genResp.Response), nil
}

// buildSuggestPrompt creates the LLM prompt for fix suggestions.
func buildSuggestPrompt(title, severity, namespace string) string {
	return fmt.Sprintf(`You are a Kubernetes SRE expert. An incident has occurred:
Title: %s
Severity: %s
Namespace: %s

Provide a SINGLE concise remediation suggestion (max 2 sentences, plain text, no markdown).
Focus on the most likely Kubernetes action (rollback, scale, restart, check logs, etc.).
Reply in the same language as the title.`, title, severity, namespace)
}

// buildParsePrompt creates the LLM prompt for command parsing.
func buildParsePrompt(message, incidentTitle string) string {
	return fmt.Sprintf(`You are interpreting a WhatsApp reply from an on-call engineer responding to a Kubernetes incident.

Incident: %s
Message: "%s"

Map the message to EXACTLY ONE of these actions (reply with only the action keyword):
- rollback  (e.g. "rollback", "revert", "undo", "voltar versão", "rollback la api")
- restart   (e.g. "restart", "reiniciar", "reinicia")
- scale:N   (e.g. "scale to 3", "escalar a 2 replicas") — replace N with the number
- ack       (e.g. "ack", "acknowledged", "entendido", "ok", "visto")
- resolve   (e.g. "resolved", "fixed", "resolvido", "solucionado")
- ignore    (anything else)

Reply with only the action keyword, nothing else.`, incidentTitle, message)
}

// normaliseAction cleans the LLM output to a single action keyword.
func normaliseAction(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	// Strip any surrounding quotes or punctuation.
	s = strings.Trim(s, `"'.`)
	// Take only first word/line in case LLM adds explanation.
	if idx := strings.IndexAny(s, " \n\r\t"); idx > 0 {
		s = s[:idx]
	}
	switch {
	case s == "rollback" || s == "revert" || s == "undo":
		return "rollback"
	case s == "restart":
		return "restart"
	case s == "ack" || s == "acknowledged":
		return "ack"
	case s == "resolve" || s == "resolved":
		return "resolve"
	case strings.HasPrefix(s, "scale:"):
		return s
	default:
		return "ignore"
	}
}
