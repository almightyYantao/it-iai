package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Slack alerting for the ops dashboard. Webhook stored KEK-encrypted in
// system_config; messages use legacy attachments (colour bar + fields).

const (
	cfgSlackWebhook  = "alert.slack_webhook_enc"
	cfgSlackEnabled  = "alert.slack_enabled"
	cfgDashboardURL  = "alert.dashboard_url"
	alertRemindEvery = 30 * time.Minute
)

var slackAAD = []byte("system_config/alert.slack_webhook")
var digitsRe = regexp.MustCompile(`[0-9.]+%?`)

func alertKey(a opsAlert) string { return a.Target + "|" + digitsRe.ReplaceAllString(a.Text, "#") }

type alertSettings struct {
	Webhook   string
	Enabled   bool
	Dashboard string
}

func (s *Server) loadAlertSettings(ctx context.Context) alertSettings {
	cfg, _ := s.store.GetSystemConfig(ctx)
	st := alertSettings{Enabled: cfg[cfgSlackEnabled] == "true", Dashboard: cfg[cfgDashboardURL]}
	if enc := cfg[cfgSlackWebhook]; enc != "" && s.kek != nil {
		if ct, err := base64.StdEncoding.DecodeString(enc); err == nil {
			if pt, err := s.kek.Decrypt(ct, slackAAD); err == nil {
				st.Webhook = string(pt)
			}
		}
	}
	return st
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}
type slackAttachment struct {
	Color     string       `json:"color"`
	Fallback  string       `json:"fallback"`
	Pretext   string       `json:"pretext,omitempty"`
	Title     string       `json:"title"`
	TitleLink string       `json:"title_link,omitempty"`
	Text      string       `json:"text,omitempty"`
	Fields    []slackField `json:"fields"`
	Footer    string       `json:"footer"`
	TS        int64        `json:"ts"`
}

const (
	colorCrit     = "#E5484D"
	colorWarn     = "#F5A524"
	colorResolved = "#30A46C"
)

func alertAttachment(kind string, a opsAlert, first time.Time, dash string) slackAttachment {
	color, level, head := colorWarn, "警告", "🟠 告警"
	if a.Level == "crit" {
		color, level, head = colorCrit, "严重", "🔴 告警"
	}
	switch kind {
	case "remind":
		head = "⏰ 仍未恢复"
	case "resolved":
		color, head = colorResolved, "✅ 已恢复"
	}
	link := dash
	if a.Link != "" && dash != "" {
		link = strings.TrimSuffix(strings.TrimSuffix(dash, "/"), "/ops") + a.Link
	}
	dur := time.Since(first).Round(time.Minute)
	f := []slackField{
		{"级别", level, true},
		{"对象", a.Target, true},
		{"首次出现", first.In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04"), true},
		{"持续", dur.String(), true},
	}
	return slackAttachment{
		Color: color, Fallback: fmt.Sprintf("[%s] %s %s", head, a.Target, a.Text),
		Title: fmt.Sprintf("%s · %s %s", head, a.Target, a.Text), TitleLink: link,
		Fields: f, Footer: "iai 运维监控", TS: time.Now().Unix(),
	}
}

func postSlack(ctx context.Context, webhook string, atts []slackAttachment) error {
	for len(atts) > 0 {
		n := len(atts)
		if n > 20 {
			n = 20
		}
		b, _ := json.Marshal(map[string]any{"attachments": atts[:n]})
		ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx2, http.MethodPost, webhook, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("slack returned HTTP %d", resp.StatusCode)
		}
		atts = atts[n:]
	}
	return nil
}

// RunAlertNotifier evaluates the ops snapshot and pushes new / reminder /
// resolved alerts to Slack. State lives in ops_alert_state so a restart
// neither re-sends everything nor forgets what's open.
func (s *Server) RunAlertNotifier(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.notifyOnce(ctx); err != nil {
			log.Printf("alert notifier: %v", err)
		}
	}
}

func (s *Server) notifyOnce(ctx context.Context) error {
	st := s.loadAlertSettings(ctx)
	_, alerts := s.opsSnapshot(ctx)
	cur := map[string]opsAlert{}
	for _, a := range alerts {
		cur[alertKey(a)] = a
	}
	type open struct {
		a     opsAlert
		first time.Time
		sent  time.Time
	}
	prev := map[string]open{}
	rows, err := s.store.Pool.Query(ctx, `SELECT key, level, target, text, first_seen, last_sent FROM ops_alert_state`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var o open
		if rows.Scan(&k, &o.a.Level, &o.a.Target, &o.a.Text, &o.first, &o.sent) == nil {
			prev[k] = o
		}
	}
	rows.Close()

	var atts []slackAttachment
	for k, a := range cur {
		p, seen := prev[k]
		switch {
		case !seen:
			atts = append(atts, alertAttachment("new", a, time.Now(), st.Dashboard))
			_, _ = s.store.Pool.Exec(ctx, `INSERT INTO ops_alert_state (key, level, target, text) VALUES ($1,$2,$3,$4)
				ON CONFLICT (key) DO NOTHING`, k, a.Level, a.Target, a.Text)
		case time.Since(p.sent) >= alertRemindEvery:
			atts = append(atts, alertAttachment("remind", a, p.first, st.Dashboard))
			_, _ = s.store.Pool.Exec(ctx, `UPDATE ops_alert_state SET last_sent = now(), text = $2, level = $3 WHERE key = $1`, k, a.Text, a.Level)
		}
	}
	for k, p := range prev {
		if _, still := cur[k]; !still {
			atts = append(atts, alertAttachment("resolved", p.a, p.first, st.Dashboard))
			_, _ = s.store.Pool.Exec(ctx, `DELETE FROM ops_alert_state WHERE key = $1`, k)
		}
	}
	if len(atts) == 0 || !st.Enabled || st.Webhook == "" {
		return nil
	}
	return postSlack(ctx, st.Webhook, atts)
}

// GET /v1/admin/alerts/settings
func (s *Server) handleGetAlertSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	st := s.loadAlertSettings(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"configured": st.Webhook != "", "enabled": st.Enabled, "dashboard_url": st.Dashboard})
}

// PUT /v1/admin/alerts/settings {"webhook_url"?: "...", "enabled": bool, "dashboard_url": "..."}
// webhook_url omitted = keep; "" = clear.
func (s *Server) handlePutAlertSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	var body struct {
		WebhookURL   *string `json:"webhook_url"`
		Enabled      bool    `json:"enabled"`
		DashboardURL string  `json:"dashboard_url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	kv := map[string]string{cfgSlackEnabled: fmt.Sprint(body.Enabled), cfgDashboardURL: strings.TrimSpace(body.DashboardURL)}
	if body.WebhookURL != nil {
		u := strings.TrimSpace(*body.WebhookURL)
		if u == "" {
			kv[cfgSlackWebhook] = ""
		} else {
			if !strings.HasPrefix(u, "https://hooks.slack.com/") {
				writeError(w, http.StatusBadRequest, "bad_webhook", "expected a Slack incoming webhook (https://hooks.slack.com/...)")
				return
			}
			if s.kek == nil {
				writeError(w, http.StatusInternalServerError, "no_kek", "KEK not configured")
				return
			}
			ct, err := s.kek.Encrypt([]byte(u), slackAAD)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "encrypt", err.Error())
				return
			}
			kv[cfgSlackWebhook] = base64.StdEncoding.EncodeToString(ct)
		}
	}
	if err := s.store.BulkSetSystemConfig(r.Context(), kv, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(), "alerts.settings.update", nil,
		map[string]any{"enabled": body.Enabled, "webhook_changed": body.WebhookURL != nil})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /v1/admin/alerts/test
func (s *Server) handleTestAlert(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	st := s.loadAlertSettings(r.Context())
	if st.Webhook == "" {
		writeError(w, http.StatusBadRequest, "not_configured", errors.New("no Slack webhook saved").Error())
		return
	}
	a := opsAlert{Level: "warn", Target: "iai", Text: "这是一条测试告警"}
	att := alertAttachment("new", a, time.Now(), st.Dashboard)
	att.Pretext = "测试消息，由 " + actor.identityString() + " 发送"
	if err := postSlack(r.Context(), st.Webhook, []slackAttachment{att}); err != nil {
		writeError(w, http.StatusBadGateway, "slack", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
