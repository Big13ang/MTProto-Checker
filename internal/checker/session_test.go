package checker

import "testing"

func TestNewCheckOptionsSharedSession(t *testing.T) {
	a := newCheckOptions(nil)
	b := newCheckOptions(nil)
	if a.SessionStorage == nil || b.SessionStorage == nil {
		t.Fatal("newCheckOptions returned nil SessionStorage")
	}
	if a.SessionStorage != b.SessionStorage {
		t.Error("SessionStorage is per-check; sharing is load-bearing: the first " +
			"successful check's auth key is reused so later checks skip the DH " +
			"exchange. Measured: per-check sessions took detection from 99/1022 " +
			"to 0/1022 and repeated key creation throttles the source IP — see " +
			"the load-bearing rule in CLAUDE.md before changing this")
	}
}
