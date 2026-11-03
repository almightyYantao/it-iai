package api

import (
	"os"
	"strings"
	"testing"

	"github.com/iai/vibedeploy/internal/store"
)

// TestRenderSquidConfDump writes the rendered config to /tmp so it can be fed to
// `squid -k parse`. Not an assertion test — the real check is that squid accepts
// the output, which no amount of Go-side string matching proves.
func TestRenderSquidConfDump(t *testing.T) {
	out := renderSquidConf([]store.EgressProxyTarget{
		{Slug: "trigrams-wvuc", Port: 13128, Domains: []store.DomainRule{
			{Value: ".anthropic.com", Port: 443},
			{Value: "open.feishu.cn", Port: 443},
			{Value: "old.example.com", Port: 80},
		}},
		{Slug: "no-rules-proj", Port: 13129},
	})
	if err := os.WriteFile("/tmp/squid-rendered.conf", []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d bytes", len(out))
}

func TestRenderSquidAnyPort(t *testing.T) {
	conf := renderSquidConf([]store.EgressProxyTarget{{Slug: "a-b", Port: 13128, Mode: "enforce",
		Domains: []store.DomainRule{{Value: "db.example.com", Port: 0}, {Value: "api.example.com", Port: 443}}}})
	allow := strings.Index(conf, "http_access allow proj_a_b_anyport proj_a_b_any")
	deny := strings.Index(conf, "http_access deny CONNECT !SSL_ports")
	if allow < 0 || deny < 0 || allow > deny {
		t.Fatalf("any-port allow must precede the CONNECT deny:\n%s", conf)
	}
	if strings.Contains(conf, "proj_a_b_https dstdomain db.example.com") {
		t.Fatal("any-port domain leaked into the 443 list")
	}
}
