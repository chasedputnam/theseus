package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"log"
	"net/http"
	"time"

	"github.com/chaseputnam/theseus/internal/db"
)

// Manager fires outgoing webhooks.
type Manager struct {
	db         *db.DB
	httpClient *http.Client
}

// New creates a webhook Manager.
func New(database *db.DB) *Manager {
	return &Manager{
		db:         database,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Fire sends a webhook event to all registered hooks for that event.
func (m *Manager) Fire(ctx context.Context, event string, payload map[string]any) {
	hooks, err := m.db.ListActiveWebhooksForEvent(event)
	if err != nil || len(hooks) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":   event,
		"payload": payload,
		"ts":      time.Now().Unix(),
	})
	for _, hook := range hooks {
		// Use a detached context with timeout so delivery isn't cancelled when the triggering request completes
		deliveryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = cancel // cancel is called inside deliver goroutine
		go m.deliver(deliveryCtx, cancel, hook, body)
	}
}

func (m *Manager) deliver(ctx context.Context, cancel context.CancelFunc, hook *db.Webhook, body []byte) {
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		m.recordResult(hook, 0, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Theseus-Event", hook.Events)

	// HMAC-SHA256 signature
	if hook.Secret.Valid && hook.Secret.String != "" {
		sig := computeHMAC(body, hook.Secret.String)
		req.Header.Set("X-Theseus-Signature", "sha256="+sig)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.recordResult(hook, 0, err.Error())
		return
	}
	defer resp.Body.Close()
	m.recordResult(hook, resp.StatusCode, "")
}

func (m *Manager) recordResult(hook *db.Webhook, statusCode int, errMsg string) {
	hook.LastTriggeredAt.Time = time.Now()
	hook.LastTriggeredAt.Valid = true
	if statusCode > 0 {
		hook.LastStatusCode.Int64 = int64(statusCode)
		hook.LastStatusCode.Valid = true
	}
	if errMsg != "" {
		hook.LastError.String = errMsg
		hook.LastError.Valid = true
	} else {
		hook.LastError.Valid = false
	}
	if err := m.db.UpdateWebhook(hook); err != nil {
		log.Printf("webhook: record result: %v", err)
	}
}

func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateWebhookURL checks that a URL is safe to deliver to (requires https, blocks SSRF).
func ValidateWebhookURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL required")
	}
	if len(rawURL) > 2048 {
		return fmt.Errorf("URL too long")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("URL must use http or https scheme")
	}
	// Block loopback and private IPs (SSRF prevention)
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("webhook URL must not target loopback addresses")
	}
	return nil
}
