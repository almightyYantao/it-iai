package api

import (
	"net/http"
	"strings"
)

// externalBaseURL returns the scheme://host URL the client used to reach us.
//
// Use this — not s.cfg.PublicBaseURL — whenever you put a URL inside a response
// body, redirect, or webhook payload. cfg.PublicBaseURL is a startup-time guess
// (often from `ip route get 1.1.1.1` on the platform node) and it is wrong by
// default for any host on a private subnet behind a public IP.
//
// Precedence:
//  1. X-Forwarded-Host  + X-Forwarded-Proto  — reverse proxy in front (nginx,
//     Traefik, ALB, ...). Most reliable signal of "what the client typed".
//  2. r.Host            + scheme from r.TLS  — direct hit, no proxy.
//  3. cfg.PublicBaseURL                       — last resort, used only when
//     neither header is populated (e.g. system-generated URLs without a
//     request context).
func (s *Server) externalBaseURL(r *http.Request) string {
	if r != nil {
		if fwHost := firstHeader(r, "X-Forwarded-Host"); fwHost != "" {
			scheme := firstHeader(r, "X-Forwarded-Proto")
			if scheme == "" {
				scheme = "http"
			}
			return scheme + "://" + fwHost
		}
		if r.Host != "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			return scheme + "://" + r.Host
		}
	}
	return strings.TrimRight(s.cfg.PublicBaseURL, "/")
}

// firstHeader returns the first comma-separated value of header `name`,
// trimmed. Proxies often prepend their own value, so the leftmost entry is
// the original client request.
func firstHeader(r *http.Request, name string) string {
	v := r.Header.Get(name)
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
