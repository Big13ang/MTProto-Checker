package checker

import "testing"

func TestTcpCheckLocalhost(t *testing.T) {
	if err := TCPCheck("localhost", 3000); err == nil {
		t.Log("port 3000 open (server may be running)")
	} else {
		t.Logf("port 3000 closed (%v)", err)
	}
}
