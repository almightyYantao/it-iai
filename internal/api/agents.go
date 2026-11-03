package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iai/vibedeploy/internal/auth"
)

// Agent onboarding (option A): the admin gets a one-time, 30-minute install
// command; the control-plane never holds SSH credentials for any node.

var nodeNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

const agentJoinTTL = 30 * time.Minute

func (s *Server) agentInstallBase() string {
	b := strings.TrimSpace(s.cfg.AgentInstallBaseURL)
	if b == "" {
		b = s.cfg.PublicBaseURL
	}
	return strings.TrimRight(b, "/")
}

// POST /v1/admin/agents/join  {"node_name":"it-iai-k3s-agent-03"}
func (s *Server) handleCreateAgentJoin(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	if s.cfg.K3sJoinToken == "" || s.cfg.K3sServerURL == "" {
		writeError(w, http.StatusPreconditionFailed, "not_configured",
			"agent onboarding is not configured (CP_K3S_SERVER_URL / CP_K3S_JOIN_TOKEN)")
		return
	}
	var body struct {
		NodeName string `json:"node_name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	name := strings.ToLower(strings.TrimSpace(body.NodeName))
	if !nodeNameRe.MatchString(name) {
		writeError(w, http.StatusBadRequest, "bad_name", "node name: lowercase letters, digits and -, max 63")
		return
	}
	ctx := r.Context()
	if s.deployer != nil {
		if exists, _, err := s.deployer.NodeExists(ctx, name); err == nil && exists {
			writeError(w, http.StatusConflict, "exists", "a node with this name is already in the cluster")
			return
		}
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "rand", err.Error())
		return
	}
	tok := base64.RawURLEncoding.EncodeToString(raw[:])
	exp := time.Now().Add(agentJoinTTL)
	if _, err := s.store.Pool.Exec(ctx,
		`INSERT INTO agent_join_tokens (node_name, token_hash, created_by, expires_at) VALUES ($1,$2,$3,$4)`,
		name, auth.HashToken(tok), actor.identityString(), exp); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(), "agent.join.create", nil, map[string]any{"node": name})
	cmd := fmt.Sprintf("curl -fsSL %s/v1/agent-join/%s/install.sh | sudo sh", s.agentInstallBase(), tok)
	writeJSON(w, http.StatusCreated, map[string]any{"node_name": name, "command": cmd, "expires_at": exp})
}

// GET /v1/admin/agents/join — recent onboarding attempts and whether the node joined.
func (s *Server) handleListAgentJoins(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	ctx := r.Context()
	rows, err := s.store.Pool.Query(ctx, `SELECT id, node_name, created_by, created_at, expires_at, used_at, coalesce(used_from,'')
		FROM agent_join_tokens WHERE created_at > now() - interval '7 days' ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	type row struct {
		ID        string     `json:"id"`
		NodeName  string     `json:"node_name"`
		CreatedBy string     `json:"created_by"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt time.Time  `json:"expires_at"`
		UsedAt    *time.Time `json:"used_at,omitempty"`
		UsedFrom  string     `json:"used_from,omitempty"`
		State     string     `json:"state"` // pending | expired | installing | joined | ready
	}
	out := []row{}
	for rows.Next() {
		var x row
		if rows.Scan(&x.ID, &x.NodeName, &x.CreatedBy, &x.CreatedAt, &x.ExpiresAt, &x.UsedAt, &x.UsedFrom) != nil {
			continue
		}
		switch {
		case x.UsedAt == nil && time.Now().After(x.ExpiresAt):
			x.State = "expired"
		case x.UsedAt == nil:
			x.State = "pending"
		default:
			x.State = "installing"
			if s.deployer != nil {
				if exists, ready, err := s.deployer.NodeExists(ctx, x.NodeName); err == nil && exists {
					x.State = "joined"
					if ready {
						x.State = "ready"
					}
				}
			}
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"joins": out, "configured": s.cfg.K3sJoinToken != "" && s.cfg.K3sServerURL != ""})
}

func shQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

// GET /v1/agent-join/{token}/install.sh — public; the token is the credential.
// Served exactly once, then dead.
func (s *Server) handleAgentInstallScript(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	var name string
	err := s.store.Pool.QueryRow(r.Context(), `UPDATE agent_join_tokens SET used_at = now(), used_from = $2
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING node_name`,
		auth.HashToken(tok), ip).Scan(&name)
	if err != nil {
		http.Error(w, "echo 'iai: this install link is invalid, expired or already used' >&2; exit 1\n", http.StatusGone)
		return
	}
	s.store.WriteAudit(r.Context(), "system", "agent-join", "agent.join.used", nil, map[string]any{"node": name, "from": ip})
	ver := s.cfg.K3sVersion
	if ver == "" && s.deployer != nil {
		if nodes, err := s.deployer.OpsNodes(r.Context()); err == nil {
			for _, n := range nodes {
				if n.Role == "control-plane" {
					ver = n.Kubelet
				}
			}
		}
	}
	w.Header().Set("Content-Type", "text/x-shellscript")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `#!/bin/sh
# iai agent installer for %[1]s — single use, generated by the control-plane.
set -eu
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
echo "==> joining %[1]s to %[2]s (k3s %[3]s)"
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=%[4]s K3S_URL=%[5]s K3S_TOKEN=%[6]s sh -s - agent --node-name %[7]s
echo "==> done; the node appears in the iai ops dashboard within a minute"
`, name, s.cfg.K3sServerURL, ver, shQuote(ver), shQuote(s.cfg.K3sServerURL), shQuote(s.cfg.K3sJoinToken), shQuote(name))
}

// POST /v1/admin/nodes/{name}/{action}  action = cordon|uncordon|drain|delete
func (s *Server) handleNodeAction(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	if s.deployer == nil {
		writeError(w, http.StatusServiceUnavailable, "no_cluster", "k8s driver disabled")
		return
	}
	ctx := r.Context()
	name, action := chi.URLParam(r, "name"), chi.URLParam(r, "action")
	var err error
	res := map[string]any{"ok": true}
	switch action {
	case "cordon":
		err = s.deployer.SetNodeSchedulable(ctx, name, false)
	case "uncordon":
		err = s.deployer.SetNodeSchedulable(ctx, name, true)
	case "drain":
		var n int
		n, err = s.deployer.DrainNode(ctx, name)
		res["evicted"] = n
	case "delete":
		err = s.deployer.DeleteNode(ctx, name)
	default:
		writeError(w, http.StatusBadRequest, "bad_action", "action must be cordon, uncordon, drain or delete")
		return
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(), "node."+action, nil,
		map[string]any{"node": name, "error": fmt.Sprint(err)})
	if err != nil {
		writeError(w, http.StatusBadRequest, "node_action", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
