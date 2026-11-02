package browser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Target identifies an element across snapshots using its accessible role and name.
type Target struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

// Assertion tests the state after a routine action. A legacy string means text_contains.
type Assertion struct {
	Type   string  `json:"type"`
	Value  string  `json:"value,omitempty"`
	Target *Target `json:"target,omitempty"`
}

// Step is persisted without snapshot refs; Ref is accepted only when recording.
type Step struct {
	Action      string          `json:"action"`
	Ref         string          `json:"ref,omitempty"`
	Target      Target          `json:"target"`
	Value       string          `json:"value,omitempty"`
	Assert      json.RawMessage `json:"assert,omitempty"`
	Destination *Target         `json:"destination,omitempty"`
}

func loadRoutines(dir string) (map[string][]Step, error) {
	data, err := os.ReadFile(filepath.Join(dir, "routines.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string][]Step{}, nil
	}
	if err != nil {
		return nil, err
	}
	var result map[string][]Step
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, errors.New("browser: invalid routine storage")
	}
	if result == nil {
		result = map[string][]Step{}
	}
	return result, nil
}
func saveRoutines(dir string, routines map[string][]Step) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(routines)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".routines-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "routines.json"))
}
func routineNames(routines map[string][]Step) []string {
	out := []string{}
	for name := range routines {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
func parseAssertion(raw json.RawMessage) (Assertion, error) {
	var legacy string
	if json.Unmarshal(raw, &legacy) == nil {
		return Assertion{Type: "text_contains", Value: legacy}, nil
	}
	var a Assertion
	if json.Unmarshal(raw, &a) != nil {
		return a, errors.New("browser: invalid assertion")
	}
	switch a.Type {
	case "text_contains":
	case "url_matches":
		if _, err := regexp.Compile(a.Value); err != nil {
			return a, errors.New("browser: invalid url_matches pattern")
		}
	case "element_visible":
		if a.Target == nil || a.Target.Role == "" {
			return a, errors.New("browser: assertion needs a target")
		}
	default:
		return a, errors.New("browser: unknown assertion")
	}
	return a, nil
}
func textAssertion(a Assertion, text, rawURL string) bool {
	switch a.Type {
	case "text_contains":
		return strings.Contains(text, a.Value)
	case "url_matches":
		re, err := regexp.Compile(a.Value)
		return err == nil && re.MatchString(rawURL)
	}
	return false
}
