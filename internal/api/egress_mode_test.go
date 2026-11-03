package api

import "testing"

func TestParseProxyLog(t *testing.T) {
	raw := "1.0 13128 TCP_TUNNEL/200 CONNECT open.larksuite.com:443\n" +
		"1.1 13128 TCP_DENIED/403 GET http://evil.example.com/x\n" +
		"1.2 3128 NONE_NONE/000 - -\n"
	got := parseProxyLog(raw)
	if len(got) != 2 || got[0].host != "open.larksuite.com" || got[1].host != "evil.example.com" || got[1].port != 13128 {
		t.Fatalf("got %+v", got)
	}
}
