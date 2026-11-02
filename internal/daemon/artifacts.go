package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const (
	maxTurnArtifacts                         = 20
	maxArtifactRecordBytes                   = 20 << 20
	maxArtifactBytes                         = 8 << 20
	maxArtifactSourceBytes                   = 1 << 20
	maxTurnArtifactBytes                     = 16 << 20
	artifactRecordType     journal.EntryType = "visual_artifact"
	artifactRunType        journal.EntryType = "visual_artifact_run"
)

// artifactRecord snapshots bytes when the tool returns. Fetch never reopens a
// mutable tool path, including after reconnects or a daemon restart.
type artifactRecord struct {
	Artifact api.Artifact `json:"artifact"`
	Content  []byte       `json:"content"`
	Source   string       `json:"source,omitempty"`
}

type artifactStore struct {
	mu      sync.Mutex
	journal journal.Store
	bus     *telemetry.Bus
	blobs   gateway.BlobStore
}

func artifactConversationKey(conv string) string { return "visual-artifacts:" + conv }

func (s *artifactStore) records(ctx context.Context, conv, run string) ([]artifactRecord, error) {
	entries, err := s.journal.List(ctx, run)
	if err != nil {
		return nil, err
	}
	var out []artifactRecord
	for _, entry := range entries {
		if entry.Type != artifactRecordType {
			continue
		}
		payload := entry.Payload
		if entry.BlobRef != "" {
			var metadata api.Artifact
			if err := json.Unmarshal(entry.Payload, &metadata); err != nil {
				return nil, err
			}
			if metadata.ConversationID != conv || metadata.RunID != run {
				continue
			}
			if s.blobs == nil {
				return nil, fmt.Errorf("artifact blob store unavailable")
			}
			reader, size, err := s.blobs.Get(entry.BlobRef)
			if err != nil {
				return nil, fmt.Errorf("artifact snapshot unavailable")
			}
			if size > maxArtifactRecordBytes {
				_ = reader.Close()
				return nil, fmt.Errorf("artifact snapshot exceeds size limit")
			}
			payload, err = io.ReadAll(io.LimitReader(reader, maxArtifactRecordBytes+1))
			_ = reader.Close()
			if err != nil || len(payload) > maxArtifactRecordBytes {
				return nil, fmt.Errorf("artifact snapshot read failed")
			}
		}
		var rec artifactRecord
		if err := json.Unmarshal(payload, &rec); err != nil {
			return nil, err
		}
		if rec.Artifact.ConversationID == conv && rec.Artifact.RunID == run {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *artifactStore) collect(ctx context.Context, conv, run, call string, refs []tools.ArtifactRef) ([]api.Artifact, error) {
	if s == nil || len(refs) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.records(ctx, conv, run)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	used := 0
	for _, rec := range previous {
		seen[rec.Artifact.URLOrPath] = true
		used += len(rec.Content) + len(rec.Source)
	}
	var added []api.Artifact
	var problems []error
	for _, ref := range refs {
		if len(previous)+len(added) >= maxTurnArtifacts {
			break
		}
		if seen[ref.Path] {
			continue
		}
		rec, err := snapshotArtifact(conv, run, call, ref)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if used+len(rec.Content)+len(rec.Source) > maxTurnArtifactBytes {
			continue
		}
		if len(previous) == 0 && len(added) == 0 {
			// The conversation index permits discovery without an in-memory map.
			_, err = s.journal.Append(ctx, journal.Entry{RunID: artifactConversationKey(conv), Type: artifactRunType, Payload: journal.MarshalPayload(run)})
			if err != nil {
				return added, err
			}
		}
		payload := journal.MarshalPayload(rec)
		blobRef := ""
		if s.blobs != nil {
			if len(payload) > maxArtifactRecordBytes {
				return added, fmt.Errorf("artifact snapshot exceeds size limit")
			}
			digest := sha256.Sum256(payload)
			blobRef = hex.EncodeToString(digest[:])
			if err := s.blobs.Put(blobRef, bytes.NewReader(payload)); err != nil {
				return added, fmt.Errorf("artifact snapshot could not be stored")
			}
			payload = journal.MarshalPayload(rec.Artifact)
		}
		_, err = s.journal.Append(ctx, journal.Entry{RunID: run, Type: artifactRecordType, Payload: payload, BlobRef: blobRef})
		if err != nil {
			return added, err
		}
		seen[ref.Path] = true
		used += len(rec.Content) + len(rec.Source)
		added = append(added, rec.Artifact)
		if s.bus != nil {
			runID := types.RunID(run)
			if _, err := s.bus.Publish(api.EventArtifactCreated, &runID, nil, api.ArtifactCreatedEvent{ConversationID: conv, RunID: run, Artifact: rec.Artifact}); err != nil {
				return added, err
			}
		}
	}
	return added, errors.Join(problems...)
}

func snapshotArtifact(conv, run, call string, ref tools.ArtifactRef) (artifactRecord, error) {
	var rec artifactRecord
	if conv == "" || run == "" || call == "" {
		return rec, fmt.Errorf("artifact needs conversation, run and tool call")
	}
	mimes := map[string]string{"svg": "image/svg+xml", "png": "image/png", "pdf": "application/pdf"}
	format := strings.ToLower(ref.Format)
	if mimes[format] == "" {
		return rec, fmt.Errorf("unsupported artifact format")
	}
	data, err := readArtifactFile(ref.Path, maxArtifactBytes)
	if err != nil {
		return rec, err
	}
	if format == "svg" && !utf8.Valid(data) {
		return rec, fmt.Errorf("artifact SVG must be UTF-8")
	}
	var source []byte
	if ref.SourcePath != "" {
		source, err = readArtifactFile(ref.SourcePath, maxArtifactSourceBytes)
		if err != nil {
			return rec, err
		}
		if !utf8.Valid(source) {
			return rec, fmt.Errorf("artifact source must be UTF-8")
		}
	}
	digest := sha256.Sum256([]byte(run + "\x00" + ref.Path))
	rec.Artifact = api.Artifact{ID: "art_" + hex.EncodeToString(digest[:16]), ConversationID: conv, RunID: run, ToolCallID: call,
		Kind: ref.Kind, Title: ref.Title, Format: format, Dialect: ref.Dialect, Group: ref.Group,
		MIME: mimes[format], Size: int64(len(data)), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), URLOrPath: ref.Path}
	rec.Content, rec.Source = data, string(source)
	return rec, nil
}

func readArtifactFile(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("artifact path must be absolute and clean")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("artifact file unavailable")
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, fmt.Errorf("artifact must be a regular file within the size limit")
	}
	f, err := os.OpenInRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("artifact file unavailable")
	}
	defer f.Close()
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, fmt.Errorf("artifact must be a regular file within the size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("artifact read failed")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("artifact exceeds size limit")
	}
	return data, nil
}

// artifactTurn binds observations to identities established by the daemon.
type artifactTurn struct {
	store        *artifactStore
	conversation string
	run          string
}

func (t *artifactTurn) collect(ctx context.Context, call string, refs []tools.ArtifactRef) error {
	if t == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := t.store.collect(ctx, t.conversation, t.run, call, refs)
	return err
}

const artifactReadyType journal.EntryType = "visual_artifact_ready"

func (t *artifactTurn) finish(ctx context.Context) error {
	if t == nil || t.store == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s := t.store
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.journal.List(ctx, t.run)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Type == artifactReadyType {
			return nil
		}
	}
	records, err := s.records(ctx, t.conversation, t.run)
	if err != nil || len(records) == 0 {
		return err
	}
	payload := api.ArtifactReadyEvent{ConversationID: t.conversation, RunID: t.run}
	for _, rec := range records {
		payload.Artifacts = append(payload.Artifacts, rec.Artifact)
	}
	if _, err := s.journal.Append(ctx, journal.Entry{RunID: t.run, Type: artifactReadyType, Payload: journal.MarshalPayload(payload)}); err != nil {
		return err
	}
	if s.bus != nil {
		runID := types.RunID(t.run)
		_, err = s.bus.Publish(api.EventArtifactReady, &runID, nil, payload)
	}
	return err
}

func (a *assistantTurns) finishArtifacts(ctx context.Context, turn *artifactTurn) {
	if err := turn.finish(ctx); err != nil {
		a.notice(turn.conversation, "Visual artifacts could not be announced.")
	}
}
