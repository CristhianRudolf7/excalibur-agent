package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// chatWorkspacePreparer belongs only to a root chat runner. Nested delegations
// retain their parent's workdir; embedded and eval tool definitions never change.
func (s *Services) chatWorkspacePreparer() func(context.Context, json.RawMessage, string) (json.RawMessage, error) {
	var mu sync.Mutex
	prepared := map[string]json.RawMessage{}
	return func(ctx context.Context, args json.RawMessage, key string) (json.RawMessage, error) {
		mu.Lock()
		defer mu.Unlock()
		if previous, ok := prepared[key]; key != "" && ok {
			return previous, nil
		}
		out, err := s.prepareChatWorkspace(ctx, args)
		if err == nil && key != "" {
			prepared[key] = out
		}
		return out, err
	}
}

func (s *Services) prepareChatWorkspace(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Action string                     `json:"action"`
		Card   map[string]json.RawMessage `json:"card"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	if input.Action != "spawn" || len(input.Card) == 0 {
		return args, nil
	}
	var scope, name, goal string
	if err := json.Unmarshal(input.Card["scope"], &scope); len(input.Card["scope"]) > 0 && err != nil {
		return nil, err
	}
	if scope != "" && scope != "general" {
		return args, nil
	}
	if err := json.Unmarshal(input.Card["name"], &name); len(input.Card["name"]) > 0 && err != nil {
		return nil, err
	}
	if err := json.Unmarshal(input.Card["goal"], &goal); err != nil {
		return nil, fmt.Errorf("a goal is required before creating a workspace")
	}
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("a goal is required before creating a workspace")
	}
	if strings.TrimSpace(name) == "" {
		name = goal
	}
	project, err := s.createChatWorkspace(ctx, name, goal)
	if err != nil {
		return nil, err
	}
	input.Card["scope"], _ = json.Marshal(project.Scope().String())
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	fields["card"], _ = json.Marshal(input.Card)
	return json.Marshal(fields)
}

func (s *Services) createChatWorkspace(ctx context.Context, name, goal string) (*workspace.Project, error) {
	if strings.TrimSpace(s.workspaceDataDir) == "" || s.Projects == nil {
		return nil, fmt.Errorf("managed workspaces require a daemon data directory and project registry")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.workspaceDataDir, 0700); err != nil {
		return nil, err
	}
	data, err := filepath.EvalSymlinks(s.workspaceDataDir)
	if err != nil {
		return nil, err
	}
	data, err = filepath.Abs(data)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(data)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := root.Mkdir("workspaces", 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := root.Lstat("workspaces")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("managed workspaces must be a directory, not a symlink")
	}
	workspaces, err := root.OpenRoot("workspaces")
	if err != nil {
		return nil, err
	}
	defer workspaces.Close()
	// Only a bounded ASCII slug and a random suffix enter the filesystem path.
	var slug strings.Builder
	for _, c := range foldAccents(strings.ToLower(name)) {
		if slug.Len() >= 48 {
			break
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			slug.WriteRune(c)
		} else if slug.Len() > 0 && !strings.HasSuffix(slug.String(), "-") {
			slug.WriteByte('-')
		}
	}
	stem := strings.Trim(slug.String(), "-")
	if stem == "" {
		stem = "project"
	}
	leaf := stem + "-" + strings.ToLower(rand.Text())
	if err := workspaces.Mkdir(leaf, 0700); err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = workspaces.RemoveAll(leaf)
		}
	}()
	dir := filepath.Join(data, "workspaces", leaf)
	cmd := exec.CommandContext(ctx, "git", "init", "--quiet", "--template=", "--initial-branch=main", ".")
	cmd.Dir = dir
	// Inherited Git path overrides must not redirect initialization elsewhere.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("initialize managed Git workspace: %w", err)
	}
	name = strings.TrimSpace(boundedTurnText(name, 120))
	if name == "" {
		name = "Project"
	}
	// Registry names are unique too, including repeated cards and concurrent chats.
	project, err := s.Projects.Register(workspace.Project{Name: name + " (" + leaf + ")", Description: boundedTurnText(goal, 240), Roots: []string{dir}, VCS: "git"})
	if err != nil {
		return nil, err
	}
	keep = true
	return project, nil
}
