package vault

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Detection patterns for secrets pasted into chat (Section 20.1). The
// reception runs DetectChatSecrets BEFORE persisting a message: the
// stored message keeps only markers, the value moves to an encrypted
// draft, and the user is asked whether to keep it under a name.
var chatSecretPatterns = []*regexp.Regexp{
	// Provider and service tokens.
	regexp.MustCompile(`(?i)\b(ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{10,}|glpat-[A-Za-z0-9\-_]{10,})\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\b(sk-ant-[A-Za-z0-9\-_]{10,}|sk-[A-Za-z0-9]{16,})\b`),
	regexp.MustCompile(`\bxai-[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{20,}\b`),
	regexp.MustCompile(`(?i)\bxox[baprs]-[A-Za-z0-9\-]{8,}\b`),
	regexp.MustCompile(`(?i)\b(sk_live_[A-Za-z0-9]{10,}|rk_live_[A-Za-z0-9]{10,})\b`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
	// Labeled secrets: password:, contraseña:, api_key=..., token: ...
	regexp.MustCompile(`(?im)^[^:\n]{0,40}?\b(passw(or)?d|contrase[ñn]a|api[-_ ]?key|api[-_ ]?token|secret[-_ ]?key|access[-_ ]?token|auth[-_ ]?token|client[-_ ]?secret)\b\s*[:=]\s*(\S.{0,200})`),
}

// Finding is one detected secret span in a chat message.
type Finding struct {
	// Value is the raw detected secret (zero after drafting).
	Value string
	// Start and End are byte offsets in the original message.
	Start, End int
	// Kind names the matching pattern family.
	Kind string
	// Label is the field label for labeled secrets ("password", ...).
	Label string
}

// DetectChatSecrets scans text for secret formats. It never persists:
// callers use SanitizeMessage and SaveDraft.
func DetectChatSecrets(text string) []Finding {
	var out []Finding
	for i, re := range chatSecretPatterns {
		kind := fmt.Sprintf("pattern-%d", i)
		for _, loc := range re.FindAllStringSubmatchIndex(text, -1) {
			value := text[loc[0]:loc[1]]
			start, end := loc[0], loc[1]
			label := ""
			// Labeled-secret patterns capture the value in group 3.
			if len(loc) >= 8 && loc[6] >= 0 {
				value = strings.TrimSpace(text[loc[6]:loc[7]])
				value = strings.Trim(value, `"'`)
				start, end = loc[6], loc[7]
				if len(loc) >= 4 && loc[2] >= 0 {
					label = strings.ToLower(text[loc[2]:loc[3]])
				}
			}
			if strings.TrimSpace(value) == "" {
				continue
			}
			out = append(out, Finding{Value: value, Start: start, End: end, Kind: kind, Label: label})
		}
	}
	return out
}

// SanitizeMessage returns text with every finding replaced by a ‹secret:…›
// marker, plus the findings. Persist the returned text, never the input.
func SanitizeMessage(text string) (string, []Finding) {
	findings := DetectChatSecrets(text)
	if len(findings) == 0 {
		return text, nil
	}
	marks := make([]match, 0, len(findings))
	for _, f := range findings {
		label := "pattern"
		if f.Label != "" {
			label = f.Label
		}
		marks = append(marks, match{start: f.Start, end: f.End, marker: RedactSecretPrefix + "chat-" + label + "›"})
	}
	// Reuse the redactor's deterministic overlap sweep.
	r := NewRedactor()
	_ = r
	var b strings.Builder
	b.Grow(len(text))
	// Sort: earlier start first, longer first.
	for i := 0; i < len(marks); i++ {
		for j := i + 1; j < len(marks); j++ {
			if marks[j].start < marks[i].start ||
				(marks[j].start == marks[i].start && (marks[j].end-marks[j].start) > (marks[i].end-marks[i].start)) {
				marks[i], marks[j] = marks[j], marks[i]
			}
		}
	}
	pos := 0
	for _, h := range marks {
		if h.start < pos || h.start > len(text) || h.end > len(text) {
			continue
		}
		b.WriteString(text[pos:h.start])
		b.WriteString(h.marker)
		pos = h.end
	}
	b.WriteString(text[pos:])
	return b.String(), findings
}

// DraftInfo describes an encrypted chat-paste candidate awaiting the
// user's keep/discard answer.
type DraftInfo struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Hint      string `json:"hint"`
}

// SaveDraft encrypts one detected value into the drafts table (or the
// in-memory pending set while locked) and returns its id. The draft
// holds no name or type yet: ConfirmDraft assigns them.
func (v *Vault) SaveDraft(ctx context.Context, value []byte, hint string) (string, error) {
	if len(value) == 0 {
		return "", fmt.Errorf("vault: empty draft value")
	}
	v.mu.Lock()
	locked := v.dek == nil
	v.mu.Unlock()
	if locked {
		id := "drf_" + hexID()
		cp := append([]byte(nil), value...)
		v.mu.Lock()
		v.pendingDrafts[id] = cp
		v.mu.Unlock()
		return id, nil
	}
	return v.saveDraft(ctx, value, hint)
}

func (v *Vault) saveDraft(ctx context.Context, value []byte, hint string) (string, error) {
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return "", ErrLocked
	}
	dek := *v.dek
	dekVer := v.dekVersion
	v.mu.Unlock()

	id := "drf_" + hexID()
	nonce, sealed, err := sealEntry(dek, id, "draft", "note", 1, value)
	if err != nil {
		return "", err
	}
	if hint == "" {
		hint = "chat-paste"
	}
	err = v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO drafts(id,created_at,ciphertext,nonce,dek_version,hint) VALUES(?,?,?,?,?,?)`,
			id, utcNow(), sealed, nonce, dekVer, hint)
		return err
	})
	if err != nil {
		return "", err
	}
	v.Touch()
	return id, nil
}

// ListDrafts returns pending candidates (encrypted rows plus locked-time
// in-memory ones) without values.
func (v *Vault) ListDrafts(ctx context.Context) ([]DraftInfo, error) {
	var out []DraftInfo
	v.mu.Lock()
	for id := range v.pendingDrafts {
		out = append(out, DraftInfo{ID: id, Hint: "chat-paste (locked)"})
	}
	v.mu.Unlock()
	err := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,created_at,hint FROM drafts ORDER BY created_at`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var d DraftInfo
			if err := rows.Scan(&d.ID, &d.CreatedAt, &d.Hint); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []DraftInfo{}
	}
	return out, nil
}

// ConfirmDraft stores a draft as a named entry and discards the draft.
// If the user does not confirm, DiscardDraft removes it.
func (v *Vault) ConfirmDraft(ctx context.Context, draftID string, p SetParams) (Metadata, error) {
	value, err := v.takeDraft(ctx, draftID)
	if err != nil {
		return Metadata{}, err
	}
	defer ZeroBytes(value)
	p.Value = value
	return v.Set(ctx, p)
}

// DiscardDraft drops a candidate without storing it.
func (v *Vault) DiscardDraft(ctx context.Context, draftID string) error {
	v.mu.Lock()
	if _, ok := v.pendingDrafts[draftID]; ok {
		zero(v.pendingDrafts[draftID])
		delete(v.pendingDrafts, draftID)
		v.mu.Unlock()
		return nil
	}
	v.mu.Unlock()
	return v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM drafts WHERE id=?`, draftID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrDraftNotFound
		}
		return nil
	})
}

// takeDraft decrypts and deletes one draft, returning its value.
func (v *Vault) takeDraft(ctx context.Context, draftID string) ([]byte, error) {
	v.mu.Lock()
	if cp, ok := v.pendingDrafts[draftID]; ok {
		delete(v.pendingDrafts, draftID)
		v.mu.Unlock()
		v.mu.Lock()
		locked := v.dek == nil
		v.mu.Unlock()
		if locked {
			return nil, ErrLocked
		}
		return cp, nil
	}
	if v.dek == nil {
		v.mu.Unlock()
		return nil, ErrLocked
	}
	dek := *v.dek
	v.mu.Unlock()

	var sealed, nonce []byte
	var dv int
	err := v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		rows, err := ex.Query(ctx, `SELECT ciphertext,nonce,dek_version FROM drafts WHERE id=?`, draftID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return ErrDraftNotFound
		}
		if err := rows.Scan(&sealed, &nonce, &dv); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		_ = rows.Close()
		_, err = ex.Exec(ctx, `DELETE FROM drafts WHERE id=?`, draftID)
		return err
	})
	if err != nil {
		return nil, err
	}
	dd := dek
	if dv != v.currentDEKVersion() {
		var krow keysRow
		rerr := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
			var rerr error
			krow, rerr = v.readKeysRowQ(ctx, q, dv)
			return rerr
		})
		if rerr != nil {
			return nil, rerr
		}
		kek, kerr := v.kekForDecrypt(ctx, krow)
		if kerr != nil {
			return nil, kerr
		}
		dk, kerr := unwrapDEK(kek, dv, krow.wrapNonce, krow.wrapped)
		zero(kek)
		if kerr != nil {
			return nil, kerr
		}
		dd = dk
		zero(dk[:])
	}
	return openEntry(dd, draftID, "draft", "note", 1, nonce, sealed)
}
