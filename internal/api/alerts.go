package api

import (
	"net/url"

	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/iai/vibedeploy/internal/k8sdriver"
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
	cfgSlackBot      = "alert.slack_bot_enc"
	alertRemindEvery = 30 * time.Minute
)

var slackAAD = []byte("system_config/alert.slack_webhook")
var slackBotAAD = []byte("system_config/alert.slack_bot")
var digitsRe = regexp.MustCompile(`[0-9.]+%?`)

func alertKey(a opsAlert) string { return a.Target + "|" + digitsRe.ReplaceAllString(a.Text, "#") }

type alertSettings struct {
	BotToken  string
	Webhook   string
	Enabled   bool
	Dashboard string
}

func (s *Server) loadAlertSettings(ctx context.Context) alertSettings {
	cfg, _ := s.store.GetSystemConfig(ctx)
	st := alertSettings{Enabled: cfg[cfgSlackEnabled] == "true", Dashboard: cfg[cfgDashboardURL]}
	if enc := cfg[cfgSlackBot]; enc != "" && s.kek != nil {
		if ct, err := base64.StdEncoding.DecodeString(enc); err == nil {
			if pt, err := s.kek.Decrypt(ct, slackBotAAD); err == nil {
				st.BotToken = string(pt)
			}
		}
	}
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
		if err := s.notifyCrashes(ctx); err != nil {
			log.Printf("crash notifier: %v", err)
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
	writeJSON(w, http.StatusOK, map[string]any{"configured": st.Webhook != "", "bot_configured": st.BotToken != "", "enabled": st.Enabled, "dashboard_url": st.Dashboard})
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
		BotToken     *string `json:"bot_token"`
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
	if body.BotToken != nil {
		t := strings.TrimSpace(*body.BotToken)
		if t == "" {
			kv[cfgSlackBot] = ""
		} else {
			if !strings.HasPrefix(t, "xoxb-") {
				writeError(w, http.StatusBadRequest, "bad_bot_token", "expected a Slack bot token (xoxb-...)")
				return
			}
			if s.kek == nil {
				writeError(w, http.StatusInternalServerError, "no_kek", "KEK not configured")
				return
			}
			ct, err := s.kek.Encrypt([]byte(t), slackBotAAD)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "encrypt", err.Error())
				return
			}
			kv[cfgSlackBot] = base64.StdEncoding.EncodeToString(ct)
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

// ---- Slack bot (DMs to project owners) --------------------------------------

func slackAPI(ctx context.Context, token, method string, body any) (map[string]any, error) {
	b, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/"+method, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if ok, _ := out["ok"].(bool); !ok {
		return out, fmt.Errorf("slack %s: %v", method, out["error"])
	}
	return out, nil
}

// dmByEmail sends attachments to the Slack user with that email.
func dmByEmail(ctx context.Context, token, email string, atts []slackAttachment) error {
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx2, http.MethodGet, "https://slack.com/api/users.lookupByEmail?email="+url.QueryEscape(email), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	var u struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&u)
	resp.Body.Close()
	if !u.OK {
		return fmt.Errorf("lookup %s: %s", email, u.Error)
	}
	_, err = slackAPI(ctx, token, "chat.postMessage", map[string]any{"channel": u.User.ID, "attachments": atts, "text": atts[0].Fallback})
	return err
}

// ---- Crash alerts ---------------------------------------------------------------

// notifyCrashes posts one attachment per new container crash to the channel
// and DMs the project owner when a bot token is configured. crash_events
// dedupes, so each termination is announced once even across restarts.
func (s *Server) notifyCrashes(ctx context.Context) error {
	if s.deployer == nil {
		return nil
	}
	crashes, err := s.deployer.RecentCrashes(ctx, time.Now().Add(-15*time.Minute))
	if err != nil {
		return err
	}
	st := s.loadAlertSettings(ctx)
	for _, c := range crashes {
		tag, err := s.store.Pool.Exec(ctx, `INSERT INTO crash_events (pod_uid, container, finished_at, slug, reason, exit_code)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, c.PodUID, c.Container, c.FinishedAt, c.Slug, c.Reason, c.ExitCode)
		if err != nil || tag.RowsAffected() == 0 || !st.Enabled {
			continue
		}
		var owner string
		_ = s.store.Pool.QueryRow(ctx, `SELECT coalesce(u.email,'') FROM projects p LEFT JOIN users u ON u.id = p.owner_id
			WHERE p.slug = $1 AND p.deleted_at IS NULL`, c.Slug).Scan(&owner)
		att := crashAttachment(c, owner, st.Dashboard, s.deployer.PreviousLogTail(ctx, c.Slug, c.Pod, c.Container, 15))
		if st.Webhook != "" {
			if err := postSlack(ctx, st.Webhook, []slackAttachment{att}); err != nil {
				log.Printf("crash alert: %v", err)
			}
		}
		if st.BotToken != "" && owner != "" {
			if err := dmByEmail(ctx, st.BotToken, owner, []slackAttachment{att}); err != nil {
				log.Printf("crash alert DM: %v", err)
			}
		}
	}
	_, _ = s.store.Pool.Exec(ctx, `DELETE FROM crash_events WHERE created_at < now() - interval '30 days'`)
	return nil
}

func crashHint(reason string, code int32) string {
	switch {
	case reason == "OOMKilled":
		return "内存超出上限被杀：调高内存上限，或排查内存泄漏"
	case code == 137:
		return "被 SIGKILL 结束（常见于内存不足或健康检查失败）"
	case code == 143:
		return "收到 SIGTERM 退出"
	case code == 1:
		return "应用异常退出，见下方日志"
	}
	return ""
}

func crashAttachment(c k8sdriver.Crash, owner, dash, logTail string) slackAttachment {
	link := ""
	if dash != "" {
		link = strings.TrimSuffix(strings.TrimSuffix(dash, "/"), "/ops") + "/projects/" + c.Slug
	}
	fields := []slackField{
		{"项目", c.Slug, true},
		{"负责人", owner, true},
		{"原因", fmt.Sprintf("%s（退出码 %d）", c.Reason, c.ExitCode), true},
		{"累计重启", fmt.Sprint(c.Restarts), true},
		{"Pod", c.Pod + " / " + c.Container, false},
	}
	if h := crashHint(c.Reason, c.ExitCode); h != "" {
		fields = append(fields, slackField{"提示", h, false})
	}
	text := ""
	if logTail = strings.TrimSpace(logTail); logTail != "" {
		text = "```" + logTail + "```"
	}
	return slackAttachment{
		Color: colorCrit, Fallback: fmt.Sprintf("[容器崩溃] %s %s", c.Slug, c.Reason),
		Title: "💥 容器崩溃 · " + c.Slug, TitleLink: link, Text: text,
		Fields: fields, Footer: "iai 运维监控", TS: c.FinishedAt.Unix(),
	}
}
