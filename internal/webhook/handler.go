// Package webhook provides the HTTP server that receives incoming WhatsApp
// messages forwarded by Twilio, verifies their authenticity and dispatches
// the appropriate Kubernetes remediation actions.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubewav1alpha1 "github.com/takumi-software/kubewa-operator/api/v1alpha1"
	"github.com/takumi-software/kubewa-operator/internal/actions"
	aiclient "github.com/takumi-software/kubewa-operator/internal/ai"
	twiliointernal "github.com/takumi-software/kubewa-operator/internal/twilio"
)

const (
	// maxBodySize caps incoming webhook payloads to prevent DoS.
	maxBodySize = 1 << 16 // 64 KB

	// rateLimitWindow is the per-phone window for rate limiting.
	rateLimitWindow = time.Minute
	// rateLimitMax is the maximum messages per phone per window.
	rateLimitMax = 10
)

// TwilioMessage represents the parsed POST body from Twilio.
type TwilioMessage struct {
	From           string
	Body           string
	MessageSID     string
	IncidentRef    string // extracted from our custom token param
	IncidentNS     string // extracted from our custom token param
	NumMedia       string
	ProfileName    string
}

// Handler handles incoming Twilio WhatsApp webhooks.
type Handler struct {
	k8sClient    client.Client
	twilioClient *twiliointernal.Client
	aiClient     *aiclient.Client
	executor     *actions.Executor
	log          logr.Logger
	webhookURL   string   // full public URL of this endpoint (for signature verification)
	allowedPhones map[string]struct{} // whitelist; empty means allow all

	// simple in-memory rate limiter: phone -> (count, windowStart)
	rateLimiter map[string]*rateEntry
}

type rateEntry struct {
	count       int
	windowStart time.Time
}

// NewHandler constructs a webhook Handler.
// allowedPhones is an optional whitelist of E.164 numbers (without "whatsapp:" prefix).
// Pass nil or empty slice to allow any phone.
func NewHandler(
	k8sClient client.Client,
	twilioClient *twiliointernal.Client,
	aiClient *aiclient.Client,
	executor *actions.Executor,
	log logr.Logger,
	webhookURL string,
	allowedPhones []string,
) *Handler {
	phones := make(map[string]struct{}, len(allowedPhones))
	for _, p := range allowedPhones {
		phones[strings.TrimPrefix(p, "whatsapp:")] = struct{}{}
	}
	return &Handler{
		k8sClient:     k8sClient,
		twilioClient:  twilioClient,
		aiClient:      aiClient,
		executor:      executor,
		log:           log,
		webhookURL:    webhookURL,
		allowedPhones: phones,
		rateLimiter:   make(map[string]*rateEntry),
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := r.ParseForm(); err != nil {
		h.log.Error(err, "parse form")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// 1. Verify Twilio signature.
	sig := r.Header.Get("X-Twilio-Signature")
	if err := h.twilioClient.ValidateSignature(h.webhookURL, sig, r.PostForm); err != nil {
		h.log.Info("invalid Twilio signature", "remoteAddr", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	msg := parseTwilioForm(r)

	// 2. Whitelist check.
	cleanPhone := strings.TrimPrefix(msg.From, "whatsapp:")
	if len(h.allowedPhones) > 0 {
		if _, ok := h.allowedPhones[cleanPhone]; !ok {
			h.log.Info("phone not in whitelist", "from", msg.From)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// 3. Rate limiting.
	if !h.checkRateLimit(cleanPhone) {
		h.log.Info("rate limit exceeded", "from", msg.From)
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	// 4. Dispatch in background so we can return 200 quickly (Twilio expects fast response).
	go func() {
		if err := h.dispatch(context.Background(), msg); err != nil {
			h.log.Error(err, "dispatch message", "from", msg.From, "body", msg.Body)
		}
	}()

	// Twilio expects a 200 with optional TwiML body.
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`)
}

// dispatch processes a validated incoming WhatsApp message and applies the
// corresponding Kubernetes action.
func (h *Handler) dispatch(ctx context.Context, msg TwilioMessage) error {
	log := h.log.WithValues("from", msg.From, "incidentRef", msg.IncidentRef, "incidentNS", msg.IncidentNS)

	if msg.IncidentRef == "" {
		log.Info("no incident ref in message, ignoring")
		return nil
	}

	// Fetch the Incident CR.
	incident := &kubewav1alpha1.Incident{}
	key := types.NamespacedName{Name: msg.IncidentRef, Namespace: msg.IncidentNS}
	if err := h.k8sClient.Get(ctx, key, incident); err != nil {
		return fmt.Errorf("get incident %s/%s: %w", msg.IncidentNS, msg.IncidentRef, err)
	}

	// Only process messages from the notified phone.
	cleanFrom := strings.TrimPrefix(msg.From, "whatsapp:")
	if incident.Status.NotifiedPhone != "" &&
		cleanFrom != strings.TrimPrefix(incident.Status.NotifiedPhone, "whatsapp:") {
		log.Info("message from unexpected phone, ignoring", "expected", incident.Status.NotifiedPhone)
		return nil
	}

	body := strings.TrimSpace(msg.Body)
	log.Info("processing message", "body", body, "state", incident.Status.State)

	// Parse command: try numeric first, then AI-assisted NL parsing.
	action, err := h.resolveAction(ctx, body, incident)
	if err != nil {
		return err
	}

	return h.applyAction(ctx, action, msg, incident)
}

// resolveAction converts a WhatsApp reply into an action string.
func (h *Handler) resolveAction(ctx context.Context, body string, incident *kubewav1alpha1.Incident) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(body))

	// Numeric reply (1, 2, 3) maps to button index.
	if len(lower) == 1 && lower[0] >= '1' && lower[0] <= '9' {
		idx := int(lower[0]-'0') - 1
		if idx < len(incident.Spec.RemediationActions) {
			return incident.Spec.RemediationActions[idx].Name, nil
		}
	}

	// Simple keyword matching.
	switch lower {
	case "ack", "acknowledged", "ok", "visto", "entendido":
		return "ack", nil
	case "resolve", "resolved", "resolvido", "solucionado":
		return "resolve", nil
	case "rollback", "revert", "undo", "voltar":
		return "rollback", nil
	case "restart", "reiniciar", "reinicia":
		return "restart", nil
	}

	// Fall back to AI parsing.
	if h.aiClient != nil {
		return h.aiClient.ParseCommand(ctx, body, incident.Spec.Title)
	}
	return "ignore", nil
}

// applyAction executes the parsed action and updates the incident status.
func (h *Handler) applyAction(ctx context.Context, action string, msg TwilioMessage, incident *kubewav1alpha1.Incident) error {
	log := h.log.WithValues("action", action, "incident", incident.Name)

	now := metav1.NewTime(time.Now())
	incidentCopy := incident.DeepCopy()

	var result string
	var execErr error

	switch {
	case action == "ack":
		if incidentCopy.Status.State == kubewav1alpha1.IncidentOpen ||
			incidentCopy.Status.State == kubewav1alpha1.IncidentEscalatedToBackup {
			incidentCopy.Status.State = kubewav1alpha1.IncidentAcked
			incidentCopy.Status.AckedBy = msg.From
			incidentCopy.Status.AckedAt = &now
			result = "acknowledged"
		}

	case action == "resolve":
		incidentCopy.Status.State = kubewav1alpha1.IncidentResolved
		incidentCopy.Status.ResolvedAt = &now
		incidentCopy.Status.ResolutionAction = "manual resolve via WhatsApp"
		result = "resolved manually"

	case action == "ignore":
		log.Info("ignoring unrecognised message")
		return nil

	default:
		// Look up the action in RemediationActions by name.
		var found *kubewav1alpha1.RemediationActionSpec
		for i := range incidentCopy.Spec.RemediationActions {
			if strings.EqualFold(incidentCopy.Spec.RemediationActions[i].Name, action) {
				found = &incidentCopy.Spec.RemediationActions[i]
				break
			}
		}

		// Also handle "rollback" and "restart" as aliases for the first matching action type.
		if found == nil {
			found = findActionByTypeAlias(action, incidentCopy.Spec.RemediationActions)
		}

		if found != nil {
			ns := incident.Namespace
			result, execErr = h.executor.Execute(ctx, *found, ns)
		} else {
			log.Info("no matching action found", "action", action)
			return nil
		}
	}

	// Record audit entry.
	entry := kubewav1alpha1.AuditEntry{
		Timestamp:  now,
		Actor:      msg.From,
		Action:     action,
		MessageSID: msg.MessageSID,
		Result:     result,
	}
	if execErr != nil {
		entry.Result = fmt.Sprintf("error: %v", execErr)
	}
	incidentCopy.Status.AuditTrail = append(incidentCopy.Status.AuditTrail, entry)

	// Persist status update.
	if err := h.k8sClient.Status().Update(ctx, incidentCopy); err != nil {
		return fmt.Errorf("update incident status: %w", err)
	}

	// Send confirmation WhatsApp message.
	var replyBody string
	if execErr != nil {
		replyBody = fmt.Sprintf("❌ Action *%s* failed:\n%v\n\nIncident: %s", action, execErr, incident.Spec.Title)
	} else {
		replyBody = fmt.Sprintf("✅ Action *%s* executed:\n%s\n\nIncident: %s", action, result, incident.Spec.Title)
	}

	cleanPhone := strings.TrimPrefix(msg.From, "whatsapp:")
	if _, err := h.twilioClient.SendMessage(cleanPhone, replyBody); err != nil {
		log.Error(err, "send confirmation message")
	}

	return execErr
}

// checkRateLimit returns true if the phone is within the allowed rate.
func (h *Handler) checkRateLimit(phone string) bool {
	now := time.Now()
	entry, ok := h.rateLimiter[phone]
	if !ok || now.Sub(entry.windowStart) > rateLimitWindow {
		h.rateLimiter[phone] = &rateEntry{count: 1, windowStart: now}
		return true
	}
	entry.count++
	return entry.count <= rateLimitMax
}

// parseTwilioForm extracts fields from a Twilio POST form.
func parseTwilioForm(r *http.Request) TwilioMessage {
	msg := TwilioMessage{
		From:        r.FormValue("From"),
		Body:        r.FormValue("Body"),
		MessageSID:  r.FormValue("MessageSid"),
		NumMedia:    r.FormValue("NumMedia"),
		ProfileName: r.FormValue("ProfileName"),
	}
	// Custom parameters we embed in the webhook URL query string or message body:
	// ?incident=<name>&ns=<namespace>
	msg.IncidentRef = r.URL.Query().Get("incident")
	msg.IncidentNS = r.URL.Query().Get("ns")
	return msg
}

// findActionByTypeAlias looks for the first action whose type matches a simple alias.
func findActionByTypeAlias(alias string, acts []kubewav1alpha1.RemediationActionSpec) *kubewav1alpha1.RemediationActionSpec {
	for i := range acts {
		a := &acts[i]
		switch strings.ToLower(alias) {
		case "rollback":
			if a.Type == kubewav1alpha1.ActionRollbackDeployment || a.Type == kubewav1alpha1.ActionRollbackStatefulSet {
				return a
			}
		case "restart":
			if a.Type == kubewav1alpha1.ActionRestartDeployment || a.Type == kubewav1alpha1.ActionRestartStatefulSet {
				return a
			}
		}
	}
	return nil
}

// MarshalJSON for TwilioMessage (used in logging).
func (m TwilioMessage) MarshalJSON() ([]byte, error) {
	type alias struct {
		From        string `json:"from"`
		Body        string `json:"body"`
		MessageSID  string `json:"messageSID"`
		IncidentRef string `json:"incidentRef"`
		IncidentNS  string `json:"incidentNS"`
	}
	// Mask phone number for privacy.
	from := m.From
	if len(from) > 4 {
		from = from[:4] + strings.Repeat("*", len(from)-4)
	}
	return json.Marshal(alias{
		From:        from,
		Body:        m.Body,
		MessageSID:  m.MessageSID,
		IncidentRef: m.IncidentRef,
		IncidentNS:  m.IncidentNS,
	})
}
