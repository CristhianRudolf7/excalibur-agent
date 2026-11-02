package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHardArtifactsRequireAbsoluteCreationPath(t *testing.T) {
	for _, request := range []string{"Create result.txt", "Create ./outputs/result.txt", "Read /app/input.txt", "The file /app/input.txt is available"} {
		reqs := ExtractArtifactRequirements(request, 8)
		if p := CheckArtifactFiles(reqs, nil, "", nil, nil); len(p) != 0 {
			t.Fatalf("%q became a hard obligation: %v", request, p)
		}
	}
	request := "Save the report to /app/result.txt"
	if p := ApplyArtifactGate(DefaultArtifactConfig(), ArtifactRequest{Request: request}); len(p) == 0 {
		t.Fatal("absolute requested output did not block")
	}
	t.Setenv("EXCALIBUR_STRICT_ARTIFACT_GATE", "0")
	if p := ApplyArtifactGate(DefaultArtifactConfig(), ArtifactRequest{Request: "Create absent-result.txt"}); len(p) == 0 {
		t.Fatal("legacy check did not block")
	}
}

func TestTypedArtifactDirectoryAndIPC(t *testing.T) {
	dir := t.TempDir()
	req := ArtifactRequirement{Kind: ArtifactOutput, Path: dir, Source: "Create " + dir, Expect: []string{"content"}}
	if p := CheckArtifactFiles([]ArtifactRequirement{req}, nil, "", nil, nil); len(p) == 0 {
		t.Fatal("empty output directory accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "entry"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if p := CheckArtifactFiles([]ArtifactRequirement{req}, nil, "", nil, nil); len(p) != 0 {
		t.Fatalf("nonempty directory rejected: %v", p)
	}
	for _, mode := range []os.FileMode{os.ModeSocket, os.ModeNamedPipe} {
		stat := func(string) (ArtifactFS, error) { return ArtifactFS{Exists: true, Mode: mode}, nil }
		read := func(string) (string, error) { t.Fatal("IPC artifact read for content"); return "", nil }
		if p := CheckArtifactFiles([]ArtifactRequirement{req}, nil, "", stat, read); len(p) != 0 {
			t.Fatalf("IPC artifact rejected: %v", p)
		}
		t.Setenv("EXCALIBUR_TYPED_ARTIFACTS", "0")
		if p := CheckArtifactFiles([]ArtifactRequirement{req}, nil, "", stat, read); len(p) == 0 {
			t.Fatal("legacy IPC check did not reject empty output")
		}
		t.Setenv("EXCALIBUR_TYPED_ARTIFACTS", "1")
	}
}
