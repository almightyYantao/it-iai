package api

import "testing"

func TestAlertKeyStableAcrossValues(t *testing.T) {
	a := opsAlert{Target: "srv", Text: "内存 86%"}
	b := opsAlert{Target: "srv", Text: "内存 91%"}
	if alertKey(a) != alertKey(b) {
		t.Fatal("changing value must not open a new alert")
	}
	if alertKey(a) == alertKey(opsAlert{Target: "srv", Text: "磁盘剩余 9%"}) {
		t.Fatal("different problems must not collide")
	}
}

func TestShQuote(t *testing.T) {
	if got := shQuote("a'b"); got != `'a'\''b'` {
		t.Fatal(got)
	}
}
