package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// ReplayRequest is the compiled request the harness would send.
type ReplayRequest struct {
	Model    string                     `json:"model"`
	Messages []providers.Message        `json:"messages"`
	Tools    []providers.ToolDefinition `json:"tools,omitempty"`
}

// ReplayRecord pairs a request hash with the recorded response.
type ReplayRecord struct {
	Hash     string                 `json:"request_hash"`
	Request  ReplayRequest          `json:"request"`
	Response providers.ChatResponse `json:"response"`
}

// HashRequest canonicalizes the request and hashes it. The replay
// server serves the recorded response only while the compiled request
// matches exactly; the first mismatch stops the replay (Section 23.6).
func HashRequest(r ReplayRequest) (string, error) {
	buf, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("eval: marshal replay request: %w", err)
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// Recorder appends records to a JSONL file incrementally so partial
// recordings stay valid.
type Recorder struct {
	mu sync.Mutex
	f  *os.File
}

// NewRecorder creates or appends to path.
func NewRecorder(path string) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("eval: open recorder: %w", err)
	}
	return &Recorder{f: f}, nil
}

// Record hashes req, pairs it with resp and appends the record.
func (r *Recorder) Record(req ReplayRequest, resp providers.ChatResponse) error {
	h, err := HashRequest(req)
	if err != nil {
		return err
	}
	rec := ReplayRecord{Hash: h, Request: req, Response: resp}
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("eval: marshal record: %w", err)
	}
	buf = append(buf, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.f.Write(buf); err != nil {
		return fmt.Errorf("eval: write record: %w", err)
	}
	if err := r.f.Sync(); err != nil {
		return fmt.Errorf("eval: sync record: %w", err)
	}
	return nil
}

// Close closes the recorder.
func (r *Recorder) Close() error { return r.f.Close() }

// Divergence marks the first request that no longer matches the
// recording. Remaining calls run live only with an approved budget
// (Section 23.6).
type Divergence struct {
	Index    int
	WantHash string
	GotHash  string
}

func (d *Divergence) Error() string {
	return fmt.Sprintf("eval: replay diverged at call %d: recorded %s, now %s",
		d.Index, shortHash(d.WantHash), shortHash(d.GotHash))
}

// Player replays recorded responses in order while requests match.
type Player struct {
	mu      sync.Mutex
	records []ReplayRecord
	pos     int
	calls   int
}

// LoadPlayer reads a JSONL recording.
func LoadPlayer(path string) (*Player, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read recording: %w", err)
	}
	var recs []ReplayRecord
	start := 0
	for i, b := range raw {
		if b != '\n' {
			continue
		}
		line := raw[start:i]
		start = i + 1
		if len(line) == 0 {
			continue
		}
		var rec ReplayRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("eval: parse recording: %w", err)
		}
		recs = append(recs, rec)
	}
	return &Player{records: recs}, nil
}

// Replay returns the recorded response when req matches the next
// record, or a *Divergence on the first mismatch. A nil request hash
// error also surfaces as divergence-free error.
func (p *Player) Replay(req ReplayRequest) (providers.ChatResponse, error) {
	h, err := HashRequest(req)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pos >= len(p.records) {
		return providers.ChatResponse{}, &Divergence{Index: p.calls, WantHash: "(exhausted)", GotHash: h}
	}
	want := p.records[p.pos]
	call := p.calls
	p.calls++
	if want.Hash != h {
		return providers.ChatResponse{}, &Divergence{Index: call, WantHash: want.Hash, GotHash: h}
	}
	p.pos++
	return want.Response, nil
}

// Calls counts served-or-diverged calls.
func (p *Player) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// Done reports whether every record was consumed by matching requests.
func (p *Player) Done() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pos == len(p.records)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
