package vault

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
)

// Character sets for Generate.
const (
	Letters  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Digits   = "0123456789"
	Symbols  = "!@#$%^&*()-_=+[]{}<>?"
	URLSafe  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	Password = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}<>?"
)

// Generate returns a random secret of length n from charset, using
// crypto/rand. The agent tool `secrets generate` stores the result
// directly: the value never passes through the model (Section 20.1).
func Generate(n int, charset string) ([]byte, error) {
	if n <= 0 || n > 4096 {
		return nil, fmt.Errorf("vault: bad length %d (1..4096)", n)
	}
	if charset == "" {
		charset = Password
	}
	// Deduplicate the charset for uniform selection.
	seen := map[rune]bool{}
	var chars []rune
	for _, r := range charset {
		if !seen[r] {
			seen[r] = true
			chars = append(chars, r)
		}
	}
	if len(chars) < 2 {
		return nil, fmt.Errorf("vault: charset too small")
	}
	out := make([]rune, n)
	max := big.NewInt(int64(len(chars)))
	for i := range out {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return nil, fmt.Errorf("vault: random: %w", err)
		}
		out[i] = chars[k.Int64()]
	}
	return []byte(string(out)), nil
}

// GenerateParams carries a generate request.
type GenerateParams struct {
	Name         string
	Type         string
	Length       int
	Charset      string
	Meta         map[string]string
	Scopes       []string
	AllowedTools []string
	Domains      []string
}

// GenerateAndStore creates a random value and stores it as name in one
// step. Only metadata returns; the value never passes through the
// model or the tool result.
func (v *Vault) GenerateAndStore(ctx context.Context, p GenerateParams) (Metadata, error) {
	length := p.Length
	if length == 0 {
		length = 32
	}
	val, err := Generate(length, p.Charset)
	if err != nil {
		return Metadata{}, err
	}
	defer ZeroBytes(val)
	return v.Set(ctx, SetParams{
		Name:         p.Name,
		Type:         p.Type,
		Value:        val,
		Meta:         p.Meta,
		Scopes:       p.Scopes,
		AllowedTools: p.AllowedTools,
		Domains:      p.Domains,
	})
}
