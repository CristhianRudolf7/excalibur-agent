// Required-artifact completion gate (Sections 7.3, 7.5).
//
// Small models declare success while required files were never
// created: the instruction names files, paths and outputs (backticked
// absolute paths, "put the function in a file called X", "install to
// Y") that no tool call ever touches, or the run declares done while
// a background server is still starting. The Section 7.5 gate is
// strict over recorded criteria, but on paths with no recorded
// criteria it passes vacuously; the self-verification contrast only
// checks history mentions, never the filesystem.
//
// This file closes that hole with a deterministic,
// engine-independent mechanism:
//
//  1. Requirement extraction: a cheap deterministic pass over the
//     literal instruction pulls out required artifacts — absolute
//     and relative paths, file names, executables, ports, URLs and
//     output files with their expected content — while ignoring
//     example paths in prose ("for example", "e.g.", "such as",
//     pattern descriptions).
//  2. Filesystem gate: before completed, every path-like requirement
//     must exist; outputs must be non-empty; executables must carry
//     an execute bit; outputs with stated content must contain it.
//  3. Background gate: a session that is still alive at finish
//     blocks unless the instruction wants a persistent service and
//     the history shows the agent verified it, or the final report
//     declares it intentionally persistent with verification.
//
// Anything unverified returns the agent to work with a concise
// checklist; returns stay bounded by the gate attempts of Section
// 7.3 and end honest (completed_with_pending), never completed.
//
// The gate never fetches the network and never executes anything:
// ports and URLs are verified through the agent's own successful
// tool history (the agent must have exercised them), paths through
// the local filesystem of the run.
package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ArtifactKind names what a requirement points at.
type ArtifactKind string

// Artifact kinds.
const (
	ArtifactPath       ArtifactKind = "path"
	ArtifactExecutable ArtifactKind = "executable"
	ArtifactPort       ArtifactKind = "port"
	ArtifactURL        ArtifactKind = "url"
	ArtifactOutput     ArtifactKind = "output"
)

// ArtifactRequirement is one deterministic item extracted from the
// literal instruction: a file, directory, executable, port or URL
// the deliverable must provide.
type ArtifactRequirement struct {
	// ID is the short stable code (A1, A2, ...) in extraction order.
	ID string
	// Kind tells the checker how to verify the requirement.
	Kind ArtifactKind
	// Path is the required file or directory (path, executable,
	// output kinds).
	Path string
	// Port is the required TCP port (port kind).
	Port int
	// URL is the required endpoint (url kind).
	URL string
	// Expect holds substrings the file content must contain
	// (output kind only, from stated headers or formats).
	Expect []string
	// Detail is the human-readable statement for notes.
	Detail string
	// Source is the request sentence the requirement came from.
	Source string
}

// ArtifactConfig gates the required-artifact mechanism. It defaults
// ON so it can be measured on and off through the environment.
type ArtifactConfig struct {
	// Enable is the master switch. Off restores the pre-gate
	// behavior (history contrast only, no filesystem checks).
	Enable bool
	// MaxArtifacts caps the deterministic extraction so the
	// injected note stays small and stable.
	MaxArtifacts int
}

// DefaultArtifactConfig returns the ablation defaults, overridden by
// the environment when set.
func DefaultArtifactConfig() ArtifactConfig {
	cfg := ArtifactConfig{Enable: true, MaxArtifacts: 8}
	cfg.Enable = envBool("EXCALIBUR_ARTIFACT_GATE", cfg.Enable)
	cfg.MaxArtifacts = envInt("EXCALIBUR_ARTIFACT_MAX", cfg.MaxArtifacts)
	if cfg.MaxArtifacts < 1 {
		cfg.MaxArtifacts = 1
	}
	return cfg
}

var (
	portRe   = regexp.MustCompile(`(?i)\bports?\s*[:#]?\s*(\d{2,5})\b`)
	urlRe    = regexp.MustCompile("https?://[^\\s\"'<>`,;]+")
	sessIDRe = regexp.MustCompile(`sess-\d+`)
	sessRe   = regexp.MustCompile(`session:\s*(sess-\d+)`)
	aliveRe  = regexp.MustCompile(`alive:\s*(true|false)`)
	aliveEq  = regexp.MustCompile(`alive=(true|false)`)
	exitedRe = regexp.MustCompile(`exited:\s*(true|false)`)
	// commaLineRe matches stated row content such as a CSV header
	// (period,severity,count) on its own instruction line.
	commaLineRe = regexp.MustCompile(`^[A-Za-z_<>][A-Za-z0-9_,<>\s]*,[A-Za-z0-9_,<>\s]+$`)
)

// imperativeVerbs mark sentences that order work to exist. Bare file
// mentions only count inside them (or when quoted), so prose
// examples never become requirements.
var imperativeVerbs = []string{
	"create", "write", "put", "save", "install", "build", "extract",
	"output", "generate", "produce", "compile", "place", "store",
	"implement", "add", "ensure", "must", "required", "should",
	"need", "export", "download", "update", "modify", "fix",
	"edit", "patch", "make",
}

// executableSignals mark a path the deliverable must run.
var executableSignals = []string{
	"install", "compile", "executable", "runnable", "chmod",
	"binary", "run it", "execute",
}

// exampleMarkers mark prose that illustrates rather than requires.
// A candidate after the marker in the same sentence is an example,
// not a requirement.
var exampleMarkers = []string{
	"for example", "e.g.", "such as", "for instance", "example:",
	"examples:", "follows the pattern", "follow the pattern",
}

// outputSignals mark sentences that produce a file.
var outputSignals = []string{
	"write", "create", "save", "output", "generate", "produce",
	"export", "put", "store", "file",
}

// persistenceWords mark instructions that want a service to keep
// running (a server, listener or daemon the verifier will use).
var persistenceWords = []string{
	"server", "servers", "serve", "serving", "listen", "listening",
	"daemon", "background", "localhost", "127.0.0.1", "pypi", "ssh",
	"http", "port", "ports", "webserver", "web server",
}

// persistentDecls mark final-report phrases where the agent states
// the background session stays up on purpose.
var persistentDecls = []string{
	"left running", "still running", "kept running", "keep running",
	"running on port", "server running", "server is running",
	"listening on", "intentionally", "persistent", "in the background",
	"left up",
}

// negations mark sentences that describe what stays as-is. Bare
// file mentions inside them never become requirements; absolute
// paths still count (they name checkable inputs).
var negations = []string{
	"nothing", " not ", " no ", "n't", "never", "without",
	"already ", "unchanged", "leave ",
}

func hasNegation(sentence string) bool {
	padded := " " + strings.ToLower(sentence) + " "
	for _, n := range negations {
		if strings.Contains(padded, n) {
			return true
		}
	}
	return false
}

// ExtractArtifactRequirements runs the cheap deterministic pass over
// the literal instruction: absolute paths, relative paths, quoted
// file spans, bare file names with known extensions, ports and URLs
// become checkable requirements in request order, deduplicated and
// capped at maxArtifacts. It never calls a model and never inspects
// any benchmark material.
func ExtractArtifactRequirements(request string, maxArtifacts int) []ArtifactRequirement {
	return ExtractArtifactRequirementsIn(request, maxArtifacts, "")
}

// ExtractArtifactRequirementsIn is ExtractArtifactRequirements with the
// run workdir: absolute paths under the workdir are deliverable
// candidates even outside the known roots (D3a).
func ExtractArtifactRequirementsIn(request string, maxArtifacts int, workdir string) []ArtifactRequirement {
	if maxArtifacts < 1 {
		maxArtifacts = 8
	}
	text := strings.TrimSpace(request)
	if text == "" {
		return nil
	}
	sentences := splitSentences(text)
	seen := map[string]bool{}
	var out []ArtifactRequirement
	add := func(kind ArtifactKind, path string, port int, url, detail, source string, expect []string) {
		if DeliverableChecksEnabled() && installedDeliverablesEnabled() && installedDeliverablePath(path) {
			sentence := sentenceWith(sentences, path)
			if !installedCreateRe.MatchString(sentence) || installedNegationRe.MatchString(sentence) || isExampleUse(sentence, path) {
				return
			}
		}
		if DeliverableChecksEnabled() && filepath.IsAbs(path) && len(StrictAbsolutePaths(path, workdir)) == 0 {
			return
		}
		norm := strings.ToLower(string(kind) + "|" + path + "|" + strconv.Itoa(port) + "|" + url)
		if norm == "" || seen[norm] {
			return
		}
		if len(out) >= maxArtifacts {
			return
		}
		seen[norm] = true
		id := "A" + strconv.Itoa(len(out)+1)
		out = append(out, ArtifactRequirement{
			ID: id, Kind: kind, Path: path, Port: port, URL: url,
			Expect: append([]string(nil), expect...),
			Detail: detail, Source: source,
		})
	}
	// Absolute paths first: the strongest, most checkable items.
	absPaths := absPathRe.FindAllString(text, -1)
	if DeliverableChecksEnabled() {
		absPaths = StrictAbsolutePaths(text, workdir)
	}
	for _, p := range absPaths {
		cleaned := strings.TrimRight(p, ".,;:")
		sent := sentenceWith(sentences, cleaned)
		if isExampleUse(sent, cleaned) || isBarePatternToken(cleaned) {
			continue
		}
		kind := classifyPath(sent, cleaned)
		add(kind, cleaned, 0, "", string(kind)+" "+cleaned+" exists as stated",
			trimLen(sent, 120), nil)
	}
	// Relative paths.
	for _, m := range relPathRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		p := strings.TrimRight(m[1], ".,;:")
		sent := sentenceWith(sentences, m[0])
		if isExampleUse(sent, m[0]) || isBarePatternToken(p) {
			continue
		}
		kind := classifyPath(sent, p)
		add(kind, p, 0, "", string(kind)+" "+p+" exists as stated",
			trimLen(sent, 120), nil)
	}
	// Quoted spans that name a file or command output.
	for _, m := range quotedRe.FindAllStringSubmatch(text, -1) {
		span := firstNonEmpty(m[1], m[2], m[3])
		span = strings.TrimSpace(strings.TrimRight(span, ".,;:"))
		if span == "" || len(span) > 160 {
			continue
		}
		if !strings.ContainsAny(span, "/.") && !looksLikeFileToken(span) {
			continue
		}
		if isBarePatternToken(span) {
			continue
		}
		sent := sentenceWith(sentences, m[0])
		if isExampleUse(sent, m[0]) {
			continue
		}
		if !hasAny(sent, imperativeVerbs) && !hasAny(sent, outputSignals) {
			continue
		}
		// Multi-word quotes describe usage ("from run import
		// run_tasks") or commands, never a creatable path: only
		// single-token spans become filesystem requirements.
		if strings.Contains(span, " ") {
			continue
		}
		// An import line ("from run import run_tasks") describes
		// how the file is used, not file content: it becomes a
		// path requirement, never a content expectation.
		add(classifyPath(sent, span), span, 0, "",
			string(classifyPath(sent, span))+" "+span+" exists as stated",
			trimLen(sent, 120), nil)
	}
	// Bare file names with known extensions: only when quoted or in
	// an imperative, non-negated sentence, so prose examples and
	// as-is descriptions never count.
	for _, loc := range bareFileRe.FindAllStringIndex(text, -1) {
		f := text[loc[0]:loc[1]]
		// A basename inside a newly supported installed path is not a
		// second, workdir-relative deliverable.
		if installedDeliverablesEnabled() && installedPathFragment(text, loc[0], loc[1]) {
			continue
		}
		if isBarePatternToken(f) {
			continue
		}
		sent := sentenceWith(sentences, f)
		if isExampleUse(sent, f) {
			continue
		}
		if hasNegation(sent) {
			continue
		}
		quoted := isQuoted(text, f)
		if !quoted && !hasAny(strings.ToLower(sent), imperativeVerbs) {
			continue
		}
		kind := classifyPath(sent, f)
		add(kind, f, 0, "", string(kind)+" "+f+" exists as stated",
			trimLen(sent, 120), nil)
	}
	// Ports: explicit port numbers and localhost endpoints.
	for _, m := range portRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > 65535 {
			continue
		}
		sent := sentenceWith(sentences, m[0])
		if isExampleUse(sent, m[0]) {
			continue
		}
		add(ArtifactPort, "", n, "", "service on port "+m[1]+" responds",
			trimLen(sent, 120), nil)
	}
	for _, u := range urlRe.FindAllString(text, -1) {
		cleaned := strings.TrimRight(u, ".,;:)]}")
		sent := sentenceWith(sentences, u)
		if isExampleUse(sent, u) {
			continue
		}
		add(ArtifactURL, "", 0, cleaned, "service at "+cleaned+" responds",
			trimLen(sent, 120), nil)
		if m := regexp.MustCompile(`:(\d{2,5})\b`).FindStringSubmatch(cleaned); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 65535 {
				add(ArtifactPort, "", n, "", "service on port "+m[1]+" responds",
					trimLen(sent, 120), nil)
			}
		}
	}
	attachContentExpectations(text, out)
	// Reassign stable IDs after content attachment kept order.
	for i := range out {
		out[i].ID = "A" + strconv.Itoa(i+1)
	}
	return out
}

// DeliverableRoots are the directories a required absolute path may
// live under (D3a). Anything else is not checked as a deliverable,
// except paths under the run workdir.
var DeliverableRoots = []string{"/app", "/home", "/tmp", "/root", "/srv", "/opt", "/data", "/workspace", "/output", "/var/www"}

// dateShapeRe matches a date-shaped segment that is a value rather than
// a deliverable location.
var dateShapeRe = regexp.MustCompile(`\d{4}/\d{2}/\d{2}`)

// StrictAbsolutePaths returns the absolute paths named in text that
// may be checked as deliverables: the match must not continue a longer
// token on its left (a URL, `data/2025/02/14`), must not be date-shaped,
// and must sit under a known root or under workdir. Trailing
// punctuation is trimmed. Paths are returned in text order.
func StrictAbsolutePaths(text, workdir string) []string {
	var out []string
	for _, loc := range absPathRe.FindAllStringIndex(text, -1) {
		p := strings.TrimRight(text[loc[0]:loc[1]], ".,;:")
		if loc[0] > 0 && (unicode.IsLetter(rune(text[loc[0]-1])) || unicode.IsDigit(rune(text[loc[0]-1])) || strings.ContainsRune("_.:/-", rune(text[loc[0]-1]))) {
			continue
		}
		if dateShapeRe.MatchString(p) {
			continue
		}
		if len(strings.Split(strings.Trim(p, "/"), "/")) < 2 || !underKnownRoot(filepath.Clean(p), workdir) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// underKnownRoot reports whether an absolute path is under one of the
// deliverable roots or under the run workdir.
func underKnownRoot(p, workdir string) bool {
	if installedDeliverablesEnabled() && installedDeliverablePath(p) {
		return true
	}
	if w := strings.TrimRight(workdir, "/"); w != "" && (p == w || strings.HasPrefix(p, w+"/")) {
		return true
	}
	for _, root := range DeliverableRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

// DeliverableChecksEnabled reports the EXCALIBUR_DELIVERABLE_CHECKS
// switch (default on). Off restores the unrestricted absolute-path
// extraction of the gate.
func DeliverableChecksEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_DELIVERABLE_CHECKS"))) {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

// attachContentExpectations links stated row content (a CSV header,
// a key=value format) to the output file in the same instruction:
// the file must then contain it. It only fires when the instruction
// names a header or structure explicitly, so example rows in prose
// never become content requirements. Import-usage quotes and bracket
// patterns ([src][dst]) never qualify: only comma/colon/equals
// content does.
func attachContentExpectations(text string, out []ArtifactRequirement) {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "header") && !strings.Contains(lower, "structure") &&
		!strings.Contains(lower, "columns") {
		return
	}
	var header string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if commaLineRe.MatchString(t) && !strings.Contains(t, " ") {
			header = t
			break
		}
	}
	if header == "" {
		for _, line := range strings.Split(text, "\n") {
			t := strings.TrimSpace(line)
			if commaLineRe.MatchString(t) {
				header = strings.ReplaceAll(t, " ", "")
				break
			}
		}
	}
	if header == "" {
		return
	}
	for i := range out {
		if out[i].Kind != ArtifactOutput && out[i].Kind != ArtifactPath {
			continue
		}
		lower := strings.ToLower(out[i].Path)
		if strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".tsv") {
			out[i].Expect = append(out[i].Expect, header)
			if out[i].Kind == ArtifactPath {
				out[i].Kind = ArtifactOutput
				out[i].Detail = "output " + out[i].Path + " is written and non-empty"
			}
		}
	}
}

// sentenceWith returns the first sentence containing token, or "".
func sentenceWith(sentences []string, token string) string {
	for _, s := range sentences {
		if strings.Contains(s, token) {
			return s
		}
	}
	return ""
}

// isExampleUse reports whether token is used as an illustration in
// its sentence: it sits after an example marker, or the token
// itself is a glob pattern rather than a concrete artifact.
func isExampleUse(sentence, token string) bool {
	if sentence == "" {
		return false
	}
	lower := strings.ToLower(sentence)
	markerAt := -1
	for _, m := range exampleMarkers {
		if i := strings.Index(lower, m); i >= 0 {
			if markerAt < 0 || i < markerAt {
				markerAt = i
			}
		}
	}
	if markerAt >= 0 {
		if idx := strings.Index(sentence, token); idx >= markerAt {
			return true
		}
	}
	return false
}

// isBarePatternToken drops glob-style pattern fragments
// (YYYY-MM-DD_<source>.log, [src][dst]) that name no concrete file.
func isBarePatternToken(tok string) bool {
	if strings.Contains(tok, "*") || strings.Contains(tok, "<") ||
		strings.Contains(tok, ">") || strings.Contains(tok, "[") ||
		strings.Contains(tok, "]") || strings.Contains(tok, "{") ||
		strings.Contains(tok, "}") {
		return true
	}
	return false
}

// classifyPath decides whether a required path is an executable, an
// output, or a plain path from its sentence and location.
func classifyPath(sentence, path string) ArtifactKind {
	if DeliverableChecksEnabled() && installedDeliverablesEnabled() && installedDeliverablePath(path) {
		if strings.Contains(filepath.Clean(path), "/bin/") {
			return ArtifactExecutable
		}
		return ArtifactPath
	}
	lower := strings.ToLower(sentence)
	pl := strings.ToLower(path)
	if hasAny(lower, executableSignals) || strings.Contains(pl, "/bin/") ||
		strings.HasSuffix(pl, ".exe") {
		return ArtifactExecutable
	}
	if hasAny(lower, outputSignals) {
		return ArtifactOutput
	}
	return ArtifactPath
}

// Installed destinations require a named path and a create/install action.
// Mere mentions remain inputs or context rather than finish requirements.
var installedCreateRe = regexp.MustCompile(`(?i)\b(create|install|place|build|put|write|save|compile|produce|generate|make|(?:must|should|will|to)\s+be\s+(?:created|installed|placed|built|put|written|saved|compiled|produced|generated|made))\b`)
var installedNegationRe = regexp.MustCompile(`(?i)\b(do not|don't|never|must not|should not|need not|already)\b`)

func installedDeliverablesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_INSTALLED_DELIVERABLES"))) {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

func installedDeliverablePath(path string) bool {
	path = filepath.Clean(path)
	for _, root := range []string{"/usr/local/bin", "/usr/local/lib", "/usr/local/share", "/opt"} {
		if strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func installedPathFragment(text string, start, end int) bool {
	for _, loc := range absPathRe.FindAllStringIndex(text, -1) {
		if loc[0] <= start && end <= loc[1] && installedDeliverablePath(strings.TrimRight(text[loc[0]:loc[1]], ".,;:")) {
			return true
		}
	}
	return false
}

// looksLikeFileToken reports whether a quoted span names a file
// (has an extension or a slash) rather than prose.
func looksLikeFileToken(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	return bareFileRe.MatchString(s)
}

// isQuoted reports whether token appears inside backticks or quotes
// anywhere in the text.
func isQuoted(text, token string) bool {
	for _, m := range quotedRe.FindAllString(text, -1) {
		if strings.Contains(m, token) {
			return true
		}
	}
	return false
}

func hasAny(lower string, words []string) bool {
	for _, w := range words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// ArtifactFS is the filesystem view of one required path.
type ArtifactFS struct {
	Mode       os.FileMode
	Exists     bool
	IsDir      bool
	Size       int64
	Executable bool
	NonEmpty   bool
}

// StatFunc stats one resolved path. Tests inject fakes; the engine
// passes the OS implementation.
type StatFunc func(path string) (ArtifactFS, error)

// ReadFunc reads one resolved path, capped by the implementation.
type ReadFunc func(path string) (string, error)

// DefaultStat stats the live filesystem: existence, size, the
// execute bit, and (for directories) whether anything is inside.
func DefaultStat(path string) (ArtifactFS, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return ArtifactFS{}, err
	}
	fs := ArtifactFS{Exists: true, Size: fi.Size(), Mode: fi.Mode()}
	if fi.IsDir() {
		fs.IsDir = true
		f, err := os.Open(path)
		if err == nil {
			names, _ := f.Readdirnames(2)
			_ = f.Close()
			fs.NonEmpty = len(names) > 0
		}
		return fs, nil
	}
	fs.NonEmpty = fi.Size() > 0
	fs.Executable = fi.Mode().Perm()&0o111 != 0
	return fs, nil
}

// DefaultRead reads a file capped at 64 KiB for content checks.
func DefaultRead(path string) (string, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(buf) > 64*1024 {
		buf = buf[:64*1024]
	}
	return string(buf), nil
}

// resolveArtifact maps a requirement path onto the run filesystem:
// absolute paths stay absolute; anything else resolves under
// workdir (or the process directory when workdir is empty).
func resolveArtifact(workdir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	trimmed := strings.TrimPrefix(path, "./")
	if workdir != "" {
		return filepath.Join(workdir, trimmed)
	}
	return trimmed
}

// CheckArtifactFiles verifies every path-like requirement against
// the filesystem and every port/URL requirement against the agent's
// own successful tool history. It returns one concise pending per
// problem, most blocking first, capped so the note stays small.
func CheckArtifactFiles(reqs []ArtifactRequirement, history []HistoryTool, workdir string, stat StatFunc, read ReadFunc) []string {
	if stat == nil {
		stat = DefaultStat
	}
	if read == nil {
		read = DefaultRead
	}
	var pendings []string
	corpus := successCorpus(history)
	for _, r := range reqs {
		switch r.Kind {
		case ArtifactPath, ArtifactExecutable, ArtifactOutput:
			if !HardArtifactRequirement(r) {
				continue
			}
			resolved := resolveArtifact(workdir, r.Path)
			fs, err := stat(resolved)
			if err != nil || !fs.Exists {
				pendings = append(pendings,
					"required artifact missing: "+r.Path+" — create it as stated ("+shortSource(r.Source)+")")
				continue
			}
			if TypedArtifactsEnabled() && fs.Mode&(os.ModeSocket|os.ModeNamedPipe) != 0 {
				// IPC endpoints are exercised through clients; reading them here can block.
				continue
			}
			installedDirectory := !TypedArtifactsEnabled() && installedDeliverablesEnabled() && installedDeliverablePath(r.Path) && fs.IsDir
			if r.Kind == ArtifactExecutable && (installedDirectory || (!fs.IsDir && !fs.Executable)) {
				pendings = append(pendings,
					"required artifact not executable: "+r.Path+" — install it so it runs ("+shortSource(r.Source)+")")
				continue
			}
			if r.Kind == ArtifactOutput {
				if (fs.IsDir && !fs.NonEmpty) || (!fs.IsDir && !fs.NonEmpty) {
					pendings = append(pendings,
						"required artifact empty: "+r.Path+" — write the specified content ("+shortSource(r.Source)+")")
					continue
				}
				if len(r.Expect) > 0 && !fs.IsDir {
					content, err := read(resolved)
					if err == nil && !containsAny(content, r.Expect) {
						pendings = append(pendings,
							"required output content missing in "+r.Path+": expected "+quotedList(r.Expect)+" — write it as specified")
					}
				}
			}
		case ArtifactPort:
			token := strconv.Itoa(r.Port)
			if !strings.Contains(corpus, token) {
				pendings = append(pendings,
					"no evidence that port "+token+" serves traffic — start the service and verify it (curl the port or run the client check) before finishing")
			}
		case ArtifactURL:
			token := urlToken(r.URL)
			if token == "" || !strings.Contains(corpus, token) {
				pendings = append(pendings,
					"no evidence that "+r.URL+" responds — exercise it (curl, client install) and confirm success before finishing")
			}
		}
	}
	sort.Strings(pendings)
	if len(pendings) > 5 {
		extra := len(pendings) - 5
		pendings = append(pendings[:5], "…and "+strconv.Itoa(extra)+" more required artifacts need attention")
	}
	return pendings
}

// successCorpus joins the successful tool history into one
// lowercased search body: only passing calls count as verification.
func successCorpus(history []HistoryTool) string {
	var b strings.Builder
	for _, h := range history {
		if h.IsError {
			continue
		}
		b.WriteString(strings.ToLower(h.Args) + "\n")
		b.WriteString(strings.ToLower(h.Output) + "\n")
	}
	return b.String()
}

// urlToken reduces a URL to its matchable core (host plus path) so
// history mentions with different schemes still match.
func urlToken(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimRight(s, "/")
	if s == "" {
		return ""
	}
	return s
}

func containsAny(content string, wants []string) bool {
	for _, w := range wants {
		if strings.Contains(content, w) {
			return true
		}
	}
	return false
}

func quotedList(wants []string) string {
	quoted := make([]string, 0, len(wants))
	for _, w := range wants {
		quoted = append(quoted, `"`+trimLen(w, 60)+`"`)
	}
	return strings.Join(quoted, ", ")
}

func shortSource(source string) string {
	return trimLen(strings.TrimSpace(source), 90)
}

// LiveSessions returns the terminal sessions whose last known state
// is still running, in first-seen order. It replays the history the
// way the executor renders it: exec results carry
// "session: sess-N alive: true/false", exec_io reads carry
// "exited: true/false", and lists carry "alive=true/false".
func LiveSessions(history []HistoryTool) []string {
	if envBool("EXCALIBUR_SESSION_AUTHORITY", true) {
		var states map[string]SessionState
		for _, h := range history {
			if h.SessionStates != nil {
				states = h.SessionStates
			}
		}
		if states != nil {
			var live []string
			seen := map[string]bool{}
			for _, h := range history {
				for _, id := range sessIDRe.FindAllString(h.Args+" "+h.Output, -1) {
					if !seen[id] && states[id].Alive {
						live = append(live, id)
						seen[id] = true
					}
				}
			}
			return live
		}
	}

	alive := map[string]bool{}
	seen := map[string]bool{}
	var order []string
	for _, h := range history {
		ids := sessIDRe.FindAllString(h.Args+" "+h.Output, -1)
		// Prefer the exec-style session header when present.
		if m := sessRe.FindStringSubmatch(h.Output); len(m) == 2 {
			ids = append([]string{m[1]}, ids...)
		}
		if len(ids) == 0 {
			continue
		}
		state, ok := sessionState(h.Output, h.Args)
		if !ok {
			continue
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				order = append(order, id)
			}
			alive[id] = state
		}
	}
	var live []string
	for _, id := range order {
		if alive[id] {
			live = append(live, id)
		}
	}
	return live
}

// sessionState decodes one tool result into (alive, known).
func sessionState(output, args string) (bool, bool) {
	if m := aliveRe.FindStringSubmatch(output); len(m) == 2 {
		return m[1] == "true", true
	}
	if m := aliveEq.FindStringSubmatch(output); len(m) == 2 {
		return m[1] == "true", true
	}
	if m := exitedRe.FindStringSubmatch(output); len(m) == 2 {
		return m[1] == "false", true
	}
	lowerArgs := strings.ToLower(args)
	if strings.Contains(lowerArgs, `"close"`) || strings.Contains(lowerArgs, `"signal"`) {
		if strings.Contains(lowerArgs, "exec_io") || sessIDRe.MatchString(args) {
			if strings.Contains(lowerArgs, `"close"`) {
				return false, true
			}
		}
	}
	return false, false
}

// CheckBackgroundSessions blocks finishing while a session the agent
// started is still running without an intentional, verified reason.
// A live session passes only when the instruction wants a persistent
// service (or the final report declares it intentionally persistent)
// AND the history shows the agent verified it. Otherwise the agent
// goes back to verify-then-stop or declare.
func CheckBackgroundSessions(history []HistoryTool, request, finalText string) []string {
	live := LiveSessions(history)
	if len(live) == 0 {
		return nil
	}
	verified := hasServerVerification(history)
	if verified && (wantsPersistence(request) || declaresPersistence(finalText)) {
		return nil
	}
	ids := strings.Join(live, ", ")
	if !verified {
		return []string{
			"background session " + ids + " still running and unverified — confirm it serves correctly (curl the port, run the client check), then finish; stop it with exec_io close if the task needs no persistent service",
		}
	}
	return []string{
		"background session " + ids + " still running — stop it (exec_io close/signal) or state in the final report that it stays up intentionally",
	}
}

// wantsPersistence reports whether the instruction asks for a
// service that keeps running.
func wantsPersistence(request string) bool {
	return hasAny(strings.ToLower(request), persistenceWords)
}

// declaresPersistence reports whether the final report states the
// session stays up on purpose.
func declaresPersistence(finalText string) bool {
	return hasAny(strings.ToLower(finalText), persistentDecls)
}

// hasServerVerification reports whether the agent exercised a live
// session after it started: any successful exec_io poll past list,
// or a successful client-shaped exec (curl, wget, pip, ssh, http).
func hasServerVerification(history []HistoryTool) bool {
	seenAlive := false
	for _, h := range history {
		if h.IsError {
			continue
		}
		lower := strings.ToLower(h.Args + "\n" + h.Output)
		if strings.Contains(lower, "alive: true") || strings.Contains(lower, "alive=true") {
			seenAlive = true
			continue
		}
		if !seenAlive {
			continue
		}
		if isPoll(h) || isClientCheck(lower) {
			return true
		}
	}
	// A wait_for that matched, or a read showing served output,
	// also counts even without a later call.
	for _, h := range history {
		if h.IsError {
			continue
		}
		lower := strings.ToLower(h.Output)
		if strings.Contains(lower, "matched") || strings.Contains(lower, "200 ok") ||
			strings.Contains(lower, "successfully installed") || strings.Contains(lower, "serving on") {
			return true
		}
	}
	return false
}

// isPoll reports successful exec_io interactions past listing.
func isPoll(h HistoryTool) bool {
	name := strings.ToLower(h.Name)
	if name != "exec_io" && name != "call_tool" {
		return false
	}
	lower := strings.ToLower(h.Args)
	for _, action := range []string{"read", "wait_for", "write", "keys"} {
		if strings.Contains(lower, action) {
			return true
		}
	}
	return false
}

// isClientCheck reports successful client-shaped exec output.
func isClientCheck(lower string) bool {
	for _, w := range []string{"curl", "wget", "pip install", "pip download", "ssh ", "nc ", "telnet", "http://", "https://", "localhost", "127.0.0.1"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// ArtifactRequest is everything the artifact gate needs for one
// finish attempt.
type ArtifactRequest struct {
	// Request is the literal original instruction ("", when unknown).
	Request string
	// Reqs carries pre-extracted requirements (computed when empty).
	Reqs []ArtifactRequirement
	// History carries the tool results in order.
	History []HistoryTool
	// FinalText is the model's final answer text.
	FinalText string
	// Workdir resolves relative requirement paths.
	Workdir string
	// Stat and Read inject the filesystem; nil selects the OS.
	Stat StatFunc
	Read ReadFunc
}

// ApplyArtifactGate merges the required-artifact gate into pendings:
// missing/empty/unexecutable files, unverified ports/URLs, and live
// background sessions. With the switch off, or with no request and
// no extractable requirements, it returns nil (legacy runs,
// including every existing unit test, pass through untouched).
func ApplyArtifactGate(cfg ArtifactConfig, req ArtifactRequest) []string {
	if !cfg.Enable {
		return nil
	}
	reqs := req.Reqs
	if len(reqs) == 0 && strings.TrimSpace(req.Request) != "" {
		reqs = ExtractArtifactRequirements(req.Request, cfg.MaxArtifacts)
	}
	var pendings []string
	if len(reqs) > 0 {
		pendings = append(pendings, CheckArtifactFiles(reqs, req.History, req.Workdir, req.Stat, req.Read)...)
	}
	pendings = append(pendings, CheckBackgroundSessions(req.History, req.Request, req.FinalText)...)
	if len(pendings) > 5 {
		pendings = append(pendings[:5], "…and "+strconv.Itoa(len(pendings)-5)+" more required artifacts need attention")
	}
	return pendings
}

// StrictArtifactGateEnabled limits filesystem obligations to explicit absolute
// output paths. Prose names remain available for advisory checklists.
func StrictArtifactGateEnabled() bool { return envBool("EXCALIBUR_STRICT_ARTIFACT_GATE", true) }

func TypedArtifactsEnabled() bool { return envBool("EXCALIBUR_TYPED_ARTIFACTS", true) }

// HardArtifactRequirement distinguishes requested outputs from prose mentions.
func HardArtifactRequirement(r ArtifactRequirement) bool {
	if !StrictArtifactGateEnabled() || r.Path == "" {
		return true
	}
	return filepath.IsAbs(r.Path) && installedCreateRe.MatchString(r.Source) && !installedNegationRe.MatchString(r.Source) && !isExampleUse(r.Source, r.Path)
}
