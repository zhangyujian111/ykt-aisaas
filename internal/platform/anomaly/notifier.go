package anomaly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Notifier 通知器接口。
type Notifier interface {
	Notify(ctx context.Context, event Event, channels []string) error
}

// NotifierConfig 通知器配置。
type NotifierConfig struct {
	SlackWebhookURL string   `mapstructure:"slackWebhookUrl"`
	WebhookURLs     []string `mapstructure:"webhookUrls"`
	EmailSMTPHost   string   `mapstructure:"emailSmtpHost"`
	EmailSMTPPort   int      `mapstructure:"emailSmtpPort"`
	EmailFrom       string   `mapstructure:"emailFrom"`
	EmailTo         []string `mapstructure:"emailTo"`
}

// MultiNotifier 多渠道通知器。
type MultiNotifier struct {
	cfg    NotifierConfig
	client *http.Client
}

// NewMultiNotifier 创建多渠道通知器。
func NewMultiNotifier(cfg NotifierConfig) *MultiNotifier {
	return &MultiNotifier{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Notify 按渠道发送通知。
func (n *MultiNotifier) Notify(ctx context.Context, event Event, channels []string) error {
	for _, ch := range channels {
		switch ch {
		case "slack":
			if err := n.sendSlack(ctx, event); err != nil {
				slog.Warn("slack notify failed", "ruleId", event.RuleID, "err", err)
			}
		case "webhook":
			if err := n.sendWebhook(ctx, event); err != nil {
				slog.Warn("webhook notify failed", "ruleId", event.RuleID, "err", err)
			}
		case "email":
			if err := n.sendEmail(ctx, event); err != nil {
				slog.Warn("email notify failed", "ruleId", event.RuleID, "err", err)
			}
		default:
			slog.Warn("unknown notification channel", "channel", ch, "ruleId", event.RuleID)
		}
	}
	return nil
}

// slackMessage Slack 消息体。
type slackMessage struct {
	Text     string            `json:"text"`
	Blocks   []slackBlock      `json:"blocks,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type slackBlock struct {
	Type string `json:"type"`
	Text *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"text,omitempty"`
	Fields []*struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"fields,omitempty"`
}

// sendSlack 发送 Slack 通知。
func (n *MultiNotifier) sendSlack(ctx context.Context, event Event) error {
	if n.cfg.SlackWebhookURL == "" {
		slog.Debug("slack webhook not configured, skip")
		return nil
	}

	emoji := ":warning:"
	switch event.Severity {
	case SeverityCritical:
		emoji = ":rotating_light:"
	case SeverityInfo:
		emoji = ":information_source:"
	}

	msg := slackMessage{
		Text: fmt.Sprintf("%s AI Anomaly Alert: %s", emoji, event.Message),
	}

	body, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.SlackWebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack responded with %d", resp.StatusCode)
	}

	slog.Info("slack notification sent", "ruleId", event.RuleID, "severity", event.Severity)
	return nil
}

// webhookPayload Webhook 消息体。
type webhookPayload struct {
	RuleID      string  `json:"ruleId"`
	TenantID    string  `json:"tenantId"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	MetricValue float64 `json:"metricValue"`
	Timestamp   string  `json:"timestamp"`
}

// sendWebhook 发送 Webhook 通知。
func (n *MultiNotifier) sendWebhook(ctx context.Context, event Event) error {
	if len(n.cfg.WebhookURLs) == 0 {
		slog.Debug("webhook not configured, skip")
		return nil
	}

	payload := webhookPayload{
		RuleID:      event.RuleID,
		TenantID:    event.TenantID,
		Severity:    string(event.Severity),
		Message:     event.Message,
		MetricValue: event.MetricValue,
		Timestamp:   event.TriggeredAt.Format(time.RFC3339),
	}

	body, _ := json.Marshal(payload)

	for _, url := range n.cfg.WebhookURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			slog.Warn("webhook request create failed", "url", url, "err", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := n.client.Do(req)
		if err != nil {
			slog.Warn("webhook send failed", "url", url, "err", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			slog.Warn("webhook responded with error", "url", url, "status", resp.StatusCode)
		}
	}

	slog.Info("webhook notifications sent", "ruleId", event.RuleID, "urls", len(n.cfg.WebhookURLs))
	return nil
}

// sendEmail 发送邮件通知（stub 实现）。
// 生产环境建议对接 AWS SES / SendGrid / 企业邮件服务。
func (n *MultiNotifier) sendEmail(ctx context.Context, event Event) error {
	if n.cfg.EmailSMTPHost == "" {
		slog.Debug("email not configured, skip")
		return nil
	}

	subject := fmt.Sprintf("[%s] AI Anomaly Alert: %s", event.Severity, event.RuleID)
	body := fmt.Sprintf(
		"AI Anomaly Detected\n\n"+
			"Rule: %s\n"+
			"Severity: %s\n"+
			"Metric Value: %.2f\n"+
			"Message: %s\n"+
			"Time: %s\n",
		event.RuleID, event.Severity, event.MetricValue, event.Message,
		event.TriggeredAt.Format(time.RFC3339),
	)

	// stub: log the email content; production should use net/smtp or SES
	slog.Info("email notification (stub)",
		"subject", subject,
		"to", n.cfg.EmailTo,
		"body", body,
	)
	return nil
}