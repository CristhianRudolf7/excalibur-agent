// Package googleapi supplies daemon-only tools for the connected Google account.
package googleapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// BlobStore is satisfied by the daemon's gateway blob stores.
type BlobStore interface {
	Put(string, io.Reader) error
	Get(string) (io.ReadCloser, int64, error)
}

// ToolOptions allows HTTP fakes and the daemon's persistent blob store.
// Base URLs include the API prefix, e.g. https://gmail.googleapis.com/gmail/v1.
type ToolOptions struct {
	HTTPClient                                                  *http.Client
	GmailBaseURL, DriveBaseURL, DriveUploadBaseURL, DocsBaseURL string
	Blobs                                                       BlobStore
	// DisableConfirmation is an explicit operator setting, equivalent to
	// EXCALIBUR_GOOGLE_CONFIRM=0. Tool arguments cannot change it.
	DisableConfirmation bool
}

type client struct {
	p    TokenProvider
	opts ToolOptions
}
type googleTool struct {
	name string
	c    *client
}

type attachment struct {
	Digest   string `json:"digest"`
	Name     string `json:"name"`
	MIMEType string `json:"mime_type,omitempty"`
}
type arguments struct {
	Action       string       `json:"action"`
	ID           string       `json:"id,omitempty"`
	Query        string       `json:"query,omitempty"`
	PlainText    bool         `json:"plain_text,omitempty"`
	MaxResults   int          `json:"max_results,omitempty"`
	PageToken    string       `json:"page_token,omitempty"`
	ThreadID     string       `json:"thread_id,omitempty"`
	DraftID      string       `json:"draft_id,omitempty"`
	ReplyToID    string       `json:"reply_to_id,omitempty"`
	To           []string     `json:"to,omitempty"`
	CC           []string     `json:"cc,omitempty"`
	BCC          []string     `json:"bcc,omitempty"`
	Subject      string       `json:"subject,omitempty"`
	Body         string       `json:"body,omitempty"`
	Attachments  []attachment `json:"attachments,omitempty"`
	AddLabels    []string     `json:"add_labels,omitempty"`
	RemoveLabels []string     `json:"remove_labels,omitempty"`
	Read         *bool        `json:"read,omitempty"`
	Starred      *bool        `json:"starred,omitempty"`
	Name         string       `json:"name,omitempty"`
	FolderID     string       `json:"folder_id,omitempty"`
	Format       string       `json:"format,omitempty"`
	Digest       string       `json:"digest,omitempty"`
	MIMEType     string       `json:"mime_type,omitempty"`
	Text         string       `json:"text,omitempty"`
	Role         string       `json:"role,omitempty"`
	Email        string       `json:"email,omitempty"`
	Anyone       bool         `json:"anyone,omitempty"`
	Title        string       `json:"title,omitempty"`
	Find         string       `json:"find,omitempty"`
	Replacement  string       `json:"replacement,omitempty"`
	AfterHeading string       `json:"after_heading,omitempty"`
	Confirmed    bool         `json:"confirmed,omitempty"`
}

// RegisterTools registers a stable daemon catalog, including while disconnected.
// It deliberately uses the existing Registry interface, not a pointer to it.
func RegisterTools(reg tools.Registry, p TokenProvider, opts ToolOptions) error {
	if reg == nil {
		return errors.New("googleapi: tool registry is required")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.GmailBaseURL == "" {
		opts.GmailBaseURL = "https://gmail.googleapis.com/gmail/v1"
	}
	if opts.DriveBaseURL == "" {
		opts.DriveBaseURL = "https://www.googleapis.com/drive/v3"
	}
	if opts.DriveUploadBaseURL == "" {
		opts.DriveUploadBaseURL = "https://www.googleapis.com/upload/drive/v3"
	}
	if opts.DocsBaseURL == "" {
		opts.DocsBaseURL = "https://docs.googleapis.com/v1"
	}
	c := &client{p: p, opts: opts}
	for _, name := range []string{"gmail", "drive", "docs"} {
		if _, err := reg.Get(name); err == nil {
			return fmt.Errorf("googleapi: tool %s already registered", name)
		}
	}
	for _, name := range []string{"gmail", "drive", "docs"} {
		if err := reg.Register(&googleTool{name, c}); err != nil {
			return err
		}
	}
	return nil
}

func (t *googleTool) Definition() tools.Definition {
	actions := map[string][]string{
		"gmail": {"search", "read", "draft", "send", "modify", "archive", "trash", "untrash", "delete", "labels"},
		"drive": {"search", "list", "get", "download", "upload", "create_folder", "move", "rename", "copy", "share", "trash", "delete"},
		"docs":  {"read", "create", "append", "replace_text", "insert"},
	}
	descriptions := map[string]string{
		"gmail": "Use for connected Gmail: search query syntax, read messages or thread_id, draft/send UTF-8 mail, reply_to_id for threaded replies, labels and mailbox changes. to/cc/bcc are address arrays; attachments use blob digests. send/delete/trash require confirmed:true after explicit chat approval of recipients, subject, attachments and message IDs.",
		"drive": "Use for connected Google Drive: search (Drive query syntax or ordinary text; plain_text:true forces text), list folder_id, get/download id, upload text or blob digest, create_folder, move to folder_id, rename/copy with name, share role/email or anyone:true. download format: pdf/docx/xlsx/pptx/markdown/txt. share/trash/delete require confirmed:true after explicit chat approval of files and sharing recipients/access.",
		"docs":  "Use for Google Docs: read id as heading-structured markdown; create title with optional text; append text; insert text at end or after_heading; replace_text find/replacement. Create/insert/append accept plain text or Markdown source (stored verbatim). replace_text changes every match and requires confirmed:true after explicit chat approval of document and exact replacement.",
	}
	props := map[string]any{}
	fields := map[string]string{
		"gmail": "id query page_token thread_id draft_id reply_to_id subject body name",
		"drive": "id query page_token folder_id format digest mime_type text name role email",
		"docs":  "id title text find replacement after_heading",
	}
	for _, s := range strings.Fields(fields[t.name]) {
		props[s] = map[string]any{"type": "string"}
	}
	props["action"] = map[string]any{"type": "string", "enum": actions[t.name]}
	props["confirmed"] = map[string]any{"type": "boolean"}
	if t.name != "docs" {
		props["max_results"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	}
	if t.name == "drive" {
		for _, s := range []string{"plain_text", "anyone"} {
			props[s] = map[string]any{"type": "boolean"}
		}
	}
	if t.name == "gmail" {
		for _, s := range []string{"to", "cc", "bcc", "add_labels", "remove_labels"} {
			props[s] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		}
		for _, s := range []string{"read", "starred"} {
			props[s] = map[string]any{"type": "boolean"}
		}
		props["attachments"] = map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"digest", "name"}, "additionalProperties": false, "properties": map[string]any{"digest": map[string]string{"type": "string"}, "name": map[string]string{"type": "string"}, "mime_type": map[string]string{"type": "string"}}}}
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "additionalProperties": false, "required": []string{"action"}, "properties": props})
	return tools.Definition{Name: t.name, Description: descriptions[t.name], InputSchema: schema, Effect: types.EffectWriteExternal, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelImmediate, Timeout: 90 * time.Second, OutputBudget: 24000, Profiles: []string{"default"}, Group: "google", Tags: []string{"google", t.name}}
}
func (t *googleTool) ResourceKeys(json.RawMessage) ([]string, error) {
	return []string{"google-account"}, nil
}

func (t *googleTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	fail := func(status tools.ResultStatus, err error) (tools.Result, error) {
		return tools.Result{Status: status, Output: err.Error()}, err
	}
	var a arguments
	dec := json.NewDecoder(bytes.NewReader(inv.Arguments))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return fail(tools.StatusInputError, errors.New("googleapi: invalid arguments"))
	}
	if dec.Decode(new(any)) != io.EOF {
		return fail(tools.StatusInputError, errors.New("googleapi: expected one argument object"))
	}
	if a.Action == "" {
		return fail(tools.StatusInputError, errors.New("action is required"))
	}
	if t.c.p == nil || !t.c.p.Status().Connected {
		return fail(tools.StatusToolError, connectionError(false))
	}
	gated := t.name == "gmail" && (a.Action == "send" || a.Action == "delete" || a.Action == "trash") || t.name == "drive" && (a.Action == "share" || a.Action == "delete" || a.Action == "trash") || t.name == "docs" && a.Action == "replace_text"
	if gated && !a.Confirmed && !t.c.opts.DisableConfirmation && os.Getenv("EXCALIBUR_GOOGLE_CONFIRM") != "0" {
		details, _ := json.Marshal(a)
		return fail(tools.StatusDenied, fmt.Errorf("confirmation required for %s.%s. Show the user exactly what will happen (recipients, subject, attachments, file/document IDs, sharing access or replacement): %s. Ask for explicit approval in chat, then retry with confirmed:true", t.name, a.Action, details))
	}
	var v any
	var err error
	switch t.name {
	case "gmail":
		v, err = t.c.gmail(ctx, a)
	case "drive":
		v, err = t.c.drive(ctx, a)
	case "docs":
		v, err = t.c.docs(ctx, a)
	}
	if err != nil {
		return fail(tools.StatusToolError, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fail(tools.StatusToolError, err)
	}
	return tools.Result{Status: tools.StatusOK, Output: string(b)}, nil
}
func connectionError(reconnect bool) error {
	if reconnect {
		return errors.New("google is not connected; ask the user to open Settings → Connections → Reconnect Google")
	}
	return errors.New("google is not connected; ask the user to open Settings → Connections → Connect Google")
}

const maxData = 25 << 20

func (c *client) request(ctx context.Context, method, endpoint, contentType string, body io.Reader) ([]byte, error) {
	token, err := c.p.AccessToken(ctx)
	if err != nil {
		return nil, connectionError(true)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errors.New("googleapi: invalid endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// Never forward credentials through a redirect, including same-host redirects.
	hc := *c.opts.HTTPClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("googleapi: HTTP request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return nil, connectionError(true)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("googleapi: HTTP %d (check account permissions and resource ID)", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxData+1))
	if err != nil {
		return nil, errors.New("googleapi: could not read response")
	}
	if len(b) > maxData {
		return nil, errors.New("googleapi: response exceeds 25 MiB limit")
	}
	return b, nil
}
func (c *client) json(ctx context.Context, method, endpoint string, in, out any) error {
	var r io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	b, err := c.request(ctx, method, endpoint, "application/json", r)
	if err != nil {
		return err
	}
	if out != nil && len(b) > 0 {
		if json.Unmarshal(b, out) != nil {
			return errors.New("googleapi: invalid JSON response")
		}
	}
	return nil
}
func endpoint(base, path string, q url.Values) string {
	u := strings.TrimRight(base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}
func required(v, name string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}
func limit(n int) int {
	if n <= 0 {
		return 20
	}
	return min(n, 100)
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "… [truncated]"
	}
	return s
}
func (c *client) blob(digest string) ([]byte, error) {
	if c.opts.Blobs == nil {
		return nil, errors.New("googleapi: daemon blob storage unavailable")
	}
	if len(digest) != 64 {
		return nil, errors.New("googleapi: expected SHA-256 blob digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return nil, errors.New("googleapi: invalid blob digest")
	}
	r, _, err := c.opts.Blobs.Get(digest)
	if err != nil {
		return nil, errors.New("googleapi: blob not found")
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxData+1))
	if len(b) > maxData {
		return nil, errors.New("googleapi: blob exceeds 25 MiB limit")
	}
	return b, err
}
func (c *client) saveBlob(b []byte) (string, error) {
	if c.opts.Blobs == nil {
		return "", errors.New("googleapi: daemon blob storage unavailable")
	}
	h := sha256.Sum256(b)
	digest := hex.EncodeToString(h[:])
	if err := c.opts.Blobs.Put(digest, bytes.NewReader(b)); err != nil {
		return "", errors.New("googleapi: could not store download")
	}
	return digest, nil
}
