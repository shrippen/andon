package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	Init("test-master-key")
	blob, err := Encrypt("hunter2", PurposeCredential, nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := Decrypt(blob, PurposeCredential)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("want hunter2, got %q", got)
	}
}

func TestDecryptWrongPurposeFails(t *testing.T) {
	Init("test-master-key")
	blob, _ := Encrypt("secret", PurposeCredential, nil)
	if _, err := Decrypt(blob, PurposeTOTP); err == nil {
		t.Fatal("expected decrypt under wrong purpose to fail")
	}
}

func TestPasswordHashRoundtrip(t *testing.T) {
	hash, err := HashPassword("s3cret!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !CheckPassword(hash, "s3cret!") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestCheckPasswordEmptyStored(t *testing.T) {
	if CheckPassword("", "anything") {
		t.Fatal("empty stored hash must never verify")
	}
}

func TestNewTokenUnique(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b {
		t.Fatal("tokens must be random")
	}
	if TokenHash(a) == TokenHash(b) {
		t.Fatal("hashes must differ for different tokens")
	}
}

// TestMaskToken: a masked token differs on every call (compressed pages
// must not repeat it, BREACH) and unmasks to the token; tampering fails.
func TestMaskToken(t *testing.T) {
	const token = "csrf-token-value"
	a, b := MaskToken(token), MaskToken(token)
	if a == b || strings.Contains(a, token) {
		t.Fatalf("masks repeat or leak: %q %q", a, b)
	}
	for _, masked := range []string{a, b} {
		if got, ok := UnmaskToken(masked); !ok || got != token {
			t.Fatalf("unmask %q: %q %v", masked, got, ok)
		}
	}
	if _, ok := UnmaskToken(a[:len(a)-2]); ok {
		t.Fatal("truncated token unmasked")
	}
	if _, ok := UnmaskToken("not base64 !"); ok {
		t.Fatal("garbage unmasked")
	}
}
