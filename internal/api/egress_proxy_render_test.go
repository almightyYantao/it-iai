package api

import (
	"os"
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
