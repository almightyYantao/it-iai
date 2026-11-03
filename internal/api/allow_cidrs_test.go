package api

import "testing"

func TestHasWorldCIDR(t *testing.T) {
	cases := map[string]bool{"0.0.0.0/0": true, "::/0": true, " 0.0.0.0/0 ": true, "10.0.0.0/8": false, "1.2.3.4/32": false, "junk": false}
	for c, want := range cases {
		if got := hasWorldCIDR([]string{c}); got != want {
			t.Errorf("hasWorldCIDR(%q) = %v, want %v", c, got, want)
		}
	}
}
