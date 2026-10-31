package vault

import (
	"bytes"
	"testing"
)

// TestCrypto_AADBinding covers 20.2 row 2: a ciphertext copied to
// another row fails to decrypt because the AAD binds id, name, type
// and version.
func TestCrypto_AADBinding(t *testing.T) {
	dek, err := newDEK()
	if err != nil {
		t.Fatal(err)
	}
	val := []byte("super-secret-value-001")
	nonce, sealed, err := sealEntry(dek, "sec_aaa", "github-token", TypeAPIKey, 1, val)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openEntry(dek, "sec_aaa", "github-token", TypeAPIKey, 1, nonce, sealed); err != nil {
		t.Fatalf("own row must decrypt: %v", err)
	}
	for _, tc := range []struct {
		name, id, entry, typ string
		ver                  int
	}{
		{"other id", "sec_bbb", "github-token", TypeAPIKey, 1},
		{"other name", "sec_aaa", "other-name", TypeAPIKey, 1},
		{"other type", "sec_aaa", "github-token", TypeNote, 1},
		{"other version", "sec_aaa", "github-token", TypeAPIKey, 2},
	} {
		if _, err := openEntry(dek, tc.id, tc.entry, tc.typ, tc.ver, nonce, sealed); err == nil {
			t.Fatalf("copied ciphertext decrypted for %s", tc.name)
		}
	}
	other, _ := newDEK()
	if _, err := openEntry(other, "sec_aaa", "github-token", TypeAPIKey, 1, nonce, sealed); err == nil {
		t.Fatal("wrong DEK decrypted the entry")
	}
}

// TestCrypto_WrapRoundtrip covers DEK wrap/unwrap and wrong-KEK failure.
func TestCrypto_WrapRoundtrip(t *testing.T) {
	kek := bytes.Repeat([]byte{7}, 32)
	dek, _ := newDEK()
	nonce, wrapped, err := wrapDEK(kek, 1, dek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := unwrapDEK(kek, 1, nonce, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if got != dek {
		t.Fatal("DEK roundtrip mismatch")
	}
	if _, err := unwrapDEK(bytes.Repeat([]byte{8}, 32), 1, nonce, wrapped); err == nil {
		t.Fatal("wrong KEK unwrapped the DEK")
	}
	if _, err := unwrapDEK(kek, 2, nonce, wrapped); err == nil {
		t.Fatal("wrong DEK version unwrapped the DEK")
	}
}

// TestCrypto_Argon2id checks KDF determinism and salt separation.
func TestCrypto_Argon2id(t *testing.T) {
	s1, _ := newSalt()
	s2, _ := newSalt()
	k1, err := deriveKEK([]byte("correct horse"), s1[:])
	if err != nil {
		t.Fatal(err)
	}
	defer zero(k1)
	k2, err := deriveKEK([]byte("correct horse"), s1[:])
	if err != nil {
		t.Fatal(err)
	}
	defer zero(k2)
	if !bytes.Equal(k1, k2) {
		t.Fatal("Argon2id not deterministic")
	}
	k3, err := deriveKEK([]byte("correct horse"), s2[:])
	if err != nil {
		t.Fatal(err)
	}
	defer zero(k3)
	if bytes.Equal(k1, k3) {
		t.Fatal("different salts derived the same KEK")
	}
	if _, err := deriveKEK(nil, s1[:]); err == nil {
		t.Fatal("empty passphrase must fail")
	}
}
