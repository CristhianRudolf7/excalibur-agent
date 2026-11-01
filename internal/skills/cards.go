package skills

import (
	"regexp"
	"strings"
)

// Cards for small models (Section 12.4): instead of injecting the
// full L1 catalog and task-start suggestions into every prompt (see
// docs/development/skills.md for the measured cost), cards mode
// injects nothing by default. The model pulls short cards on demand
// through search_skills/load_skill, and the harness appends one
// compact card excerpt to a failed tool result only when the output
// matches a concrete error pattern below.
//
// A card is at most CardMaxLines lines: exact commands, pitfalls
// and verification steps, never generic advice. Error matching is
// deterministic and precision-first: a pattern fires only when the
// output names the failing binary or carries the distinctive error
// string, so unrelated failures never pay for a card.
const (
	// CardMaxLines caps an error-triggered excerpt: short enough
	// for a cheap model to act on without losing the thread.
	CardMaxLines = 40
	// CardMaxChars caps the same excerpt in chars, whichever binds
	// first.
	CardMaxChars = 4000
)

// CardsPointer is the only skills text injected in cards mode: a
// one-line on-demand pointer riding the stable L1 layer. It teaches
// the pull pattern without the catalog's thousands of tokens.
func CardsPointer() string {
	return "On-demand skill cards: use search_skills with a specific query (never empty) " +
		"and load_skill with a name (plus section= or ref= for a slice); " +
		"concrete tool errors may append one short card automatically."
}

// CardExcerpt builds the compact card for skill name: the title,
// the when-to-use line, the Procedure/Pitfalls/Verification
// sections and a pointer to the full skill, truncated to
// CardMaxLines lines and CardMaxChars chars. reason names the
// concrete error that fired the card. An unknown or archived skill
// yields "" and false; a skill without the template sections falls
// back to its body head, still capped.
func (ix *Index) CardExcerpt(name, reason string, maxLines int) (string, bool) {
	loaded, err := ix.LoadForMode(name, LibraryCards)
	if err != nil {
		return "", false
	}
	if maxLines <= 0 || maxLines > CardMaxLines {
		maxLines = CardMaxLines
	}
	var b strings.Builder
	b.WriteString("# " + loaded.Name + " (group " + loaded.Group + ") — card for: " + reason + "\n")
	sections := SectionsOf(loaded.Body)
	byHead := map[string]string{}
	for _, s := range sections {
		if s.Heading != "" {
			byHead[normHeading(s.Heading)] = strings.Trim(s.Body, "\n")
		}
	}
	// The preamble carries the one-line when-to-use framing.
	if pre, ok := byHead[""]; ok && pre != "" {
		first := strings.TrimSpace(strings.SplitN(pre, "\n", 2)[0])
		if first != "" {
			b.WriteString(first + "\n")
		}
	}
	for _, want := range []string{"procedure", "pitfalls", "verification"} {
		body, ok := byHead[want]
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		b.WriteString("## " + want + "\n" + strings.Trim(body, "\n") + "\n")
	}
	out := b.String()
	// Fallback: a skill without template sections still serves its
	// head rather than nothing.
	if !strings.Contains(out, "## ") {
		out = "# " + loaded.Name + " (group " + loaded.Group + ") — card for: " + reason + "\n" +
			strings.Trim(loaded.Body, "\n") + "\n"
	}
	out += "(full skill: load_skill {\"name\": \"" + loaded.Name + "\"})\n"
	out = capLines(out, maxLines, CardMaxChars)
	return out, true
}

// capLines truncates s to at most max lines and maxChars chars,
// marking the cut so the model knows the excerpt is a slice.
func capLines(s string, max, maxChars int) string {
	if len(s) > maxChars {
		s = s[:maxChars]
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > max {
		lines = append(lines[:max-1], "(…truncated to "+itoa(max)+" lines; load the full skill for the rest)")
	}
	return strings.Join(lines, "\n") + "\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// cardTrigger is one error-triggered retrieval rule: the pattern
// matches a failed tool output, skill names the card to append, and
// reason is the one-line why shown in the card header.
type cardTrigger struct {
	re     *regexp.Regexp
	skill  string
	reason string
}

// cardBin maps a missing binary to its card. Only binaries with a
// concrete recipe skill are listed: an unknown binary fires no card
// rather than a generic one.
var cardBin = map[string]string{
	"make": "make-builds", "cmake": "cmake-projects", "ninja": "cmake-projects",
	"gcc": "gcc-clang-toolchain", "g++": "gcc-clang-toolchain", "cc": "gcc-clang-toolchain",
	"c++": "gcc-clang-toolchain", "clang": "gcc-clang-toolchain", "clang++": "gcc-clang-toolchain",
	"ld":     "linking-errors",
	"docker": "docker-containers", "podman": "docker-containers",
	"sqlite3": "sqlite-recipes", "psql": "postgres-operations",
	"redis-cli": "redis-operations", "redis-server": "redis-operations",
	"jq":     "jq-recipes",
	"ffmpeg": "ffmpeg-recipes", "ffprobe": "ffmpeg-recipes",
	"magick": "image-processing-cli", "convert": "image-processing-cli", "identify": "image-processing-cli",
	"openssl": "tls-openssl-recipes",
	"dig":     "dns-troubleshooting", "nslookup": "dns-troubleshooting", "host": "dns-troubleshooting",
	"systemctl": "systemd-units-services", "journalctl": "systemd-units-services",
	"nginx": "nginx-static-services",
	"tmux":  "tmux-persistent-sessions", "screen": "tmux-persistent-sessions",
	"python": "python-pip-uv-venv", "python3": "python-pip-uv-venv",
	"pip": "python-pip-uv-venv", "pip3": "python-pip-uv-venv", "uv": "python-pip-uv-venv",
	"conda": "python-conda-envs", "mamba": "python-conda-envs",
	"cargo": "rust-cargo-workflows", "rustc": "rust-cargo-workflows",
	"go":   "go-modules-toolchain",
	"node": "node-npm-packaging", "npm": "node-npm-packaging", "npx": "node-npm-packaging",
	"r": "r-language-notes", "rscript": "r-language-notes",
	"java": "java-language-notes", "javac": "java-language-notes",
	"mvn": "java-language-notes", "gradle": "java-language-notes",
	"gfortran": "fortran-language-notes", "ifx": "fortran-language-notes",
	"cobc": "cobol-language-notes",
	"ghc":  "haskell-language-notes", "cabal": "haskell-language-notes", "stack": "haskell-language-notes",
	"opam": "ocaml-opam-packaging", "dune": "ocaml-opam-packaging", "ocaml": "ocaml-language-notes",
	"qemu-system-x86_64": "qemu-virtual-machines", "qemu-img": "qemu-virtual-machines",
	"qemu-aarch64": "qemu-virtual-machines",
	"pdflatex":     "latex-documents", "xelatex": "latex-documents",
	"lualatex": "latex-documents", "latexmk": "latex-documents", "bibtex": "latex-documents",
	"awk": "awk-pattern-recipes", "gawk": "awk-pattern-recipes",
	"git": "git-internals",
	"ssh": "ssh-client-essentials", "scp": "ssh-client-essentials",
	"tar": "archive-compression-formats", "unzip": "archive-compression-formats",
	"zip": "archive-compression-formats", "7z": "archive-compression-formats",
	"zstd": "archive-compression-formats",
	"file": "elf-binary-analysis", "readelf": "elf-binary-analysis",
	"perf": "performance-profiling", "valgrind": "performance-profiling",
	"gdb": "systematic-debugging",
}

// cardPatterns are the concrete error strings that fire a card,
// most specific first. Every pattern names the failing subsystem
// literally, so a passing run or an unrelated failure never
// matches. Matching is case-insensitive except where noted.
var cardPatterns = []cardTrigger{
	{re: mustRe(`(?i)database is locked`), skill: "sqlite-recipes", reason: "SQLite lock contention"},
	{re: mustRe(`(?i)missing separator|recipe commences before first target|no rule to make target|make: \*\*\*`), skill: "make-builds", reason: "make build failure"},
	{re: mustRe(`(?i)ssh: (connect to host|connection refused|operation timed out)|permission denied \(publickey\)`), skill: "ssh-client-essentials", reason: "SSH connection failure"},
	{re: mustRe(`(?i)permission denied|EACCES`), skill: "file-permissions-ownership", reason: "permission/ownership failure"},
	{re: mustRe(`(?i)address already in use|EADDRINUSE|port .* in use|bind:.*in use|failed to bind`), skill: "network-ports-debugging", reason: "port bind conflict"},
	{re: mustRe(`(?i)certificate verify failed|self.signed certificate|tls.*handshake|ssl.*error|unknown ca`), skill: "tls-openssl-recipes", reason: "TLS/certificate failure"},
	{re: mustRe(`(?i)undefined reference|ld returned|cannot find -l|relocation .* can not be used|linker command failed|multiple definition`), skill: "linking-errors", reason: "link failure"},
	{re: mustRe(`(?i)not a git repository|fatal:.*git|unknown revision|refusing to merge unrelated`), skill: "git-internals", reason: "git repository/state failure"},
	{re: mustRe(`(?i)go: (go\.mod|no required module|updates to go\.mod needed|missing go\.sum|module .* not found)`), skill: "go-modules-toolchain", reason: "Go module failure"},
	{re: mustRe(`(?i)npm ERR!|cannot find module|node:.*not found`), skill: "node-npm-packaging", reason: "Node/npm failure"},
	{re: mustRe(`(?i)no matching distribution|no matching version|could not find a version.*satisfies`), skill: "python-pip-uv-venv", reason: "pip resolution failure"},
	{re: mustRe(`(?i)packagesnotfound|resolvepackagenotfound|environmentnotfound`), skill: "python-conda-envs", reason: "conda resolution failure"},
	{re: mustRe(`(?i)cmake error`), skill: "cmake-projects", reason: "CMake configure failure"},
	{re: mustRe(`(?i)configure: error`), skill: "autotools-projects", reason: "autotools configure failure"},
	{re: mustRe(`(?i)error\[E\d+\]`), skill: "rust-language-notes", reason: "rustc error code"},
	{re: mustRe(`(?im)^error in .{0,80}?:|execution halted`), skill: "r-language-notes", reason: "R execution failure"},
	{re: mustRe(`(?i)exception in thread|javac:.*error`), skill: "java-language-notes", reason: "Java failure"},
	{re: mustRe(`(?i)fortran runtime error`), skill: "fortran-language-notes", reason: "Fortran runtime failure"},
	{re: mustRe(`.*\.cbl:\d+.*[Ee]rror|(?i)cobc:.*error`), skill: "cobol-language-notes", reason: "COBOL compile failure"},
	{re: mustRe(`(?i)file ".*\.ml", line.*error|ocaml:.*error|dune:.*error`), skill: "ocaml-language-notes", reason: "OCaml failure"},
	{re: mustRe(`(?i)jq: error`), skill: "jq-recipes", reason: "jq query failure"},
	{re: mustRe(`(?i)\btar: .*error|\bunzip: .*error|cannot exec.*archive|invalid compressed data`), skill: "archive-compression-formats", reason: "archive extraction failure"},
	{re: mustRe(`(?i)invalid regular expression|nothing to repeat|repetition.operator|grep: invalid`), skill: "regex-practical", reason: "regex failure"},
	{re: mustRe(`(?i)could not resolve host|temporary failure in name resolution|name or service not known|no address associated with hostname`), skill: "dns-troubleshooting", reason: "DNS resolution failure"},
	{re: mustRe(`(?i)job for .* failed|failed to (start|restart|enable).*service`), skill: "systemd-units-services", reason: "systemd unit failure"},
	{re: mustRe(`(?i)nginx: \[emerg\]`), skill: "nginx-static-services", reason: "nginx config failure"},
	{re: mustRe(`(?i)cannot connect to the docker daemon|no such image|manifest unknown|docker: error`), skill: "docker-containers", reason: "Docker failure"},
	{re: mustRe(`(?i)psql: (error|FATAL)|role .* does not exist|database .* does not exist`), skill: "postgres-operations", reason: "Postgres failure"},
	{re: mustRe(`(?i)oom command not allowed|misconf redis`), skill: "redis-operations", reason: "Redis failure"},
	{re: mustRe(`(?i)^awk: .*error|awk: (syntax|fatal)`), skill: "awk-pattern-recipes", reason: "awk failure"},
	{re: mustRe(`(?is)ffmpeg[\s\S]{0,400}?(error|invalid data|failed|could not|unable to)`), skill: "ffmpeg-recipes", reason: "ffmpeg failure"},
	{re: mustRe(`(?i)qemu.*(error|failed)|could not open disk image`), skill: "qemu-virtual-machines", reason: "QEMU failure"},
	{re: mustRe(`(?i)! latex error|! undefined control sequence|no output pdf produced`), skill: "latex-documents", reason: "LaTeX build failure"},
}

func mustRe(p string) *regexp.Regexp { return regexp.MustCompile(p) }

// missingBinary extracts the binary name from a command-not-found
// output. It understands the shell form ("X: command not found"),
// the harness hint form ("command not found: X", trailing sentence
// punctuation stripped), the dash form ("sh: 1: X: not found") and
// bare reports, returning "" when no binary is identifiable.
func missingBinary(output string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?im)^\s*(?:/usr/bin/)?bash:\s*(?:line \d+:\s*)?(\S+?):\s*command not found`),
		regexp.MustCompile(`(?i)command not found[:\s]+[` + "`" + `'"\s]*([A-Za-z0-9_+.+-]+)`),
		regexp.MustCompile(`(?im)^\s*(\S+?):\s*command not found`),
		regexp.MustCompile(`(?im)^\s*(?:\S+:\s*\d+:\s*)?([A-Za-z0-9_+.+-]+):\s*not found\s*$`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(output); m != nil {
			bin := strings.Trim(m[1], "`'\"")
			bin = strings.TrimRight(bin, ".")
			if i := strings.LastIndex(bin, "/"); i >= 0 {
				bin = bin[i+1:]
			}
			return strings.ToLower(bin)
		}
	}
	return ""
}

// MatchError maps a failed tool output to one skill card. It
// returns the skill name and the one-line reason, or ok=false when
// nothing concrete matches. Concrete error patterns win over the
// command-not-found fallback; at most one card fires per failure.
func MatchError(output string) (name, reason string, ok bool) {
	if strings.TrimSpace(output) == "" {
		return "", "", false
	}
	for _, t := range cardPatterns {
		if t.re.MatchString(output) {
			return t.skill, t.reason, true
		}
	}
	if bin := missingBinary(output); bin != "" {
		if skill, found := cardBin[bin]; found {
			return skill, "missing binary: " + bin, true
		}
		// qemu-user and versioned tool names map by prefix.
		for _, prefix := range []string{"qemu-"} {
			if strings.HasPrefix(bin, prefix) {
				return "qemu-virtual-machines", "missing binary: " + bin, true
			}
		}
	}
	return "", "", false
}

// ErrorCardFor matches output against the trigger table and builds
// the compact excerpt for the winning card. It returns ok=false
// when nothing matches or the card skill is unavailable, so callers
// append nothing.
func (ix *Index) ErrorCardFor(output string, maxLines int) (string, bool) {
	name, reason, ok := MatchError(output)
	if !ok {
		return "", false
	}
	return ix.CardExcerpt(name, reason, maxLines)
}
