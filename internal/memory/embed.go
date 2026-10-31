package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Embedder produces one vector per input text. Implementations are
// the HTTP provider embedder (default), deterministic fakes for
// tests, and later the local sidecar behind the same interface
// (Section 10.8). Dims is the native width; the service truncates
// the Matryoshka prefix to the configured width.
type Embedder interface {
	// Model names the embedding model (embedding_spaces bookkeeping).
	Model() string
	// Embed returns one vector per text, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Close releases provider resources.
	Close() error
}

// Cosine returns the cosine similarity in [-1, 1]; zero vectors
// score 0.
func Cosine(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// fitDims truncates the Matryoshka prefix to dims or zero-pads short
// vectors. Both models of Section 10.8 support Matryoshka prefixes.
func fitDims(v []float32, dims int) []float32 {
	if len(v) == dims {
		return v
	}
	out := make([]float32, dims)
	copy(out, v)
	return out
}

func decodeVector(raw []byte) ([]float32, error) {
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("memory: bad vector bytes %d", len(raw))
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return out, nil
}

// ---- fake embedder ----

// FakeEmbedder is the deterministic test embedder: each word hashes
// into buckets, so texts sharing words score higher. Similar inputs
// stay similar across calls; randomness never enters.
type FakeEmbedder struct {
	model string
	dims  int
	calls int
}

// NewFakeEmbedder returns a deterministic embedder.
func NewFakeEmbedder(model string, dims int) *FakeEmbedder {
	if dims <= 0 {
		dims = 64
	}
	return &FakeEmbedder{model: model, dims: dims}
}

// Model implements Embedder.
func (f *FakeEmbedder) Model() string { return f.model }

// Calls counts Embed invocations.
func (f *FakeEmbedder) Calls() int { return f.calls }

// Embed implements Embedder.
func (f *FakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		vec := make([]float32, f.dims)
		for _, w := range strings.Fields(strings.ToLower(t)) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(w))
			vec[int(h.Sum32()%uint32(f.dims))]++
		}
		var norm float64
		for _, v := range vec {
			norm += float64(v * v)
		}
		if norm > 0 {
			norm = math.Sqrt(norm)
			for i := range vec {
				vec[i] /= float32(norm)
			}
		}
		out = append(out, vec)
	}
	return out, nil
}

// Close implements Embedder.
func (f *FakeEmbedder) Close() error { return nil }

// ErrorEmbedder always fails: it simulates the sidecar being down so
// tests prove degradation to lexical, temporal and relational search
// (Section 10.8).
type ErrorEmbedder struct {
	model string
	err   error
}

// NewErrorEmbedder returns an always-failing embedder.
func NewErrorEmbedder(model string, err error) *ErrorEmbedder {
	if err == nil {
		err = fmt.Errorf("memory: embedding provider down")
	}
	return &ErrorEmbedder{model: model, err: err}
}

// Model implements Embedder.
func (e *ErrorEmbedder) Model() string { return e.model }

// Embed implements Embedder.
func (e *ErrorEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, e.err
}

// Close implements Embedder.
func (e *ErrorEmbedder) Close() error { return nil }

// ---- HTTP provider embedder ----

// HTTPOptions configures the OpenAI-compatible embedding endpoint.
// Credentials come from the operator file or the environment; they
// are held in memory only and never logged (Section 10.8, 20.1).
type HTTPOptions struct {
	// BaseURL is the API root, e.g. https://api.openai.com/v1.
	BaseURL string
	APIKey  string
	// FallbackKey retries once on quota/rate-limit errors only.
	FallbackKey string
	Model       string
	// Dimensions requests a Matryoshka prefix; 0 means model default.
	Dimensions int
	Client     *http.Client
	Timeout    time.Duration
	// MaxAttempts bounds transient retries (default 3).
	MaxAttempts int
}

// HTTPEmbedder talks to any OpenAI-compatible /embeddings endpoint
// (Section 10.8 remote path). The default model follows the operator
// credentials file: text-embedding-3-small.
type HTTPEmbedder struct {
	opts HTTPOptions
}

// NewHTTPEmbedder validates options and returns the embedder.
func NewHTTPEmbedder(o HTTPOptions) (*HTTPEmbedder, error) {
	if strings.TrimSpace(o.BaseURL) == "" {
		return nil, fmt.Errorf("memory: embedding base URL is required")
	}
	if strings.TrimSpace(o.APIKey) == "" {
		return nil, fmt.Errorf("memory: embedding API key is required")
	}
	if strings.TrimSpace(o.Model) == "" {
		o.Model = DefaultEmbeddingModel
	}
	if o.MaxAttempts < 1 {
		o.MaxAttempts = 3
	}
	if o.Timeout <= 0 {
		o.Timeout = 60 * time.Second
	}
	return &HTTPEmbedder{opts: o}, nil
}

// Model implements Embedder.
func (e *HTTPEmbedder) Model() string { return e.opts.Model }

// Close implements Embedder.
func (e *HTTPEmbedder) Close() error { return nil }

type embedRequest struct {
	Input      []string `json:"input"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
}

// Embed implements Embedder with bounded retries: transient and
// rate-limit failures retry with backoff; quota/rate-limit errors
// retry once with the fallback key; auth failures return at once.
func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	req := embedRequest{Input: texts, Model: e.opts.Model, Dimensions: e.opts.Dimensions}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var last error
	keys := []string{e.opts.APIKey}
	if e.opts.FallbackKey != "" {
		keys = append(keys, e.opts.FallbackKey)
	}
	for attempt := 1; attempt <= e.opts.MaxAttempts; attempt++ {
		for ki, key := range keys {
			vecs, retry, err := e.post(ctx, raw, key)
			if err == nil {
				return vecs, nil
			}
			last = err
			if !retry {
				// Auth and input errors never retry.
				return nil, err
			}
			if ki == 0 && len(keys) > 1 {
				// Fall through to the fallback key on this attempt.
				continue
			}
			break
		}
		if attempt < e.opts.MaxAttempts {
			backoff := time.Duration(attempt*attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return nil, last
}

// embedError classifies an HTTP failure: retry reports whether the
// caller may try again (transient or rate-limit).
type embedError struct {
	status int
	msg    string
	retry  bool
}

func (e *embedError) Error() string {
	return fmt.Sprintf("memory: embeddings HTTP %d: %s", e.status, e.msg)
}

func (e *HTTPEmbedder) post(ctx context.Context, body []byte, key string) ([][]float32, bool, error) {
	url := strings.TrimRight(e.opts.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := e.opts.Client
	if client == nil {
		client = &http.Client{Timeout: e.opts.Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, false, &embedError{status: resp.StatusCode, msg: "authentication failed"}
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
		return nil, false, &embedError{status: resp.StatusCode, msg: strings.TrimSpace(string(raw))}
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, &embedError{status: resp.StatusCode, retry: true, msg: strings.TrimSpace(string(raw))}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, &embedError{status: resp.StatusCode, msg: strings.TrimSpace(string(raw))}
	}
	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, true, err
	}
	if len(parsed.Data) == 0 {
		return nil, true, fmt.Errorf("memory: empty embedding response")
	}
	out := make([][]float32, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, true, fmt.Errorf("memory: embedding index %d out of range", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, false, nil
}

// ---- spaces, shadow search and the pending queue ----

// spaceID names one versioned embedding space: model plus width.
func spaceID(model string, dims int) string {
	return fmt.Sprintf("%s:%d", model, dims)
}

// ensureSpaceTx returns the space row, creating it when absent.
func ensureSpaceTx(ctx context.Context, ex store.Execer, model string, dims int) (string, error) {
	id := spaceID(model, dims)
	var status string
	err := ex.QueryRow(ctx, `SELECT status FROM embedding_spaces WHERE id=?`, id).Scan(&status)
	if err == nil {
		return id, nil
	}
	if _, err := ex.Exec(ctx, `INSERT INTO embedding_spaces(id, model, dimensions, quantization, status) VALUES(?,?,?,?,?)`,
		id, model, dims, "float32", "active"); err != nil {
		return "", err
	}
	return id, nil
}

// shadowTopK ranks every shadow vector of kind in scopes and space by
// exact cosine in Go. It is the portable path when vec0 is missing
// (pure-Go backend) and the reindex path when models change: exact,
// deterministic, and scoped in SQL (Section 9.5).
func shadowTopK(ctx context.Context, q store.Queryer, kind string, query []float32, scopes []string, space string, k int) ([]string, []float64, error) {
	args := []any{kind, space}
	ph, args := scopePlaceholders(scopes, args)
	rows, err := q.Query(ctx, `SELECT entity_id, vector FROM embedding_vectors WHERE entity_kind=? AND space_id=? AND scope_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var hits []scoredHit
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, nil, err
		}
		vec, err := decodeVector(raw)
		if err != nil {
			continue
		}
		hits = append(hits, scoredHit{id: id, s: Cosine(query, vec)})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	top := topScored(hits, k)
	ids := make([]string, 0, len(top))
	scores := make([]float64, 0, len(top))
	for _, h := range top {
		ids = append(ids, h.id)
		scores = append(scores, h.s)
	}
	return ids, scores, nil
}

// scoredHit is one id with its similarity score.
type scoredHit struct {
	id string
	s  float64
}

// topScored orders by score desc, id asc for ties, and keeps the
// first k. The tie-break keeps every run deterministic.
func topScored(hits []scoredHit, k int) []scoredHit {
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && lessScored(hits[j], hits[j-1]); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	if k > len(hits) || k <= 0 {
		k = len(hits)
	}
	return hits[:k]
}

func lessScored(a, b scoredHit) bool {
	if a.s != b.s {
		return a.s > b.s
	}
	return a.id < b.id
}

// enqueuePendingTx records one vector that could not be computed
// while the provider was down (Section 10.8 degradation).
func enqueuePendingTx(ctx context.Context, ex store.Execer, kind, id, scope string, nowMS int64) error {
	_, err := ex.Exec(ctx, `INSERT INTO pending_vectors(kind, id, scope_id, attempts, next_ms) VALUES(?,?,?,?,?)
		ON CONFLICT(kind, id) DO UPDATE SET attempts=pending_vectors.attempts+1, next_ms=excluded.next_ms`,
		kind, id, scope, 0, nowMS)
	return err
}

// pendingItem is one queued embedding job.
type pendingItem struct {
	kind  string
	id    string
	scope string
}

func listPendingTx(ctx context.Context, q store.Queryer, nowMS int64, limit int) ([]pendingItem, error) {
	rows, err := q.Query(ctx, `SELECT kind, id, scope_id FROM pending_vectors WHERE next_ms<=? ORDER BY next_ms LIMIT ?`, nowMS, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pendingItem
	for rows.Next() {
		var p pendingItem
		if err := rows.Scan(&p.kind, &p.id, &p.scope); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
