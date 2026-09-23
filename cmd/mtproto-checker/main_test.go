package main

import "testing"

func TestResolveAddr(t *testing.T) {
	tests := []struct {
		host, port, want string
	}{
		{"", "", "127.0.0.1:3000"},      // loopback by default
		{"", "8080", "127.0.0.1:8080"},  // PORT honored
		{"0.0.0.0", "", "0.0.0.0:3000"}, // explicit opt-in to all interfaces
		{"192.168.1.5", "8080", "192.168.1.5:8080"},
		{"::1", "", "[::1]:3000"},     // IPv6 host bracketed
		{"", "abc", "127.0.0.1:3000"}, // lenient PORT parsing preserved:
		{"", "80abc", "127.0.0.1:80"}, // Sscanf error ignored, partial parse kept
	}
	for _, tt := range tests {
		if got := resolveAddr(tt.host, tt.port); got != tt.want {
			t.Errorf("resolveAddr(%q, %q) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}
