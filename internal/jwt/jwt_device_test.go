package jwt

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClaims_UserOnly_NoDid(t *testing.T) {
	secret := "test-secret-device"
	claims := Claims{UserID: 99, Username: "bob", Role: "user"}
	tok, err := Sign(secret, claims, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts: %d", len(parts))
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, hasDid := payload["did"]; hasDid {
		t.Fatalf("user-only token payload should not contain did, got %v", payload)
	}
	parsed, err := Parse(secret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.DeviceID != "" {
		t.Fatalf("expected empty DeviceID for user-only token, got %q", parsed.DeviceID)
	}
}

func TestClaims_Device_RoundTrip(t *testing.T) {
	secret := "test-secret-device"
	did := DeviceID("my-device_123")
	claims := Claims{UserID: 42, Username: "alice", Role: "admin", DeviceID: did}
	tok, err := Sign(secret, claims, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parts := strings.Split(tok, ".")
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var payload map[string]any
	_ = json.Unmarshal(payloadBytes, &payload)
	if payload["did"] != string(did) {
		t.Fatalf("expected did %q in payload, got %v", did, payload["did"])
	}
	parsed, err := Parse(secret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.DeviceID != did {
		t.Fatalf("device id mismatch: got %q want %q", parsed.DeviceID, did)
	}
	if parsed.UserID != 42 || parsed.Username != "alice" || parsed.Role != "admin" {
		t.Fatalf("claims mismatch: %+v", parsed)
	}
}

func TestClaims_ExistingToken_NoDid_Parseable(t *testing.T) {
	secret := "test-secret-device"
	// Simulate old token without did: sign with old Claims shape (no did field)
	oldClaims := Claims{UserID: 7, Username: "legacy", Role: "user"}
	tok, err := Sign(secret, oldClaims, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parsed, err := Parse(secret, tok)
	if err != nil {
		t.Fatalf("parse legacy token: %v", err)
	}
	if parsed.DeviceID != "" {
		t.Fatalf("legacy token should have empty DeviceID, got %q", parsed.DeviceID)
	}
}
