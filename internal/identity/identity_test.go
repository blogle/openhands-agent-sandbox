package identity

import (
	"regexp"
	"testing"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestRuntimeID_Format(t *testing.T) {
	id, err := RuntimeID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uuidRe.MatchString(id) {
		t.Errorf("RuntimeID %q does not match UUID v4 format", id)
	}
}

func TestRuntimeID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := RuntimeID()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate RuntimeID: %s", id)
		}
		seen[id] = true
	}
}

func TestSessionAPIKey_Length(t *testing.T) {
	key, err := SessionAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 64 {
		t.Errorf("expected 64-char hex key, got %d chars: %s", len(key), key)
	}
}

func TestRuntimeSecretKey_Length(t *testing.T) {
	key, err := RuntimeSecretKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 64 {
		t.Errorf("expected 64-char hex key, got %d chars", len(key))
	}
}

func TestClaimName_Prefix(t *testing.T) {
	name := ClaimName("550e8400-e29b-41d4-a716-446655440000")
	if len(name) > 23 {
		t.Errorf("claim name too long: %d chars: %s", len(name), name)
	}
	if name[:3] != "oh-" {
		t.Errorf("expected 'oh-' prefix, got %q", name)
	}
}

func TestClaimName_Deterministic(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	a := ClaimName(id)
	b := ClaimName(id)
	if a != b {
		t.Errorf("ClaimName not deterministic: %q != %q", a, b)
	}
}

func TestSessionHash_Deterministic(t *testing.T) {
	h1 := SessionHash("test-session-id")
	h2 := SessionHash("test-session-id")
	if h1 != h2 {
		t.Errorf("SessionHash not deterministic: %q != %q", h1, h2)
	}
}

func TestSessionHash_DifferentInputs(t *testing.T) {
	h1 := SessionHash("session-a")
	h2 := SessionHash("session-b")
	if h1 == h2 {
		t.Errorf("SessionHash should differ for different inputs")
	}
}

func TestSessionHash_Length(t *testing.T) {
	h := SessionHash("any-session")
	if len(h) != 32 {
		t.Errorf("expected 32-char hash, got %d", len(h))
	}
}

func TestConstantTimeCompare_Match(t *testing.T) {
	if !ConstantTimeCompare("secret", "secret") {
		t.Error("expected true for matching strings")
	}
}

func TestConstantTimeCompare_NoMatch(t *testing.T) {
	if ConstantTimeCompare("secret", "other") {
		t.Error("expected false for non-matching strings")
	}
}

func TestConstantTimeCompare_DifferentLength(t *testing.T) {
	if ConstantTimeCompare("short", "longer-string") {
		t.Error("expected false for different-length strings")
	}
}
