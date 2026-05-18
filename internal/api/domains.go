package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/iai/vibedeploy/internal/store"
)

// Custom-domain API:
//
//   GET    /v1/projects/:slug/domains
//   POST   /v1/projects/:slug/domains       body: { hostname: "..." }
//   DELETE /v1/projects/:slug/domains/:hostname
//
// TLS is M3 — for now the deployed Ingress is HTTP-only and we mark the
// domain unverified. Users have to point their DNS at the platform IP
// independently.

var domainHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

func validateHostname(h string) error {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" || len(h) > 253 {
		return errors.New("hostname must be 1–253 chars")
	}
	if !domainHostRe.MatchString(h) {
		return errors.New("not a valid hostname (use a fully-qualified domain like app.example.com)")
	}
	return nil
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	list, err := s.store.ListDomains(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	// Always include the platform-issued default subdomain at the top.
	writeJSON(w, http.StatusOK, map[string]any{
		"default": map[string]any{
			"hostname": p.Slug + "." + s.cfg.AppBaseDomain,
			"kind":     "subdomain",
			"verified": true,
		},
		"custom": list,
	})
}

type addDomainRequest struct {
	Hostname string `json:"hostname"`
}

func (s *Server) handleAddDomain(w http.ResponseWriter, r *http.Request) {
	var req addDomainRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	host := strings.ToLower(strings.TrimSpace(req.Hostname))
	if err := validateHostname(host); err != nil {
		writeError(w, http.StatusBadRequest, "bad_hostname", err.Error())
		return
	}
	p := projectFrom(r.Context())
	// Hostnames under the platform's wildcard domain (*.example.com) get
	// special handling — they don't need TLS / DNS work from the user, but
	// they have two ways to go wrong:
	//   1. The project's OWN default subdomain (<slug>.<base>) is already served
	//      automatically; adding it again would create a duplicate ingress rule.
	//   2. Deeper subdomains (foo.bar.<base>) aren't covered by the wildcard
	//      cert (`*.<base>` matches one level only), so the user would get a
	//      browser cert error. We reject up front to save the debugging trip.
	// Anything that's a clean one-level vanity subdomain is allowed.
	if strings.HasSuffix(host, "."+s.cfg.AppBaseDomain) {
		prefix := strings.TrimSuffix(host, "."+s.cfg.AppBaseDomain)
		if prefix == p.Slug {
			writeError(w, http.StatusBadRequest, "reserved",
				"this is the project's default subdomain — already served automatically")
			return
		}
		if strings.Contains(prefix, ".") {
			writeError(w, http.StatusBadRequest, "bad_hostname",
				"only one-level subdomains under "+s.cfg.AppBaseDomain+
					" are supported (wildcard TLS covers a single level)")
			return
		}
	}
	d, err := s.store.AddDomain(r.Context(), p.ID, host)
	if err != nil {
		if errors.Is(err, store.ErrDomainTaken) {
			writeError(w, http.StatusConflict, "hostname_taken",
				"this hostname is already bound to another project")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	if s.deployer != nil {
		if err := s.syncIngressForProject(r.Context(), p); err != nil {
			// non-fatal — domain is recorded; ingress sync error is surfaced for diagnosis
			s.store.WriteAudit(r.Context(), "system", "ingress-sync", "ingress.sync.failed", &p.ID, map[string]string{"error": err.Error()})
		}
	}

	actor, _ := ActorFrom(r.Context())
	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(), "domain.add", &p.ID, map[string]string{"hostname": host})
	writeJSON(w, http.StatusCreated, map[string]any{"domain": d})
}

func (s *Server) handleRemoveDomain(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	host := strings.ToLower(strings.TrimSpace(r.URL.Path))
	// Path is /v1/projects/:slug/domains/:hostname — chi captures :hostname for us.
	// chi.URLParam(r, "hostname") would be cleaner but we don't have it imported here:
	// re-extract from the URL path.
	idx := strings.LastIndex(host, "/")
	if idx >= 0 {
		host = host[idx+1:]
	}
	if err := validateHostname(host); err != nil {
		writeError(w, http.StatusBadRequest, "bad_hostname", err.Error())
		return
	}
	if err := s.store.RemoveDomain(r.Context(), p.ID, host); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "domain not on this project")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if s.deployer != nil {
		_ = s.syncIngressForProject(r.Context(), p)
	}
	actor, _ := ActorFrom(r.Context())
	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(), "domain.remove", &p.ID, map[string]string{"hostname": host})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
