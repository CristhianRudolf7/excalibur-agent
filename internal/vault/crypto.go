package vault

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Argon2id parameters for the passphrase-derived master key, from
// Appendix B: 64 MiB memory, 3 iterations, 4 threads, 16-byte salt.
const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // KiB
	argon2Threads = 4
	argon2KeyLen  = 32
	saltLen       = 16
)

// deriveKEK derives a 256-bit key-encryption key from a passphrase and
// salt with Argon2id (Appendix B parameters).
func deriveKEK(passphrase, salt []byte) ([]byte, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("vault: empty passphrase")
	}
	if len(salt) != saltLen {
		return nil, fmt.Errorf("vault: salt must be %d bytes", saltLen)
	}
	return argon2.IDKey(passphrase, salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen), nil
}

// newSalt returns fresh random salt.
func newSalt() ([saltLen]byte, error) {
	var s [saltLen]byte
	if _, err := rand.Read(s[:]); err != nil {
		return s, fmt.Errorf("vault: salt: %w", err)
	}
	return s, nil
}

// newDEK returns a fresh random 256-bit data-encryption key.
func newDEK() ([32]byte, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return k, fmt.Errorf("vault: dek: %w", err)
	}
	return k, nil
}

// aeadX returns an XChaCha20-Poly1305 AEAD over key.
func aeadX(key []byte) (aead, error) {
	return chacha20poly1305.NewX(key)
}

// aead is the minimal AEAD surface used below (lets tests inject a
// failing implementation if needed).
type aead interface {
	NonceSize() int
	Overhead() int
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}

// sealEntry encrypts value with the DEK under an AAD binding the
// entry id, name, type and entry version. A ciphertext copied to
// another row fails Open because the AAD no longer matches.
func sealEntry(dek [32]byte, id, name, typ string, version int, value []byte) (nonce, sealed []byte, err error) {
	a, err := aeadX(dek[:])
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("vault: nonce: %w", err)
	}
	sealed = a.Seal(nil, nonce, value, entryAAD(id, name, typ, version))
	return nonce, sealed, nil
}

// openEntry decrypts a sealed entry value.
func openEntry(dek [32]byte, id, name, typ string, version int, nonce, sealed []byte) ([]byte, error) {
	a, err := aeadX(dek[:])
	if err != nil {
		return nil, err
	}
	plain, err := a.Open(nil, nonce, sealed, entryAAD(id, name, typ, version))
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt entry %q: %w", name, err)
	}
	return plain, nil
}

// entryAAD builds the associated data binding a ciphertext to its row.
func entryAAD(id, name, typ string, version int) []byte {
	return []byte("excalibur-vault-entry-v1\x00" + id + "\x00" + name + "\x00" + typ + "\x00" + itoa(version))
}

// wrapDEK wraps the DEK with the master key (KEK). The wrap AAD binds
// the ciphertext to the DEK version so a wrapped key copied across
// versions does not unwrap.
func wrapDEK(kek []byte, dekVersion int, dek [32]byte) (nonce, wrapped []byte, err error) {
	a, err := aeadX(kek)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("vault: wrap nonce: %w", err)
	}
	wrapped = a.Seal(nil, nonce, dek[:], dekAAD(dekVersion))
	return nonce, wrapped, nil
}

// unwrapDEK recovers the DEK with the master key.
func unwrapDEK(kek []byte, dekVersion int, nonce, wrapped []byte) ([32]byte, error) {
	var dek [32]byte
	a, err := aeadX(kek)
	if err != nil {
		return dek, err
	}
	plain, err := a.Open(nil, nonce, wrapped, dekAAD(dekVersion))
	if err != nil {
		return dek, fmt.Errorf("vault: unwrap dek v%d: %w", dekVersion, err)
	}
	copy(dek[:], plain)
	zero(plain)
	return dek, nil
}

// dekAAD binds a wrapped DEK to its version.
func dekAAD(dekVersion int) []byte {
	return []byte("excalibur-vault-dek-v1\x00" + itoa(dekVersion))
}

// zero overwrites b. The Go collector may keep transient copies, so
// this reduces but does not eliminate exposure (Section 20.1).
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ZeroBytes overwrites b; callers use it to clear resolved values and
// passphrases after use.
func ZeroBytes(b []byte) { zero(b) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
