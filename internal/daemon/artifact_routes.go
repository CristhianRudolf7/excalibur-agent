package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

var artifactIDPattern = regexp.MustCompile(`^art_[a-f0-9]{32}$`)
var artifactScopePattern = regexp.MustCompile(`^[a-zA-Z0-9_:@-][a-zA-Z0-9_.:@-]{0,255}$`)

func (b *DaemonBackend) artifactOp(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	invalid := &api.Error{Code: api.CodeInvalidParams, Message: "invalid artifact parameters"}
	if b.artifacts == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "artifact store unavailable"}
	}
	switch method {
	case api.MethodArtifactsGet:
		var p api.ArtifactsGetParams
		if strictLibraryParams(raw, &p) != nil || !artifactScopePattern.MatchString(p.ConversationID) || !artifactScopePattern.MatchString(p.RunID) || !artifactIDPattern.MatchString(p.ID) {
			return nil, invalid
		}
		// The local daemon has one principal (Section 20). A supplied conversation
		// and run must match the server-recorded origin; IDs never become paths.
		records, err := b.artifacts.records(ctx, p.ConversationID, p.RunID)
		if err != nil {
			return nil, err
		}
		for _, rec := range records {
			if rec.Artifact.ID != p.ID {
				continue
			}
			if len(rec.Content) > maxArtifactBytes || len(rec.Source) > maxArtifactSourceBytes {
				return nil, &api.Error{Code: api.CodeInternal, Message: "artifact exceeds size limit"}
			}
			result := api.ArtifactsGetResult{Artifact: rec.Artifact, Encoding: "utf-8", Content: string(rec.Content), Source: rec.Source}
			if rec.Artifact.Format != "svg" {
				result.Encoding = "base64"
				result.Content = base64.StdEncoding.EncodeToString(rec.Content)
			}
			return result, nil
		}
		return nil, &api.Error{Code: api.CodeUnknownTarget, Message: "unknown artifact in this conversation and run"}
	case api.MethodArtifactsList:
		var p api.ArtifactsListParams
		if strictLibraryParams(raw, &p) != nil || !artifactScopePattern.MatchString(p.ConversationID) || (p.RunID != "" && !artifactScopePattern.MatchString(p.RunID)) {
			return nil, invalid
		}
		out := api.ArtifactsListResult{Artifacts: []api.Artifact{}}
		runs := []string{p.RunID}
		if p.RunID == "" {
			entries, err := b.artifacts.journal.List(ctx, artifactConversationKey(p.ConversationID))
			if err != nil {
				return nil, err
			}
			runs = nil
			seen := map[string]bool{}
			for i := len(entries) - 1; i >= 0; i-- {
				e := entries[i]
				if e.Type != artifactRunType {
					continue
				}
				var run string
				if err := json.Unmarshal(e.Payload, &run); err != nil {
					return nil, err
				}
				if !seen[run] {
					runs = append(runs, run)
					seen[run] = true
				}
			}
		}
		for _, run := range runs {
			records, err := b.artifacts.records(ctx, p.ConversationID, run)
			if err != nil {
				return nil, err
			}
			for _, rec := range records {
				out.Artifacts = append(out.Artifacts, rec.Artifact)
				if len(out.Artifacts) == 100 {
					return out, nil
				}
			}
		}
		return out, nil
	}
	return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown artifact method"}
}
