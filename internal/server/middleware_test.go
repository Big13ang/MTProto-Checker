package server

import "testing"

func TestHostAllowlist(t *testing.T) {
	tests := []struct {
		addr string
		want []string
	}{
		{"127.0.0.1:3000", []string{"127.0.0.1", "localhost", "::1"}},
		{"localhost:8080", []string{"127.0.0.1", "localhost", "::1"}},
		{"[::1]:3000", []string{"127.0.0.1", "localhost", "::1"}},
		{"127.0.0.2:3000", []string{"127.0.0.2", "127.0.0.1", "localhost", "::1"}},
		{"0.0.0.0:3000", nil},
		{"192.168.1.5:3000", nil},
		{"[::]:3000", nil},
		{"garbage", nil},
	}

	for _, tt := range tests {
		got := HostAllowlist(tt.addr)

		if len(got) != len(tt.want) {
			t.Errorf("HostAllowlist(%q) has %d entries, want %d (%v)",
				tt.addr, len(got), len(tt.want), got)
			continue
		}
		for _, host := range tt.want {
			if _, ok := got[host]; !ok {
				t.Errorf("HostAllowlist(%q) is missing %q (got %v)", tt.addr, host, got)
			}
		}
	}
}

func TestHostWithoutPort(t *testing.T) {
	tests := []struct{ host, want string }{
		{"127.0.0.1:3000", "127.0.0.1"},
		{"127.0.0.1", "127.0.0.1"},
		{"localhost:8080", "localhost"},
		{"localhost", "localhost"},
		{"[::1]:3000", "::1"},
		{"[::1]", "::1"},
		{"::1", "::1"},
		{"evil.test:3000", "evil.test"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := hostWithoutPort(tt.host); got != tt.want {
			t.Errorf("hostWithoutPort(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}
