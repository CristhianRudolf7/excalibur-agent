package api

// FileChange summarizes the net recorded edits made by one assistant turn.
type FileChange struct {
	Binary    bool       `json:"binary,omitempty"`
	Path      string     `json:"path"`
	Status    string     `json:"status"`
	Added     int        `json:"added"`
	Removed   int        `json:"removed"`
	Hunks     []FileHunk `json:"hunks"`
	Content   *string    `json:"content,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
}

// FileHunk is a unified diff region. Lines include their space, + or - prefix.
type FileHunk struct {
	OldStart int      `json:"old_start"`
	OldLines int      `json:"old_lines"`
	NewStart int      `json:"new_start"`
	NewLines int      `json:"new_lines"`
	Lines    []string `json:"lines"`
}

// CloneChanges isolates persisted messages from event consumers and readers.
func CloneChanges(in []FileChange) []FileChange {
	out := append([]FileChange(nil), in...)
	for i := range out {
		if in[i].Content != nil {
			s := *in[i].Content
			out[i].Content = &s
		}
		if in[i].Hunks != nil {
			out[i].Hunks = append([]FileHunk{}, in[i].Hunks...)
		}
		for j := range out[i].Hunks {
			if in[i].Hunks[j].Lines != nil {
				out[i].Hunks[j].Lines = append([]string{}, in[i].Hunks[j].Lines...)
			}
		}
	}
	return out
}
