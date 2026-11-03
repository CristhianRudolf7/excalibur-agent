package skills

import (
	"math"
	"os"
	"sort"
	"strings"
)

// Suggestion limits and evidence thresholds (Sections 12.4,
// Appendix B).
const (
	// DefaultSuggestionsMax caps suggested skills per task: 2-3
	// focused skills help, 4 or more degrade the gain.
	DefaultSuggestionsMax = 3
	// minEvidenceTasks is the smallest outcome sample before the
	// selector trusts negative per-model evidence.
	minEvidenceTasks = 5
	// negativeGap disables a group for a model when tasks using
	// its skills succeed this much less often than tasks not
	// using them.
	negativeGap = 0.10
)

// Suggestion is one skill proposed for a task: the skill plus a
// one-line reason (Section 12.4, step 1: at most 3 enter L3).
type Suggestion struct {
	Name  string
	Group string
	Why   string
	Score float64
	Entry *Entry
}

// Hit is one search_skills match: name, description, group and
// path (Section 12.4, step 3).
type Hit struct {
	Name        string
	Description string
	Group       string
	Path        string
}

// Loaded is the result of load_skill: the SKILL.md body with the
// metadata the caller needs to resolve deferred tools and honor
// restrictions (Sections 12.4 steps 2 and 4, 12.1). Section and
// RefName name a focused slice when the caller asked for one:
// Section is the served "##" heading, RefName the served
// references/ file; Sections and Refs always list what is available
// so the model can narrow down without guessing.
type Loaded struct {
	Name         string
	Group        string
	Version      string
	Hash         string
	Body         string
	Tools        []string
	AllowedTools []string
	Requires     Requires
	Section      string
	RefName      string
	Sections     []string
	Refs         []string
}

// tokenize lowercases text into alphanumeric tokens. A trailing
// "s" is stripped from tokens longer than 3 characters, so plurals
// ("packages", "wheelhouses", "status codes" aside) conflate with
// their singulars on both the query and the index side. Short
// tokens ("c", "r", "ss", "c++") are never stemmed.
func tokenize(s string) []string {
	raw := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+')
	})
	out := raw[:0]
	for _, t := range raw {
		if len(t) > 3 && strings.HasSuffix(t, "s") {
			t = strings.TrimSuffix(t, "s")
		}
		out = append(out, t)
	}
	return out
}

// termAliases is the cheap semantic leg of suggestion and search
// (Section 12.4: BM25 plus vectors; the vector leg stays optional via
// VectorScorer). A small model often uses short-hands ("py", "js",
// "k8s") or near-synonyms ("linker", "ssl", "db") instead of the
// catalog vocabulary; expansion adds the canonical terms so the
// lexical score still finds the skill. Expansion is recall-only and
// deterministic: it never removes query terms.
var termAliases = map[string][]string{
	"py":         {"python"},
	"js":         {"javascript", "node"},
	"ts":         {"typescript", "javascript", "node"},
	"node":       {"javascript", "npm"},
	"sh":         {"shell", "bash"},
	"bash":       {"shell"},
	"c":          {"compile", "gcc"},
	"c++":        {"cpp", "compile"},
	"cpp":        {"c++", "compile"},
	"go":         {"golang", "modules"},
	"rust":       {"cargo"},
	"java":       {"maven", "gradle", "jvm"},
	"r":          {"statistics", "cran"},
	"pg":         {"postgres", "postgresql", "sql"},
	"psql":       {"postgres", "sql"},
	"postgres":   {"postgresql", "sql"},
	"sqlite":     {"sql", "database"},
	"redis":      {"cache", "queue"},
	"db":         {"database", "sql"},
	"sql":        {"database", "query"},
	"ml":         {"machine", "learning", "model", "pytorch"},
	"ai":         {"machine", "learning", "model"},
	"torch":      {"pytorch"},
	"hf":         {"huggingface", "transformers"},
	"onnx":       {"inference", "export", "runtime"},
	"llm":        {"language", "model", "inference", "transformers"},
	"csv":        {"data", "table"},
	"json":       {"data", "jq"},
	"jsonl":      {"data", "jq"},
	"parquet":    {"data", "columnar", "arrow"},
	"regex":      {"regexp", "regular", "expression", "grep"},
	"regexp":     {"regex", "grep"},
	"tls":        {"ssl", "https", "certificate"},
	"ssl":        {"tls", "certificate"},
	"dns":        {"domain", "name", "resolve", "nameserver"},
	"http":       {"web", "server", "proxy", "nginx"},
	"https":      {"tls", "web", "server"},
	"ssh":        {"remote", "shell"},
	"ci":         {"continuous", "integration", "test", "build"},
	"container":  {"docker"},
	"containers": {"docker"},
	"image":      {"docker", "container", "media", "images"},
	"vm":         {"virtual", "machine", "qemu"},
	"vms":        {"virtual", "machine", "qemu"},
	"kvm":        {"qemu", "virtual", "machine"},
	"gpu":        {"cuda", "training", "inference"},
	"pdf":        {"latex", "document"},
	"tex":        {"latex", "document"},
	"git":        {"version", "control", "commit", "branch"},
	"rebase":     {"git", "history"},
	"cmake":      {"build"},
	"make":       {"build", "makefile"},
	"makefile":   {"make", "build"},
	"gcc":        {"compile", "c", "toolchain"},
	"clang":      {"compile", "c", "toolchain"},
	"linker":     {"link", "library"},
	"ldd":        {"link", "library", "shared"},
	"rpath":      {"link", "library", "runtime"},
	"debug":      {"debugging", "troubleshoot"},
	"gdb":        {"debugging"},
	"perf":       {"profiling", "performance", "benchmark"},
	"profiling":  {"performance", "profile"},
	"test":       {"testing"},
	"tdd":        {"testing"},
	"auth":       {"authentication", "security", "login"},
	"secret":     {"credential", "token", "password", "vault"},
	"secrets":    {"credential", "token", "vault"},
	"crypto":     {"cryptography", "encryption", "openssl", "tls"},
	"elf":        {"binary", "executable", "symbols"},
	"package":    {"packaging", "install", "dependency"},
	"packages":   {"packaging", "install"},
	"dependency": {"package", "install"},
	"offline":    {"cache", "vendor", "wheelhouse"},
	"venv":       {"virtualenv", "environment", "python"},
	"conda":      {"environment", "python", "mamba"},
	"dockerfile": {"docker", "container", "image"},
	"compose":    {"docker", "container"},
	"systemd":    {"service", "unit", "daemon", "systemctl"},
	"nginx":      {"http", "proxy", "reverse", "server", "static"},
	"qemu":       {"virtual", "machine", "emulation"},
	"latex":      {"tex", "document", "pdf", "paper"},
	"fortran":    {"gfortran", "scientific", "numeric"},
	"cobol":      {"mainframe", "gnucobol", "legacy"},
	"haskell":    {"ghc", "cabal", "functional"},
	"ocaml":      {"dune", "opam", "functional"},
	"archive":    {"compression", "tar", "zip"},
	"tar":        {"archive", "compression"},
	"zip":        {"archive", "compression"},
	"shell":      {"bash"},
	"terminal":   {"shell", "command", "line"},
	"pipeline":   {"pipe", "shell"},
	"build":      {"compile", "make"},
	"compile":    {"build", "toolchain"},
	"network":    {"networking", "dns", "tcp"},
	"port":       {"network", "tcp", "listen"},
	"service":    {"systemd", "server", "daemon"},
	"cache":      {"redis", "expiry"},
	"queue":      {"redis", "background"},
	"model":      {"machine", "learning", "inference"},
	"training":   {"train", "gpu"},
	"quantize":   {"quantization", "inference"},
	"video":      {"ffmpeg", "media"},
	"audio":      {"ffmpeg", "media"},
	"plot":       {"chart", "matplotlib"},
	"data":       {"csv", "json", "dataset"},
}

// stopTerms are query terms with no retrieval value: English
// function words and stray single letters. A small model writes
// chatty instructions ("add a Makefile with a run target"), and
// without this filter the articles and prepositions outvote the
// content words. Single letters stay dropped except "c" and "r",
// which name real languages in this catalog.
var stopTerms = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true,
	"but": true, "with": true, "for": true, "from": true, "that": true,
	"this": true, "these": true, "those": true, "then": true, "than": true,
	"also": true, "just": true, "into": true, "over": true, "under": true,
	"while": true, "when": true, "where": true, "which": true, "what": true,
	"how": true, "are": true, "was": true, "were": true, "been": true,
	"has": true, "have": true, "had": true, "will": true, "would": true,
	"should": true, "could": true, "there": true, "their": true, "they": true,
	"them": true, "its": true, "it": true, "is": true, "in": true,
	"on": true, "of": true, "to": true, "as": true, "at": true,
	"by": true, "be": true, "do": true, "does": true, "did": true,
	"not": true, "no": true, "if": true, "else": true, "via": true,
	"per": true, "each": true, "such": true, "only": true, "both": true,
	"between": true, "before": true, "after": true, "about": true,
	"my": true, "your": true, "our": true, "you": true, "we": true,
	"us": true, "me": true, "all": true, "any": true, "can": true,
	"s": true, "t": true, "d": true, "ll": true, "re": true, "ve": true,
}

// weightedTerm is one scoring term with its vote weight: literal
// query words vote full weight, alias expansions and environment
// evidence vote fractions. Without this, dozens of environment
// terms (every binary on PATH) outvote the handful of instruction
// words, and alias matches in skill names outvote literal matches.
type weightedTerm struct {
	term string
	w    float64
}

// expandWeighted expands terms like expandTerms but carries vote
// weights: literals vote literalW, aliases vote aliasW. When a term
// arrives both ways the higher weight wins. Output order is
// deterministic (first-seen) so reasons and tests stay stable.
func expandWeighted(terms []string, literalW, aliasW float64) []weightedTerm {
	best := map[string]float64{}
	var order []string
	add := func(t string, w float64) {
		if t == "" {
			return
		}
		if _, ok := best[t]; !ok {
			if len(best) >= 64 {
				return
			}
			order = append(order, t)
		}
		if w > best[t] {
			best[t] = w
		}
	}
	for _, t := range terms {
		if stopTerms[t] {
			continue
		}
		if len(t) < 2 && t != "c" && t != "r" {
			continue
		}
		add(t, literalW)
		for _, a := range termAliases[t] {
			add(a, aliasW)
		}
	}
	out := make([]weightedTerm, 0, len(order))
	for _, t := range order {
		out = append(out, weightedTerm{term: t, w: best[t]})
	}
	return out
}

// expandTerms adds alias terms to terms, deduplicated, order stable,
// capped so a hostile query cannot blow up scoring. Stopwords are
// dropped before expansion so they neither score nor fan out into
// aliases.
func expandTerms(terms []string) []string {
	wts := expandWeighted(terms, 1, 1)
	out := make([]string, 0, len(wts))
	for _, wt := range wts {
		out = append(out, wt.term)
	}
	return out
}

// fieldWeights score name matches above description above headings,
// mirroring "BM25 over name, description and body headings".
const (
	weightName = 3.0
	weightDesc = 2.0
	weightHead = 1.0
)

// lexicalScore is a deterministic BM25-style score of entry e
// against weighted query terms: per-field term frequency saturated
// as tf/(tf+1), weighted per field and per term vote, times an IDF
// computed over the candidate set.
func lexicalScore(terms []weightedTerm, e *Entry, df map[string]int, n int) float64 {
	nameTok := tokenize(strings.ReplaceAll(e.Name, "-", " ") + " " + e.Name)
	descTok := tokenize(e.Description)
	headTok := tokenize(strings.Join(e.Headings, " "))
	tf := func(toks []string, t string) float64 {
		c := 0
		for _, x := range toks {
			if x == t {
				c++
			}
		}
		if c == 0 {
			return 0
		}
		return float64(c) / float64(c+1)
	}
	var score float64
	for _, wt := range terms {
		idf := math.Log(1 + float64(n)/float64(1+df[wt.term]))
		score += wt.w * idf * (weightName*tf(nameTok, wt.term) + weightDesc*tf(descTok, wt.term) + weightHead*tf(headTok, wt.term))
	}
	return score
}

// scoreEntries ranks entries against query with the lexical score
// plus the optional vector leg. Query terms expand with literal
// weight 1 and alias weight 0.5 (the cheap semantic leg).
func (ix *Index) scoreEntries(query string, all []*Entry) []Suggestion {
	terms := discountFilenames(query, expandWeighted(tokenize(query), 1, 0.5))
	return ix.scoreWeighted(query, terms, all)
}

// scoreWeighted ranks entries against pre-weighted terms. query is
// the original text for the optional vector leg.
func (ix *Index) scoreWeighted(query string, terms []weightedTerm, all []*Entry) []Suggestion {
	if len(terms) == 0 {
		return nil
	}
	df := map[string]int{}
	seen := map[string]map[string]bool{}
	for _, e := range all {
		seen[e.Name] = map[string]bool{}
		for _, t := range append(append(tokenize(strings.ReplaceAll(e.Name, "-", " ")), tokenize(e.Description)...), tokenize(strings.Join(e.Headings, " "))...) {
			if !seen[e.Name][t] {
				seen[e.Name][t] = true
				df[t]++
			}
		}
	}
	ix.mu.RLock()
	vector := ix.vector
	ix.mu.RUnlock()
	var out []Suggestion
	for _, e := range all {
		s := lexicalScore(terms, e, df, len(all))
		if vector != nil {
			s += vector.Score(query, e)
		}
		if s > 0 {
			out = append(out, Suggestion{Name: e.Name, Group: e.Group, Score: s, Entry: e})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// filenameTerms extracts the stemmed components of dotted file
// names mentioned in the query ("notes.txt", "data.csv"). Such
// words name artifacts, not intent: "notes" in "create notes.txt"
// is weaker evidence than "git" in "commit it with git". The
// caller discounts them; a file that ALSO exists in the project
// still contributes full environment weight through EnvSignals,
// so real project vocabulary is never lost.
func filenameTerms(query string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(query) {
		w = strings.Trim(w, "\"'`()[]{}<>:;,!?")
		if !strings.Contains(w, ".") {
			continue
		}
		lead := strings.TrimLeft(w, "./")
		if !strings.Contains(lead, ".") {
			continue
		}
		for _, t := range tokenize(lead) {
			out[t] = true
		}
	}
	return out
}

// discountFilenames halves the vote of filename-derived terms (see
// filenameTerms). Intent words keep full weight.
func discountFilenames(query string, terms []weightedTerm) []weightedTerm {
	fn := filenameTerms(query)
	if len(fn) == 0 {
		return terms
	}
	out := make([]weightedTerm, 0, len(terms))
	for _, wt := range terms {
		if fn[wt.term] {
			wt.w *= 0.5
		}
		out = append(out, wt)
	}
	return out
}

// EnvSignals carries cheap environment evidence collected at task
// start: the files visible in the work tree and the binaries on
// PATH. Both are plain string lists (base names or relative paths);
// SuggestWithEnv maps them to catalog vocabulary without running
// anything expensive.
type EnvSignals struct {
	// Files lists paths visible at task start (top-level directory
	// entries are enough; deeper trees add noise, not signal).
	Files []string
	// Bins lists binary names on PATH.
	Bins []string
}

// envFileKeywords maps one file base name to catalog keywords.
// Matching is case-insensitive on the base name; the returned terms
// are already catalog vocabulary (no further aliasing needed, though
// aliases apply harmlessly on top).
func envFileKeywords(base string) []string {
	b := strings.ToLower(base)
	exact := map[string][]string{
		"go.mod":             {"go", "golang", "modules"},
		"go.sum":             {"go", "modules"},
		"cargo.toml":         {"rust", "cargo"},
		"cargo.lock":         {"rust", "cargo"},
		"package.json":       {"node", "npm", "javascript"},
		"package-lock.json":  {"node", "npm"},
		"makefile":           {"make", "build"},
		"gnumakefile":        {"make", "build"},
		"cmakelists.txt":     {"cmake", "build"},
		"configure":          {"autotools", "build"},
		"configure.ac":       {"autotools"},
		"dockerfile":         {"docker", "container"},
		"docker-compose.yml": {"docker", "compose"},
		"compose.yaml":       {"docker", "compose"},
		"environment.yml":    {"conda", "python"},
		"requirements.txt":   {"python", "pip"},
		"pyproject.toml":     {"python", "pip"},
		"setup.py":           {"python"},
		"poetry.lock":        {"python"},
		"dune-project":       {"ocaml", "dune"},
		"stack.yaml":         {"haskell", "stack"},
		"pom.xml":            {"java", "maven"},
		"build.gradle":       {"java", "gradle"},
		"nginx.conf":         {"nginx", "http"},
	}
	if kw, ok := exact[b]; ok {
		return kw
	}
	if strings.HasSuffix(b, ".service") {
		return []string{"systemd", "service"}
	}
	if strings.HasSuffix(b, ".opam") {
		return []string{"ocaml", "opam"}
	}
	ext := ""
	if i := strings.LastIndex(b, "."); i >= 0 {
		ext = b[i:]
	}
	if kw, ok := map[string][]string{
		".go":      {"go", "golang"},
		".rs":      {"rust", "cargo"},
		".py":      {"python"},
		".js":      {"javascript", "node"},
		".ts":      {"typescript", "node"},
		".c":       {"c", "gcc", "compile"},
		".h":       {"c", "compile"},
		".cpp":     {"c++", "cpp", "compile"},
		".cc":      {"c++", "cpp", "compile"},
		".cxx":     {"c++", "cpp", "compile"},
		".hpp":     {"c++", "compile"},
		".java":    {"java"},
		".ml":      {"ocaml", "dune"},
		".mli":     {"ocaml", "dune"},
		".hs":      {"haskell"},
		".cabal":   {"haskell"},
		".f":       {"fortran", "gfortran"},
		".for":     {"fortran", "gfortran"},
		".f90":     {"fortran", "gfortran"},
		".r":       {"r", "statistics"},
		".cbl":     {"cobol", "gnucobol"},
		".cob":     {"cobol", "gnucobol"},
		".tex":     {"latex", "document"},
		".sql":     {"sql", "database"},
		".db":      {"sqlite", "database"},
		".sqlite":  {"sqlite", "database"},
		".sqlite3": {"sqlite", "database"},
		".parquet": {"parquet", "data"},
		".csv":     {"csv", "data"},
		".tsv":     {"data"},
		".json":    {"json", "data", "jq"},
		".jsonl":   {"json", "data", "jq"},
		".sh":      {"shell", "bash", "script"},
	}[ext]; ok {
		return kw
	}
	return nil
}

// envBinKeywords maps one binary name to catalog keywords.
func envBinKeywords(bin string) []string {
	b := strings.ToLower(bin)
	if kw, ok := map[string][]string{
		"gcc": {"c", "gcc", "compile"}, "cc": {"c", "compile"}, "g++": {"c++", "compile"},
		"clang": {"clang", "compile"}, "clang++": {"c++", "compile"},
		"make": {"make", "build"}, "cmake": {"cmake", "build"}, "ninja": {"build"},
		"autoconf": {"autotools"}, "automake": {"autotools"},
		"cargo": {"rust", "cargo"}, "rustc": {"rust", "cargo"},
		"go": {"go", "modules"}, "node": {"node", "npm"}, "npm": {"node", "npm"}, "npx": {"node"},
		"python": {"python", "pip"}, "python3": {"python", "pip"}, "pip": {"python", "pip"}, "uv": {"python", "pip"},
		"conda": {"conda", "python"}, "mamba": {"conda", "python"},
		"opam": {"ocaml"}, "dune": {"ocaml", "dune"}, "ocaml": {"ocaml"},
		"ghc": {"haskell"}, "cabal": {"haskell"}, "stack": {"haskell"},
		"gfortran": {"fortran"}, "r": {"r", "statistics"}, "rscript": {"r", "statistics"},
		"cobc": {"cobol"}, "javac": {"java"}, "java": {"java"}, "mvn": {"java", "maven"}, "gradle": {"java", "gradle"},
		"psql": {"postgres", "sql"}, "pg_ctl": {"postgres", "sql"}, "postgres": {"postgres", "sql"},
		"redis-cli": {"redis"}, "redis-server": {"redis"}, "sqlite3": {"sqlite"},
		"docker": {"docker", "container"}, "podman": {"docker", "container"},
		"nginx": {"nginx", "http"}, "systemctl": {"systemd", "service"}, "journalctl": {"systemd"},
		"ssh": {"ssh", "remote"}, "scp": {"ssh", "remote"},
		"tmux": {"tmux", "terminal"}, "screen": {"terminal"},
		"openssl": {"tls", "ssl", "certificate"}, "dig": {"dns"}, "nslookup": {"dns"}, "host": {"dns"},
		"curl": {"http"}, "wget": {"http"},
		"git": {"git", "version", "control"}, "perf": {"profiling", "performance"},
		"gdb": {"debugging"}, "valgrind": {"memory", "debugging", "profiling"},
		"jq": {"json", "jq"}, "yq": {"yaml", "json"},
		"ffmpeg": {"ffmpeg", "media"}, "ffprobe": {"ffmpeg", "media"},
		"magick": {"image"}, "convert": {"image"}, "identify": {"image"},
		"pdflatex": {"latex"}, "xelatex": {"latex"}, "lualatex": {"latex"}, "latexmk": {"latex"},
		"nvidia-smi": {"gpu", "cuda"}, "nvcc": {"gpu", "cuda"},
		"tar": {"archive", "compression"}, "zip": {"archive", "compression"},
		"unzip": {"archive"}, "7z": {"archive"}, "zstd": {"compression"}, "gzip": {"compression"},
		"ps": {"process"}, "ss": {"network", "port"},
	}[b]; ok {
		return kw
	}
	if strings.HasPrefix(b, "qemu-") {
		return []string{"qemu", "virtual", "machine"}
	}
	return nil
}

// envKeywords converts environment signals to catalog query terms
// by joining the file vocabulary and the binary vocabulary. Base-
// system binaries (see ignoredBins) are skipped: they vote
// identically on every task.
func envKeywords(env EnvSignals) []string {
	seen := map[string]bool{}
	var out []string
	add := func(terms []string) {
		for _, t := range terms {
			if t == "" || seen[t] || len(out) >= 32 {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	add(fileKeywords(env.Files))
	add(binKeywords(env.Bins))
	return out
}

// fileKeywords maps project file names to catalog vocabulary.
func fileKeywords(files []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(terms []string) {
		for _, t := range terms {
			if t == "" || seen[t] || len(out) >= 32 {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	nFiles := len(files)
	if nFiles > 64 {
		nFiles = 64
	}
	for _, f := range files[:nFiles] {
		base := f
		if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
			base = base[i+1:]
		}
		// A checked-in .git directory is the strongest project
		// signal there is; extension-less dotfiles are noise.
		if strings.ToLower(base) == ".git" {
			add([]string{"git", "version", "control", "commit"})
			continue
		}
		if base == "" || strings.HasPrefix(base, ".") && !strings.Contains(base[1:], ".") {
			continue
		}
		add(envFileKeywords(base))
	}
	return out
}

// binKeywords maps binary names to catalog vocabulary, skipping
// base-system binaries without a vote.
func binKeywords(bins []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(terms []string) {
		for _, t := range terms {
			if t == "" || seen[t] || len(out) >= 32 {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	nBins := len(bins)
	if nBins > 64 {
		nBins = 64
	}
	for _, b := range bins[:nBins] {
		if ignoredBins[strings.ToLower(b)] {
			continue
		}
		add(envBinKeywords(b))
	}
	return out
}

// SuggestWithEnv consults the index with the task description plus
// cheap environment evidence (Section 12.4, step 1 with 12.3
// environment signals): file names and binaries contribute ecosystem
// vocabulary to the query, so a task starting in a Rust checkout
// surfaces cargo skills even when the instruction never says "Rust".
// Instruction words vote full weight (literals 1, their aliases
// 0.5); project files vote half weight (their aliases a quarter),
// balancing a handful of instruction words against the broader
// background of the checkout. Binaries only corroborate: a
// binary-derived term that also appears in the instruction or the
// files votes the environment weight, but a binary standing alone
// (docker installed, nothing docker about the task) votes a
// residual 0.125/0.03 instead of injecting noise. An empty query
// with environment evidence suggests from the environment alone.
func (ix *Index) SuggestWithEnv(query string, env EnvSignals, model string, max int) []Suggestion {
	terms := expandWeighted(tokenize(query), 1, 0.5)
	known := map[string]bool{}
	for _, wt := range terms {
		known[wt.term] = true
	}
	for _, t := range expandTerms(fileKeywords(env.Files)) {
		known[t] = true
	}
	best := map[string]float64{}
	var order []weightedTerm
	push := func(wt weightedTerm) {
		if _, ok := best[wt.term]; !ok {
			order = append(order, weightedTerm{term: wt.term})
		}
		if wt.w > best[wt.term] {
			best[wt.term] = wt.w
		}
	}
	for _, wt := range terms {
		push(wt)
	}
	for _, wt := range expandWeighted(fileKeywords(env.Files), 0.5, 0.25) {
		push(wt)
	}
	for _, wt := range expandWeighted(binKeywords(env.Bins), 0.5, 0.25) {
		if !known[wt.term] {
			wt.w *= 0.25
		}
		push(wt)
	}
	merged := make([]weightedTerm, 0, len(order))
	for _, wt := range order {
		merged = append(merged, weightedTerm{term: wt.term, w: best[wt.term]})
	}
	merged = discountFilenames(query, merged)
	q := strings.TrimSpace(query)
	if extra := envKeywords(env); len(extra) > 0 {
		q = strings.TrimSpace(q + " " + strings.Join(extra, " "))
	}
	if max <= 0 || max > DefaultSuggestionsMax {
		max = DefaultSuggestionsMax
	}
	ranked := ix.scoreWeighted(q, merged, ix.candidates(model))
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	for i := range ranked {
		ranked[i].Why = "matches " + strings.Join(topTerms(q, ranked[i].Entry), ", ")
	}
	return ranked
}

// readDirNames returns the top-level entry names of dir, capped at
// 64, in directory order. Dotfiles are kept: the caller filters
// them (EnvSignals ignores extension-less dotfiles; a checked-in
// .github or .config dir still contributes its name).
func readDirNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
		if len(out) >= 64 {
			break
		}
	}
	return out, nil
}

// probeBinNames are the binaries worth one LookPath each at task
// start: every name envBinKeywords understands, so the PATH probe
// stays a fixed cheap list instead of a directory walk.
var probeBinNames = []string{
	"gcc", "cc", "g++", "clang", "clang++", "make", "cmake", "ninja",
	"autoconf", "automake", "cargo", "rustc", "go", "node", "npm",
	"python", "python3", "pip", "uv", "conda", "mamba", "opam", "dune",
	"ocaml", "ghc", "cabal", "stack", "gfortran", "R", "Rscript",
	"cobc", "javac", "java", "mvn", "gradle", "psql", "redis-cli",
	"redis-server", "sqlite3", "docker", "podman", "nginx", "systemctl",
	"journalctl", "ssh", "scp", "tmux", "openssl", "dig", "nslookup",
	"curl", "wget", "git", "perf", "gdb", "valgrind", "jq", "yq",
	"ffmpeg", "ffprobe", "magick", "pdflatex", "xelatex", "lualatex",
	"latexmk", "nvidia-smi", "tar", "zip", "unzip", "ps", "ss",
}

// ignoredBins are base-system binaries present on practically every
// Linux machine (compilers, git, downloaders, archives, ssh,
// process and DNS tools). Probing them adds the same votes to
// every task, so they carry no signal and only drown the
// diagnostic binaries (toolchains, databases, media tools) and the
// project files out. Language runtimes stay probed through the
// project files (*.py, *.go, ...) and through their dedicated
// managers (pip/uv/conda/cargo/npm/...), which are far more
// selective than the bare interpreter.
var ignoredBins = map[string]bool{
	"gcc": true, "cc": true, "python": true, "python3": true,
	"pip": true, "git": true, "curl": true, "wget": true,
	"tar": true, "zip": true, "unzip": true, "ssh": true,
	"scp": true, "ps": true, "openssl": true, "dig": true,
}

// CollectEnvSignals gathers task-start environment evidence: the
// top-level entries of dir (capped) plus one LookPath probe per
// known diagnostic binary. dir may be empty (no project);
// lookPath is exec.LookPath in production and a stub in tests. It
// never fails: unreadable directories and missing binaries yield
// fewer signals, not errors.
func CollectEnvSignals(dir string, lookPath func(string) (string, error)) EnvSignals {
	var env EnvSignals
	if dir != "" {
		if ents, err := readDirNames(dir); err == nil {
			env.Files = ents
		}
	}
	if lookPath == nil {
		return env
	}
	for _, b := range probeBinNames {
		if ignoredBins[b] {
			continue
		}
		if _, err := lookPath(b); err == nil {
			env.Bins = append(env.Bins, b)
		}
		if len(env.Bins) >= 64 {
			break
		}
	}
	return env
}

// candidates returns the suggestible entries: indexed, active, and
// not disabled for model by negative evidence.
func (ix *Index) candidates(model string) []*Entry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []*Entry
	for _, e := range ix.entries {
		if e.State == StateArchived {
			continue
		}
		if ix.disabled[model][e.Group] {
			continue
		}
		out = append(out, e.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Suggest consults the index with the task description and returns
// at most max suggestions with a one-line reason each
// (Section 12.4, step 1). max <= 0 means DefaultSuggestionsMax;
// the result never exceeds DefaultSuggestionsMax.
func (ix *Index) Suggest(query, model string, max int) []Suggestion {
	if max <= 0 || max > DefaultSuggestionsMax {
		max = DefaultSuggestionsMax
	}
	ranked := ix.scoreEntries(query, ix.candidates(model))
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	for i := range ranked {
		ranked[i].Why = "matches " + strings.Join(topTerms(query, ranked[i].Entry), ", ")
	}
	return ranked
}

// SuggestForTools returns one task hint among skills declaring an activated tool.
// It shares the normal ranking and per-model exclusions (Section 12.4).
func (ix *Index) SuggestForTools(query, model string, names []string) []Suggestion {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	var entries []*Entry
	for _, entry := range ix.candidates(model) {
		for _, name := range entry.Tools {
			if wanted[name] {
				entries = append(entries, entry)
				break
			}
		}
	}
	ranked := ix.scoreEntries(query, entries)
	if len(ranked) > 1 {
		ranked = ranked[:1]
	}
	for i := range ranked {
		ranked[i].Why = "use load_skill with name=" + ranked[i].Name + " for the activated tools"
	}
	return ranked
}

// topTerms picks up to 3 query terms that hit the entry for the
// one-line reason.
// topTerms picks up to 3 query terms that hit the entry for the
// one-line reason. Matching is on whole tokens, not substrings, so
// reasons never cite single letters that merely appear inside
// other words.
func topTerms(query string, e *Entry) []string {
	hay := map[string]bool{}
	for _, t := range tokenize(strings.ReplaceAll(e.Name, "-", " ") + " " + e.Description + " " + strings.Join(e.Headings, " ")) {
		hay[t] = true
	}
	var out []string
	for _, t := range expandTerms(tokenize(query)) {
		if hay[t] {
			dup := false
			for _, o := range out {
				if o == t {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, t)
			}
		}
		if len(out) == 3 {
			break
		}
	}
	if len(out) == 0 {
		return []string{"the task"}
	}
	return out
}

// Search implements search_skills(query, group?): entries whose
// name, description, group or path match, optionally restricted to
// one group (Section 12.4, step 3). Empty query lists the group.
func (ix *Index) Search(query, group string) []Hit {
	ix.mu.RLock()
	all := make([]*Entry, 0, len(ix.entries))
	for _, e := range ix.entries {
		if e.State == StateArchived {
			continue
		}
		if group != "" && e.Group != group {
			continue
		}
		all = append(all, e.clone())
	}
	ix.mu.RUnlock()
	if strings.TrimSpace(query) == "" {
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		return hitsOf(all)
	}
	ranked := ix.scoreEntries(query, all)
	// Fall back to substring matches when no term scores: short
	// queries (e.g. "git") may not tokenize onto indexed text.
	if len(ranked) == 0 {
		q := strings.ToLower(query)
		for _, e := range all {
			hay := strings.ToLower(e.Name + " " + e.Description + " " + e.Group + " " + e.Path)
			if strings.Contains(hay, q) {
				ranked = append(ranked, Suggestion{Entry: e})
			}
		}
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].Entry.Name < ranked[j].Entry.Name })
	}
	out := make([]Hit, 0, len(ranked))
	for _, s := range ranked {
		out = append(out, Hit{Name: s.Entry.Name, Description: s.Entry.Description, Group: s.Entry.Group, Path: s.Entry.Path})
	}
	return out
}

func hitsOf(all []*Entry) []Hit {
	out := make([]Hit, 0, len(all))
	for _, e := range all {
		out = append(out, Hit{Name: e.Name, Description: e.Description, Group: e.Group, Path: e.Path})
	}
	return out
}

// Load implements load_skill(name): it returns the SKILL.md body so
// the caller can append it as a tool result at the end of the
// history without touching the stable prefix (Section 12.4,
// step 2). References and scripts stay on disk for the normal file
// and execution tools. Loading resolves nothing by itself: the
// caller resolves the declared deferred tools (12.4 step 4, 13.3)
// and honors AllowedTools (12.1).
func (ix *Index) Load(name string) (Loaded, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	e, ok := ix.entries[name]
	if !ok {
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeMissingFile, Detail: "unknown skill: use search_skills to see what exists"}
	}
	if e.State == StateArchived {
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeBadState, Detail: "skill is archived and cannot be loaded"}
	}
	return Loaded{
		Name: name, Group: e.Group, Version: e.Version, Hash: e.Hash, Body: e.Body,
		Tools:        append([]string(nil), e.Tools...),
		AllowedTools: append([]string(nil), e.AllowedTools...),
		Requires:     e.Requires,
		Sections:     SectionNames(e.Body),
		Refs:         append([]string(nil), e.Refs...),
	}, nil
}

// RecordOutcome records one task result for the per-model skill
// effect measurement (Section 12.4, last paragraph): used tells
// whether the task used skill, success whether it succeeded. The
// selector disables suggestions of a group for a model when the
// evidence is negative.
func (ix *Index) RecordOutcome(skill, model string, used, success bool) {
	if skill == "" || model == "" {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ms := ix.stats[skill]
	if ms == nil {
		ms = map[string]*modelStats{}
		ix.stats[skill] = ms
	}
	st := ms[model]
	if st == nil {
		st = &modelStats{}
		ms[model] = st
	}
	if used {
		st.WithTotal++
		if success {
			st.WithSuccess++
		}
	} else {
		st.WithoutTotal++
		if success {
			st.WithoutSucc++
		}
	}
	// Recompute the group verdict for this model from the skills
	// in the same group.
	e, ok := ix.entries[skill]
	if !ok {
		return
	}
	var wt, ws, nt, ns int
	for name, per := range ix.stats {
		oe, ok := ix.entries[name]
		if !ok || oe.Group != e.Group {
			continue
		}
		if s, ok := per[model]; ok {
			wt += s.WithTotal
			ws += s.WithSuccess
			nt += s.WithoutTotal
			ns += s.WithoutSucc
		}
	}
	if ix.disabled[model] == nil {
		ix.disabled[model] = map[string]bool{}
	}
	if wt+nt >= minEvidenceTasks && wt > 0 && nt > 0 &&
		float64(ws)/float64(wt) < float64(ns)/float64(nt)-negativeGap {
		ix.disabled[model][e.Group] = true
	} else {
		delete(ix.disabled[model], e.Group)
	}
}

// SuggestDisabled reports whether a group's suggestions are
// disabled for model after negative evidence.
func (ix *Index) SuggestDisabled(model, group string) bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.disabled[model][group]
}
