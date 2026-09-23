package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadCheckRequests(t *testing.T) {
	entry := `{"server":"a","port":1,"secret":"ee"}`

	var tooMany strings.Builder
	tooMany.WriteString("[")
	for i := 0; i <= maxBatchSize; i++ {
		if i > 0 {
			tooMany.WriteString(",")
		}
		tooMany.WriteString(entry)
	}
	tooMany.WriteString("]")

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantLen    int
	}{
		{"ok", "[" + entry + "]", 0, 1},
		{"invalid json", "{", http.StatusBadRequest, 0},
		{"count over maxBatchSize", tooMany.String(), http.StatusRequestEntityTooLarge, 0},
		{"body over maxBodySize", strings.Repeat(" ", maxBodySize+1024) + "[]", http.StatusRequestEntityTooLarge, 0},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, "/check-batch", strings.NewReader(tt.body))
		reqs, status, msg := readCheckRequests(httptest.NewRecorder(), req)
		if status != tt.wantStatus {
			t.Errorf("%s: status = %d (%q), want %d", tt.name, status, msg, tt.wantStatus)
			continue
		}
		if status == 0 && len(reqs) != tt.wantLen {
			t.Errorf("%s: len(reqs) = %d, want %d", tt.name, len(reqs), tt.wantLen)
		}
		if status != 0 && msg == "" {
			t.Errorf("%s: non-zero status with empty message", tt.name)
		}
	}
}
