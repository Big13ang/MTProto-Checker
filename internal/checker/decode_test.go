package checker

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestDecodeSecret(t *testing.T) {
	// dd-prefixed 17-byte secret whose base64 forms contain '+' and '/'
	secret, err := hex.DecodeString("dd00fbef3ec8a03bff84f0ff0fefff3f01")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{"hex", "ee", []byte{0xee}},
		{"empty", "", []byte{}},
		{"hex with junk suffix", "ee)", []byte{0xee}},
		{"base64 std padded", base64.StdEncoding.EncodeToString(secret), secret},
		{"base64 std unpadded", base64.RawStdEncoding.EncodeToString(secret), secret},
		{"base64 urlsafe", base64.RawURLEncoding.EncodeToString(secret), secret},
		// trailing '/' and '_' are in the junk-trim set; the raw input must
		// be decoded before trimming or these decode to the wrong bytes
		{"base64 std trailing slash", "AAP/", []byte{0x00, 0x03, 0xff}},
		{"base64 urlsafe trailing underscore", "AAP_", []byte{0x00, 0x03, 0xff}},
	}
	for _, tt := range tests {
		got, err := DecodeSecret(tt.input)
		if err != nil {
			t.Errorf("%s: DecodeSecret(%q) err=%v", tt.name, tt.input, err)
			continue
		}
		if !bytes.Equal(got, tt.want) {
			t.Errorf("%s: DecodeSecret(%q) = %x, want %x", tt.name, tt.input, got, tt.want)
		}
	}

	if _, err := DecodeSecret("eeRigzNJvxrFGRMCIMJdEKuVuRueWVrdGFuZXQuY29tZmFyYXqhdi5jb212YW4ubmFqdmEuY29tAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA)"); err != nil {
		t.Errorf("DecodeSecret with trailing junk: %v", err)
	}
}

func TestDecodeSecretErrors(t *testing.T) {
	_, err := DecodeSecret("!!!invalid!!!")
	if err == nil {
		t.Error("expected error for invalid secret")
	}
}
