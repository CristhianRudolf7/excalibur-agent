package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Automatic dependency provisioning (agent-quality workstream W3).
//
// Evidence: 12 of 44 failed evaluation trials end as
// environment/tooling-missing (command not found, No module named,
// missing shared libraries) while a package manager and network were
// available, and the agent never installs the missing piece. This file
// implements recognition of missing executables/modules/libraries in
// tool outputs across languages, a provisioning helper that maps them
// to install commands for the detected package managers, and the
// policy switch EXCALIBUR_AUTOPROVISION=auto|suggest|off (default auto).
//
// Every mechanism is gated by the switch so it can be measured on and
// off. Off restores the pre-provisioning behaviour (only the
// actionable hint). Suggest reports the install command without
// running it. Auto runs it when network is available and always
// reports the outcome in the tool result. Provisioning never upgrades
// or removes existing packages: only install/add commands are ever
// generated.

// AutoProvisionEnv is the ablation switch for this workstream.
const AutoProvisionEnv = "EXCALIBUR_AUTOPROVISION"

// ProvisionMode names the provisioning policy.
type ProvisionMode string

// Provisioning policies.
const (
	ProvisionAuto    ProvisionMode = "auto"
	ProvisionSuggest ProvisionMode = "suggest"
	ProvisionOff     ProvisionMode = "off"
)

// ParseProvisionMode parses the switch value. Empty means auto
// (default ON). Unknown values fall back to auto so a typo never
// silently disables provisioning; use explicit off to disable.
func ParseProvisionMode(s string) ProvisionMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ProvisionAuto
	case "auto", "1", "on", "true", "yes", "enable", "enabled":
		return ProvisionAuto
	case "suggest", "advise", "hint", "dry-run", "dry_run":
		return ProvisionSuggest
	case "off", "0", "false", "no", "disable", "disabled":
		return ProvisionOff
	default:
		return ProvisionAuto
	}
}

// ProvisionModeFromEnv reads EXCALIBUR_AUTOPROVISION (default auto).
func ProvisionModeFromEnv() ProvisionMode {
	v, ok := os.LookupEnv(AutoProvisionEnv)
	if !ok {
		return ProvisionAuto
	}
	return ParseProvisionMode(v)
}

// MissingKind names the dependency family.
type MissingKind string

// Dependency families recognized by DetectMissing.
const (
	KindCommand     MissingKind = "command"
	KindPythonMod   MissingKind = "python-module"
	KindNodeMod     MissingKind = "node-module"
	KindRubyGem     MissingKind = "ruby-gem"
	KindRPackage    MissingKind = "r-package"
	KindPerlMod     MissingKind = "perl-module"
	KindSharedLib   MissingKind = "shared-lib"
	KindGoPackage   MissingKind = "go-package"
	KindRustCrate   MissingKind = "rust-crate"
	KindJavaPackage MissingKind = "java-package"
)

// MissingDep is one recognized missing dependency.
type MissingDep struct {
	Kind MissingKind
	// Name is the normalized dependency name: binary for commands,
	// import name for python, bare specifier for node, gem name for
	// ruby, package name for R, ::-form for perl, .so name for
	// shared libs, import path for Go, crate name for Rust.
	Name string
	// Evidence is the short output line that identified it.
	Evidence string
}

// InstallPlan is the provisioning helper output: install commands for
// the detected managers plus the verification step.
type InstallPlan struct {
	Manager string
	Package string
	// Commands are shell commands in order, install-only (never
	// upgrade/remove). For apt the first command is the update when
	// update-once has not run yet.
	Commands []string
	Verify   string
	Note     string
}

var (
	reShellNotFoundColon = regexp.MustCompile(`(?m)^[^:\n]*:\s*(?:line\s+\d+:\s*)?([^:\s/][^:\s]*):\s*(?:command\s+)?not found\s*$`)
	reShellNotFoundBare  = regexp.MustCompile(`(?i)(?:command not found:\s*([a-z0-9_.\-+]+)|([a-z0-9_.\-+]+):\s*command not found)`)
	reShNotFound         = regexp.MustCompile(`(?m)^\s*sh:\s*\d+:\s*([^:\s]+):\s*not found\s*$`)

	rePythonNoModule = regexp.MustCompile(`(?i)No module named\s+['"]?([A-Za-z0-9_\-.]+)['"]?`)
	rePythonCantFind = regexp.MustCompile(`(?i)can't find (?:module|package)\s+['"]?([A-Za-z0-9_\-.]+)['"]?`)

	reNodeNoModule = regexp.MustCompile(`(?im)^\s*(?:Error(?: \[[^\]]+\])?:\s*)?Cannot find module\s+['"]([^'"]+)['"]`)

	reRubyLoadError = regexp.MustCompile(`(?i)cannot load such file\s+--\s+([^\s'"]+)`)
	reRubyLoadErr2  = regexp.MustCompile(`(?i)LoadError[^\n]*--\s+([^\s'"]+)`)

	reRNoPackage = regexp.MustCompile(`(?i)there is no package called\s+['‘’"“”]*([^'"‘’“”\s]+)['‘’"“”]*`)
	reRNotAvail  = regexp.MustCompile(`(?i)dependency\s+['‘’"“”]*([^'"‘’“”\s]+)['‘’"“”]*\s+is not available`)

	rePerlCantLocate = regexp.MustCompile(`(?i)Can't locate\s+(\S+?\.pm)\s+in @INC`)

	reSharedLib = regexp.MustCompile(`([A-Za-z0-9_+\-.]+\.so(?:\.\d+)*):\s*cannot open shared object file:\s*No such file or directory`)
	reLibImport = regexp.MustCompile(`(?i)ImportError:\s*([A-Za-z0-9_+\-.]+\.so[^:\n]*):\s*cannot open shared object file`)

	reGoNoPackage    = regexp.MustCompile(`(?i)(?:cannot find package|no required module provides package)\s+["']?([A-Za-z0-9_.\-/]+)|package\s+([A-Za-z0-9_.\-/]+)\s+is not in (?:GO\w+|goroot)`)
	reGoCannotFind   = regexp.MustCompile(`(?i)cannot find (?:package|module)\s+["']?([A-Za-z0-9_.\-/]+)["']?`)
	reRustNoCrate    = regexp.MustCompile("(?i)can't find crate for\\s+['\"`]?([A-Za-z0-9_\\-]+)['\"`]?")
	reRustUnresolved = regexp.MustCompile("(?i)unresolved import\\s+['\"`]?([A-Za-z0-9_:]+)['\"`]?")
	reRustNoMatch    = regexp.MustCompile("(?i)error:\\s*no matching package named\\s+['\"`]?([A-Za-z0-9_\\-]+)['\"`]?")
	reJavaNoPackage  = regexp.MustCompile(`(?i)error:\s*package\s+([A-Za-z0-9_.]+)\s+does not exist`)
)

// pythonStdlib gates pip provisioning: stdlib names never provision
// (the failure is a version or local-shadowing problem, not a missing
// third-party package).
var pythonStdlib = map[string]bool{
	"abc": true, "aifc": true, "argparse": true, "array": true, "ast": true,
	"asyncio": true, "base64": true, "bdb": true, "binascii": true, "bisect": true,
	"builtins": true, "bz2": true, "calendar": true, "cgi": true, "cgitb": true,
	"chunk": true, "cmath": true, "cmd": true, "code": true, "codecs": true,
	"codeop": true, "collections": true, "colorsys": true, "compileall": true,
	"concurrent": true, "configparser": true, "contextlib": true, "contextvars": true,
	"copy": true, "copyreg": true, "cprofile": true, "csv": true, "ctypes": true,
	"curses": true, "dataclasses": true, "datetime": true, "dbm": true, "decimal": true,
	"difflib": true, "dis": true, "doctest": true, "email": true, "encodings": true,
	"enum": true, "errno": true, "faulthandler": true, "fcntl": true, "filecmp": true,
	"fileinput": true, "fnmatch": true, "fractions": true, "ftplib": true, "functools": true,
	"gc": true, "getopt": true, "getpass": true, "gettext": true, "glob": true,
	"graphlib": true, "grp": true, "gzip": true, "hashlib": true, "heapq": true,
	"hmac": true, "html": true, "http": true, "idlelib": true, "imaplib": true,
	"importlib": true, "inspect": true, "io": true, "ipaddress": true, "itertools": true,
	"json": true, "keyword": true, "lib2to3": true, "linecache": true, "locale": true,
	"logging": true, "lzma": true, "mailbox": true, "mailcap": true, "marshal": true,
	"math": true, "mimetypes": true, "mmap": true, "modulefinder": true, "multiprocessing": true,
	"netrc": true, "nis": true, "nntplib": true, "numbers": true, "operator": true,
	"optparse": true, "os": true, "ossaudiodev": true, "pathlib": true, "pdb": true,
	"pickle": true, "pickletools": true, "pipes": true, "pkgutil": true, "platform": true,
	"plistlib": true, "poplib": true, "posix": true, "pprint": true, "profile": true,
	"pstats": true, "pty": true, "pwd": true, "py_compile": true, "pyclbr": true,
	"pydoc": true, "queue": true, "quopri": true, "random": true, "re": true,
	"readline": true, "reprlib": true, "resource": true, "rlcompleter": true, "runpy": true,
	"sched": true, "secrets": true, "select": true, "selectors": true, "shelve": true,
	"shlex": true, "shutil": true, "signal": true, "site": true, "smtplib": true,
	"sndhdr": true, "socket": true, "socketserver": true, "sqlite3": true, "ssl": true,
	"stat": true, "statistics": true, "string": true, "stringprep": true, "struct": true,
	"subprocess": true, "sunau": true, "symtable": true, "sys": true, "sysconfig": true,
	"syslog": true, "tabnanny": true, "tarfile": true, "telnetlib": true, "tempfile": true,
	"termios": true, "test": true, "textwrap": true, "threading": true, "time": true,
	"timeit": true, "tkinter": true, "token": true, "tokenize": true, "tomllib": true,
	"trace": true, "traceback": true, "tracemalloc": true, "tty": true, "turtle": true,
	"turtledemo": true, "types": true, "typing": true, "unicodedata": true, "unittest": true,
	"urllib": true, "uu": true, "uuid": true, "venv": true, "warnings": true,
	"wave": true, "weakref": true, "webbrowser": true, "wsgiref": true, "xdrlib": true,
	"xml": true, "xmlrpc": true, "zipapp": true, "zipfile": true, "zipimport": true,
	"zlib": true, "zoneinfo": true, "_thread": true, "__future__": true,
}

// nodeBuiltins never provision via npm.
var nodeBuiltins = map[string]bool{
	"assert": true, "buffer": true, "child_process": true, "cluster": true, "crypto": true,
	"dgram": true, "dns": true, "domain": true, "events": true, "fs": true,
	"http": true, "https": true, "net": true, "os": true, "path": true,
	"punycode": true, "querystring": true, "readline": true, "stream": true, "string_decoder": true,
	"timers": true, "tls": true, "tty": true, "url": true, "util": true,
	"v8": true, "vm": true, "zlib": true, "worker_threads": true, "perf_hooks": true,
	"async_hooks": true, "inspector": true, "module": true, "process": true, "console": true,
}

// rBase packages never provision (shipped with R).
var rBase = map[string]bool{
	"base": true, "stats": true, "utils": true, "methods": true, "datasets": true,
	"graphics": true, "grdevices": true, "grid": true, "splines": true, "tools": true,
	"tcltk": true, "compiler": true, "parallel": true,
}

// commandToApt maps common missing binaries to their apt package.
// Everything else falls back to the binary name itself (which is the
// package name for most Unix tools).
var commandToApt = map[string]string{
	"ps": "procps", "pgrep": "procps", "pkill": "procps", "free": "procps",
	"top": "procps", "uptime": "procps", "w": "procps", "vmstat": "procps", "watch": "procps",
	"ss": "iproute2", "ip": "iproute2", "bridge": "iproute2", "tc": "iproute2",
	"cc": "gcc", "c++": "g++", "g++": "g++", "gcc": "gcc",
	"python": "python-is-python3", "python3": "python3-minimal",
	"R": "r-base", "Rscript": "r-base", "killall": "psmisc",
	"pip": "python3-pip", "pip3": "python3-pip",
	"node": "nodejs", "nodejs": "nodejs", "npm": "npm",
	"r": "r-base", "rscript": "r-base",
	"ruby": "ruby", "gem": "ruby", "bundle": "ruby", "bundler": "ruby",
	"java": "default-jre", "javac": "default-jdk", "jar": "default-jdk",
	"go": "golang-go", "gofmt": "golang-go",
	"rustc": "rustc", "cargo": "cargo",
	"perl": "perl", "cpan": "perl", "cpanm": "cpanminus",
	"php": "php-cli", "composer": "composer",
	"psql": "postgresql-client", "mysql": "mysql-client", "sqlite3": "sqlite3",
	"redis-cli":   "redis-tools",
	"docker":      "docker.io",
	"qemu-mipsel": "qemu-user", "qemu-mips": "qemu-user", "qemu-aarch64": "qemu-user", "qemu-x86_64": "qemu-user",
	"file": "file", "ldd": "libc-bin",
	"jq": "jq", "yq": "yq", "curl": "curl", "wget": "wget",
	"unzip": "unzip", "zip": "zip", "tar": "tar",
	"make": "make", "cmake": "cmake", "ninja": "ninja-build",
	"autoconf": "autoconf", "automake": "automake", "libtool": "libtool", "pkg-config": "pkg-config", "pkgconf": "pkg-config",
	"git": "git",
	"vim": "vim", "nano": "nano",
	"htop": "htop",
	"lsof": "lsof", "strace": "strace", "ltrace": "ltrace",
	"xxd": "xxd", "hexdump": "bsdmainutils",
	"rsync": "rsync", "ssh": "openssh-client", "scp": "openssh-client",
	"pdflatex": "texlive-latex-base", "xelatex": "texlive-xetex",
	"convert": "imagemagick", "ffmpeg": "ffmpeg",
}

// pythonToPip maps common imports to their pip distribution.
var pythonToPip = map[string]string{
	"cv2": "opencv-python-headless", "cv": "opencv-python-headless",
	"pil": "Pillow", "PIL": "Pillow", "pillow": "Pillow",
	"sklearn": "scikit-learn", "skimage": "scikit-image",
	"yaml": "PyYAML", "bs4": "beautifulsoup4",
	"dotenv": "python-dotenv",
	"chess":  "chess",
	"torch":  "torch", "torchvision": "torchvision", "torchaudio": "torchaudio",
	"tensorflow": "tensorflow", "tf": "tensorflow",
	"numpy": "numpy", "np": "numpy",
	"pandas": "pandas", "pd": "pandas",
	"scipy": "scipy", "matplotlib": "matplotlib", "plt": "matplotlib",
	"seaborn": "seaborn", "plotly": "plotly",
	"requests": "requests", "flask": "flask", "django": "django", "fastapi": "fastapi",
	"pytest": "pytest", "coverage": "coverage",
	"click": "click", "typer": "typer", "rich": "rich", "tqdm": "tqdm",
	"lxml": "lxml", "cssselect": "cssselect",
	"sqlalchemy": "sqlalchemy", "pymongo": "pymongo", "redis": "redis",
	"boto3": "boto3", "botocore": "botocore",
	"cryptography": "cryptography", "jwt": "PyJWT",
	"nltk": "nltk", "spacy": "spacy", "transformers": "transformers", "datasets": "datasets",
	"skfuzzy":   "scikit-fuzzy",
	"rdkit":     "rdkit",
	"biopython": "biopython", "Bio": "biopython",
	"fasttext": "fasttext",
	"shap":     "shap", "lightgbm": "lightgbm", "xgboost": "xgboost",
	"networkx": "networkx", "sympy": "sympy",
	"jinja2": "Jinja2", "tornado": "tornado", "celery": "celery",
}

// sharedLibToApt maps common missing .so names to apt packages.
var sharedLibToApt = map[string]string{
	"liblapack.so.3":      "liblapack3",
	"libblas.so.3":        "libblas3",
	"libopenblas.so.0":    "libopenblas0",
	"libcblas.so.3":       "libblas3",
	"libGL.so.1":          "libgl1",
	"libGLU.so.1":         "libglu1-mesa",
	"libgthread-2.0.so.0": "libglib2.0-0",
	"libglib-2.0.so.0":    "libglib2.0-0",
	"libSM.so.6":          "libsm6",
	"libICE.so.6":         "libice6",
	"libX11.so.6":         "libx11-6",
	"libXext.so.6":        "libxext6",
	"libXtst.so.6":        "libxtst6",
	"libgomp.so.1":        "libgomp1",
	"libstdc++.so.6":      "libstdc++6",
	"libz.so.1":           "zlib1g",
	"libssl.so.3":         "libssl3",
	"libcrypto.so.3":      "libssl3",
	"libsqlite3.so.0":     "libsqlite3-0",
	"libcurl.so.4":        "libcurl4",
	"libpng16.so.16":      "libpng16-16",
	"libjpeg.so.8":        "libjpeg-turbo8",
	"libfreetype.so.6":    "libfreetype6",
	"libgfortran.so.5":    "libgfortran5",
	"libquadmath.so.0":    "libquadmath0",
}

// DetectMissing recognizes missing executables/modules/libraries in
// tool outputs across languages. It returns nil when nothing
// actionable is found. Detection is purely textual over recorded
// outputs (general engineering knowledge, never benchmark answers)
// and never fails.
func DetectMissing(command, output string) *MissingDep {
	out := output
	if out == "" {
		return nil
	}
	// Shared libraries first: they also contain "No such file or
	// directory" but need apt, not a path hint.
	if m := reSharedLib.FindStringSubmatch(out); m != nil {
		so := baseSO(m[1])
		return &MissingDep{Kind: KindSharedLib, Name: so, Evidence: firstLineWith(out, m[1])}
	}
	if m := reLibImport.FindStringSubmatch(out); m != nil {
		so := baseSO(libName(m[1]))
		if so != "" {
			return &MissingDep{Kind: KindSharedLib, Name: so, Evidence: firstLineWith(out, m[1])}
		}
	}
	// Python modules.
	if m := rePythonNoModule.FindStringSubmatch(out); m != nil {
		mod := normalizePythonMod(m[1])
		if mod == "" || pythonStdlib[strings.ToLower(mod)] || pythonStdlib[mod] {
			return nil
		}
		// A dotted stdlib root (os.path) is still stdlib.
		if root := strings.Split(mod, ".")[0]; pythonStdlib[strings.ToLower(root)] || pythonStdlib[root] {
			return nil
		}
		top := strings.Split(mod, ".")[0]
		return &MissingDep{Kind: KindPythonMod, Name: top, Evidence: firstLineWith(out, m[0])}
	}
	if m := rePythonCantFind.FindStringSubmatch(out); m != nil {
		mod := normalizePythonMod(m[1])
		if mod == "" || pythonStdlib[strings.ToLower(mod)] {
			return nil
		}
		return &MissingDep{Kind: KindPythonMod, Name: strings.Split(mod, ".")[0], Evidence: firstLineWith(out, m[0])}
	}
	// Node modules (bare specifiers only; relative/absolute paths are
	// local files, builtins never provision).
	if m := reNodeNoModule.FindStringSubmatch(out); m != nil {
		spec := strings.TrimSpace(m[1])
		if isRelativeNodeSpec(spec) || nodeBuiltins[spec] {
			return nil
		}
		if bare := bareNodeSpec(spec); bare != "" && !nodeBuiltins[bare] {
			return &MissingDep{Kind: KindNodeMod, Name: bare, Evidence: firstLineWith(out, m[0])}
		}
		return nil
	}
	// Ruby gems.
	if m := reRubyLoadError.FindStringSubmatch(out); m != nil {
		gem := normalizeRubyGem(m[1])
		if gem != "" {
			return &MissingDep{Kind: KindRubyGem, Name: gem, Evidence: firstLineWith(out, m[0])}
		}
	}
	if m := reRubyLoadErr2.FindStringSubmatch(out); m != nil {
		gem := normalizeRubyGem(m[1])
		if gem != "" {
			return &MissingDep{Kind: KindRubyGem, Name: gem, Evidence: firstLineWith(out, m[0])}
		}
	}
	// R packages.
	if m := reRNoPackage.FindStringSubmatch(out); m != nil {
		pkg := strings.Trim(m[1], "'\"‘’“”()`,;:. ")
		if pkg != "" && !rBase[pkg] {
			return &MissingDep{Kind: KindRPackage, Name: pkg, Evidence: firstLineWith(out, m[0])}
		}
	}
	if m := reRNotAvail.FindStringSubmatch(out); m != nil {
		pkg := strings.Trim(m[1], "'\"‘’“”()`,;:. ")
		if pkg != "" && !rBase[pkg] {
			return &MissingDep{Kind: KindRPackage, Name: pkg, Evidence: firstLineWith(out, m[0])}
		}
	}
	// Perl modules (Foo/Bar.pm -> Foo::Bar).
	if m := rePerlCantLocate.FindStringSubmatch(out); m != nil {
		mod := perlPMToColon(m[1])
		if mod != "" {
			return &MissingDep{Kind: KindPerlMod, Name: mod, Evidence: firstLineWith(out, m[0])}
		}
	}
	// Go packages.
	if m := reGoCannotFind.FindStringSubmatch(out); m != nil {
		pkg := strings.Trim(m[1], "\"'` ")
		if pkg != "" && !isStdGoPkg(pkg) {
			return &MissingDep{Kind: KindGoPackage, Name: pkg, Evidence: firstLineWith(out, m[0])}
		}
	} else if m := reGoNoPackage.FindStringSubmatch(out); m != nil {
		pkg := ""
		for _, g := range m[1:] {
			if g != "" {
				pkg = strings.Trim(g, "\"'` ")
				break
			}
		}
		if pkg != "" && !isStdGoPkg(pkg) {
			return &MissingDep{Kind: KindGoPackage, Name: pkg, Evidence: firstLineWith(out, m[0])}
		}
	}
	// Rust crates.
	if m := reRustNoCrate.FindStringSubmatch(out); m != nil {
		crate := strings.Trim(m[1], "'\"` ")
		if crate != "" {
			return &MissingDep{Kind: KindRustCrate, Name: crate, Evidence: firstLineWith(out, m[0])}
		}
	} else if m := reRustNoMatch.FindStringSubmatch(out); m != nil {
		crate := strings.Trim(m[1], "'\"` ")
		if crate != "" {
			return &MissingDep{Kind: KindRustCrate, Name: crate, Evidence: firstLineWith(out, m[0])}
		}
	} else if m := reRustUnresolved.FindStringSubmatch(out); m != nil {
		crate := strings.Split(m[1], "::")[0]
		crate = strings.Trim(crate, "'\"` ")
		if crate != "" && crate != "crate" && crate != "self" && crate != "super" {
			return &MissingDep{Kind: KindRustCrate, Name: crate, Evidence: firstLineWith(out, m[0])}
		}
	}
	// Java packages.
	if m := reJavaNoPackage.FindStringSubmatch(out); m != nil {
		pkg := strings.TrimSpace(m[1])
		if pkg != "" && !isJavaStdLib(pkg) {
			return &MissingDep{Kind: KindJavaPackage, Name: pkg, Evidence: firstLineWith(out, m[0])}
		}
	}
	// Shell command-not-found last: language errors above already
	// claimed their outputs.
	if dep := detectShellCommand(command, out); dep != nil {
		return dep
	}
	return nil
}

func detectShellCommand(command, out string) *MissingDep {
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "command not found") && !strings.Contains(lower, "not found") {
		return nil
	}
	// Prefer the binary named by the shell diagnostic.
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if m := reShellNotFoundColon.FindStringSubmatch(t); m != nil {
			bin := strings.TrimSpace(m[1])
			bin = strings.Trim(bin, "'\"` ")
			if bin != "" && !strings.Contains(bin, " ") && !strings.Contains(bin, "/") {
				return &MissingDep{Kind: KindCommand, Name: bin, Evidence: t}
			}
		}
		if m := reShNotFound.FindStringSubmatch(t); m != nil {
			bin := strings.TrimSpace(m[1])
			if bin != "" && !strings.Contains(bin, " ") {
				return &MissingDep{Kind: KindCommand, Name: bin, Evidence: t}
			}
		}
	}
	if m := reShellNotFoundBare.FindStringSubmatch(out); m != nil {
		for _, g := range m[1:] {
			bin := strings.TrimSpace(g)
			bin = strings.Trim(bin, "'\"` ")
			if bin != "" && !strings.Contains(bin, " ") && !strings.Contains(bin, "/") {
				return &MissingDep{Kind: KindCommand, Name: bin, Evidence: firstLineWith(out, bin)}
			}
		}
	}

	return nil
}

func normalizePythonMod(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "'\"`")
	if i := strings.Index(s, ";"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "'\"`")
	// "No module named 'foo.bar'" -> keep dotted, caller takes top.
	return s
}

func libName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ":"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, " "); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func baseSO(s string) string {
	s = libName(s)
	if i := strings.Index(s, "/"); i >= 0 {
		if j := strings.LastIndex(s, "/"); j >= 0 {
			s = s[j+1:]
		}
	}
	return s
}

func bareNodeSpec(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "node:") {
		return ""
	}
	if strings.HasPrefix(spec, "@") {
		parts := strings.Split(spec, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + strings.Split(parts[1], "/")[0]
		}
		return spec
	}
	if i := strings.Index(spec, "/"); i >= 0 {
		spec = spec[:i]
	}
	return spec
}

func isRelativeNodeSpec(spec string) bool {
	return strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "node:")
}

func normalizeRubyGem(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "'\"`")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ".rb")
	s = strings.TrimSpace(s)
	return s
}

func perlPMToColon(pm string) string {
	pm = strings.TrimSpace(pm)
	if !strings.HasSuffix(pm, ".pm") {
		return ""
	}
	pm = strings.TrimSuffix(pm, ".pm")
	pm = strings.ReplaceAll(pm, "/", "::")
	return pm
}

func isStdGoPkg(pkg string) bool {
	// Standard library has no dot and no slash (fmt, os, net/http
	// has a slash but is still stdlib: heuristic is no dot).
	if !strings.Contains(pkg, ".") {
		return true
	}
	return false
}

func isJavaStdLib(pkg string) bool {
	return strings.HasPrefix(pkg, "java.") || strings.HasPrefix(pkg, "javax.") || strings.HasPrefix(pkg, "jdk.")
}

func firstLineWith(out, needle string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) || strings.Contains(strings.ToLower(line), strings.ToLower(needle)) {
			s := strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
			if len(s) > 200 {
				s = s[:200]
			}
			return s
		}
	}
	s := strings.Join(strings.Fields(strings.TrimSpace(provFirstLine(out))), " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func provFirstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// LookPathFunc is injectable for tests.
type LookPathFunc func(string) (string, error)

func defaultLookPath(name string) (string, error) { return exec.LookPath(name) }

// PlanInstall maps a missing dependency to install commands for the
// detected package managers. It never returns upgrade/remove
// commands. It returns nil when no safe plan exists (stdlib,
// relative paths, unknown families without a manager).
func PlanInstall(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	if dep == nil {
		return nil
	}
	if lookPath == nil {
		lookPath = defaultLookPath
	}
	switch dep.Kind {
	case KindCommand:
		return planCommand(dep, lookPath)
	case KindPythonMod:
		return planPython(dep, lookPath)
	case KindNodeMod:
		return planNode(dep, lookPath)
	case KindRubyGem:
		return planRuby(dep, lookPath)
	case KindRPackage:
		return planR(dep, lookPath)
	case KindPerlMod:
		return planPerl(dep, lookPath)
	case KindSharedLib:
		return planSharedLib(dep, lookPath)
	case KindGoPackage:
		return planGo(dep, lookPath)
	case KindRustCrate:
		return planRust(dep, lookPath)
	case KindJavaPackage:
		return planJava(dep, lookPath)
	}
	return nil
}

func planCommand(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	bin := dep.Name
	pkg, ok := commandToApt[bin]
	if !ok {
		pkg = bin
	}
	// Prefer apt, then dnf/yum/apk/pacman/brew in detection order.
	if _, err := lookPath("apt-get"); err == nil {
		return aptPlan(bin, pkg, "apt-get", lookPath)
	}
	if _, err := lookPath("apt"); err == nil {
		return aptPlan(bin, pkg, "apt", lookPath)
	}
	// Debian's minimal/alias package names are not portable to other managers.
	if bin == "python" || bin == "python3" {
		pkg = "python3"
	}
	if _, err := lookPath("dnf"); err == nil {
		return &InstallPlan{Manager: "dnf", Package: pkg,
			Commands: []string{sudoPrefix(lookPath) + "dnf install -y " + shellQuote(pkg)},
			Verify:   "command -v " + shellQuote(bin),
			Note:     "dnf install only (never upgrade/remove)"}
	}
	if _, err := lookPath("yum"); err == nil {
		return &InstallPlan{Manager: "yum", Package: pkg,
			Commands: []string{sudoPrefix(lookPath) + "yum install -y " + shellQuote(pkg)},
			Verify:   "command -v " + shellQuote(bin)}
	}
	if _, err := lookPath("apk"); err == nil {
		return &InstallPlan{Manager: "apk", Package: pkg,
			Commands: []string{sudoPrefix(lookPath) + "apk add --no-cache " + shellQuote(pkg)},
			Verify:   "command -v " + shellQuote(bin)}
	}
	if _, err := lookPath("pacman"); err == nil {
		return &InstallPlan{Manager: "pacman", Package: pkg,
			Commands: []string{sudoPrefix(lookPath) + "pacman -Sy --noconfirm " + shellQuote(pkg)},
			Verify:   "command -v " + shellQuote(bin)}
	}
	if _, err := lookPath("brew"); err == nil {
		return &InstallPlan{Manager: "brew", Package: pkg,
			Commands: []string{"brew install " + shellQuote(pkg)},
			Verify:   "command -v " + shellQuote(bin)}
	}
	// No manager detected: still suggest the apt spelling so the
	// model has something concrete when apt appears later.
	return &InstallPlan{Manager: "apt", Package: pkg,
		Commands: []string{"DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=60 install -y --no-install-recommends " + shellQuote(pkg)},
		Verify:   "command -v " + shellQuote(bin),
		Note:     "no package manager detected; apt spelling shown"}
}

// aptPlan builds the apt install plan with non-interactive flags and
// update-once caching. The update command is included only when the
// process has not updated yet (see ShouldAptUpdate).
func aptPlan(bin, pkg, aptBin string, lookPath LookPathFunc) *InstallPlan {
	prefix := sudoPrefix(lookPath)
	var cmds []string
	if ShouldAptUpdate() {
		cmds = append(cmds, prefix+"DEBIAN_FRONTEND=noninteractive "+aptBin+" update -qq")
	}
	cmds = append(cmds, prefix+"DEBIAN_FRONTEND=noninteractive "+aptBin+" -o DPkg::Lock::Timeout=60 install -y --no-install-recommends "+shellQuote(pkg))
	return &InstallPlan{Manager: "apt", Package: pkg, Commands: cmds,
		Verify: "command -v " + shellQuote(bin),
		Note:   "apt install only (never upgrade/remove); non-interactive, update-once"}
}

func planPython(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	dist, ok := pythonToPip[dep.Name]
	if !ok {
		// Case-insensitive fallback for Pillow-style keys.
		for k, v := range pythonToPip {
			if strings.EqualFold(k, dep.Name) {
				dist = v
				ok = true
				break
			}
		}
	}
	if !ok {
		dist = sanitizePipName(dep.Name)
		if dist == "" {
			return nil
		}
	}
	installer, verifyPy := pythonInstaller(lookPath)
	var cmd string
	if strings.HasPrefix(installer, "uv ") {
		// uv pip with the right interpreter when one is present.
		// uv manages its own environment and never touches the
		// system Python, so no PEP 668 flag is needed.
		if verifyPy == "python3" || verifyPy == "python" {
			cmd = installer + " --python " + verifyPy + " " + shellQuote(dist)
		} else {
			cmd = installer + " " + shellQuote(dist)
		}
	} else if strings.Contains(installer, "-m pip") {
		// PEP 668 (Debian/Ubuntu externally-managed-environment):
		// system-wide installs in containers need an explicit
		// opt-out, otherwise pip exits 1 and the agent burns a
		// venv workaround. Still install-only, never upgrade.
		cmd = installer + " install --quiet --break-system-packages " + shellQuote(dist)
	} else {
		cmd = installer + " install --quiet --break-system-packages " + shellQuote(dist)
	}
	verify := verifyPy + " -c " + shellQuote("import "+strings.Split(dep.Name, ".")[0])
	return &InstallPlan{Manager: pipManagerName(installer), Package: dist,
		Commands: []string{cmd}, Verify: verify,
		Note: "pip install only (never upgrade/remove existing packages)"}
}

func planNode(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	pkg := dep.Name
	if pkg == "" {
		return nil
	}
	mgr := "npm"
	cmd := "npm install " + shellQuote(pkg)
	if _, err := lookPath("npm"); err != nil {
		if _, err2 := lookPath("yarn"); err2 == nil {
			mgr = "yarn"
			cmd = "yarn add " + shellQuote(pkg)
		} else if _, err3 := lookPath("pnpm"); err3 == nil {
			mgr = "pnpm"
			cmd = "pnpm add " + shellQuote(pkg)
		}
	}
	return &InstallPlan{Manager: mgr, Package: pkg, Commands: []string{cmd},
		Verify: "npm ls " + shellQuote(pkg),
		Note:   "local install in the task directory (never global upgrade)"}
}

func planRuby(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	gem := dep.Name
	return &InstallPlan{Manager: "gem", Package: gem,
		Commands: []string{"gem install --no-document " + shellQuote(gem)},
		Verify:   "ruby -e " + shellQuote("require '"+gem+"'")}
}

func planR(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	pkg := dep.Name
	expr := "install.packages('" + rEscape(pkg) + "', repos='https://cloud.r-project.org')"
	var cmd string
	if _, err := lookPath("Rscript"); err == nil {
		cmd = "Rscript -e " + shellQuote(expr)
	} else {
		cmd = "R -e " + shellQuote(expr)
	}
	return &InstallPlan{Manager: "R", Package: pkg, Commands: []string{cmd},
		Verify: "Rscript -e " + shellQuote("library("+pkg+")")}
}

func planPerl(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	mod := dep.Name
	if _, err := lookPath("cpanm"); err == nil {
		return &InstallPlan{Manager: "cpanm", Package: mod,
			Commands: []string{"cpanm --notest " + shellQuote(mod)},
			Verify:   "perl -M" + shellQuote(mod) + " -e 1"}
	}
	return &InstallPlan{Manager: "cpan", Package: mod,
		Commands: []string{"cpan -T " + shellQuote(mod)},
		Verify:   "perl -M" + shellQuote(mod) + " -e 1"}
}

func planSharedLib(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	so := dep.Name
	pkg, ok := sharedLibToApt[so]
	if !ok {
		// Heuristic: libfoo.so.N -> libfooN (covers many -dev
		// adjacent runtime libs); still install-only.
		pkg = soToPkgHeuristic(so)
	}
	if _, err := lookPath("apt-get"); err == nil {
		return aptPlan(so, pkg, "apt-get", lookPath)
	}
	if _, err := lookPath("apt"); err == nil {
		return aptPlan(so, pkg, "apt", lookPath)
	}
	// Without apt there is no safe default for .so files: report the
	// heuristic without running it.
	return &InstallPlan{Manager: "apt", Package: pkg,
		Commands: []string{"DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=60 install -y --no-install-recommends " + shellQuote(pkg)},
		Verify:   "ldconfig -p | grep " + shellQuote(so),
		Note:     "no apt detected; spelling shown for when apt is available"}
}

func planGo(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	return &InstallPlan{Manager: "go", Package: dep.Name,
		Commands: []string{"go get " + shellQuote(dep.Name)},
		Verify:   "go list " + shellQuote(dep.Name)}
}

func planRust(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	return &InstallPlan{Manager: "cargo", Package: dep.Name,
		Commands: []string{"cargo add " + shellQuote(dep.Name)},
		Verify:   "cargo metadata --format-version 1 | grep " + shellQuote(dep.Name)}
}

func planJava(dep *MissingDep, lookPath LookPathFunc) *InstallPlan {
	// No safe automatic install for arbitrary Maven/Gradle
	// coordinates: point at the build file instead.
	return &InstallPlan{Manager: "maven", Package: dep.Name,
		Commands: []string{"# add " + dep.Name + " to pom.xml/build.gradle, then: mvn -q compile"},
		Verify:   "mvn -q compile",
		Note:     "suggest-only: Java coordinates need a build-file edit"}
}

// pythonInstaller picks pip/uv with the right interpreter.
func pythonInstaller(lookPath LookPathFunc) (installer, verifyPy string) {
	if _, err := lookPath("python3"); err == nil {
		if _, err2 := lookPath("uv"); err2 == nil {
			return "uv pip install --quiet", "python3"
		}
		return "python3 -m pip", "python3"
	}
	if _, err := lookPath("python"); err == nil {
		if _, err2 := lookPath("uv"); err2 == nil {
			return "uv pip install --quiet", "python"
		}
		return "python -m pip", "python"
	}
	if _, err := lookPath("pip3"); err == nil {
		return "pip3", "python3"
	}
	if _, err := lookPath("pip"); err == nil {
		return "pip", "python"
	}
	if _, err := lookPath("uv"); err == nil {
		return "uv pip install --quiet", "python3"
	}
	return "pip", "python3"
}

func pipManagerName(installer string) string {
	if strings.HasPrefix(installer, "uv ") {
		return "uv"
	}
	return "pip"
}

func sanitizePipName(mod string) string {
	top := strings.Split(mod, ".")[0]
	top = strings.Split(top, "/")[0]
	top = strings.Split(top, ":")[0]
	top = strings.TrimSpace(top)
	if top == "" {
		return ""
	}
	// Reject paths, flags, shell metacharacters: pip names are
	// alnum plus - _ . only.
	for _, r := range top {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ""
	}
	if len(top) > 100 {
		return ""
	}
	return top
}

func soToPkgHeuristic(so string) string {
	base := so
	if i := strings.Index(base, ".so"); i >= 0 {
		stem := base[:i]
		rest := base[i+len(".so"):]
		rest = strings.Trim(rest, ".")
		digits := ""
		for _, r := range rest {
			if r >= '0' && r <= '9' {
				digits += string(r)
			} else if r == '.' {
				continue
			} else {
				break
			}
		}
		stem = strings.TrimPrefix(stem, "lib")
		if digits != "" {
			return "lib" + stem + digits
		}
		return "lib" + stem
	}
	return base
}

func rEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	simple := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.' || r == '/' {
			continue
		}
		simple = false
		break
	}
	if simple {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func sudoPrefix(lookPath LookPathFunc) string {
	if os.Geteuid() == 0 {
		return ""
	}
	if _, err := lookPath("sudo"); err == nil {
		return "sudo -n "
	}
	return ""
}

// apt update-once caching (process-wide): the first apt plan in the
// process carries the update, later plans skip it so repeated missing
// tools do not pay for repeated updates.
var (
	aptMu      sync.Mutex
	aptUpdated = false
)

// ShouldAptUpdate reports whether the caller should include the apt
// update command (first apt plan only). Tests reset via
// ResetAptUpdateCache.
func ShouldAptUpdate() bool {
	aptMu.Lock()
	defer aptMu.Unlock()
	return !aptUpdated
}

// MarkAptUpdated records a successfully completed apt update.
func MarkAptUpdated() {
	aptMu.Lock()
	defer aptMu.Unlock()
	aptUpdated = true
}

// ResetAptUpdateCache clears the update-once flag (tests only).
func ResetAptUpdateCache() {
	aptMu.Lock()
	defer aptMu.Unlock()
	aptUpdated = false
}

// Provisioner executes InstallPlans under the policy switch.
type Provisioner struct {
	Mode ProvisionMode
	// LookPath detects managers (nil means exec.LookPath).
	LookPath LookPathFunc
	// Run executes one shell command (nil means the real runner).
	// It returns combined output and exit code.
	Run func(ctx context.Context, command, workdir string) (output string, exit int)
	// Network optionally overrides connectivity policy in tests; nil lets the package manager try.
	Network func(ctx context.Context) bool
	// Timeout bounds one install command (default 180s).
	Timeout time.Duration
}

func (p *Provisioner) mode() ProvisionMode {
	if p == nil {
		return ProvisionModeFromEnv()
	}
	if p.Mode == "" {
		return ProvisionModeFromEnv()
	}
	return p.Mode
}

func (p *Provisioner) lookPath() LookPathFunc {
	if p != nil && p.LookPath != nil {
		return p.LookPath
	}
	return defaultLookPath
}

func (p *Provisioner) timeout() time.Duration {
	if p != nil && p.Timeout > 0 {
		return p.Timeout
	}
	return 180 * time.Second
}

func (p *Provisioner) networkOK(ctx context.Context) bool {
	if p != nil && p.Network != nil {
		return p.Network(ctx)
	}
	return defaultNetworkAvailable(ctx)
}

// defaultNetworkAvailable lets the package manager report connectivity errors.
func defaultNetworkAvailable(ctx context.Context) bool {
	// Package managers are the authority on mirror connectivity; DNS TCP
	// reachability does not establish whether HTTP proxies/mirrors work.
	return ctx.Err() == nil
}

// defaultRunProvisionCommand runs one shell command with the
// non-interactive tool environment and a bounded timeout.
func defaultRunProvisionCommand(ctx context.Context, command, workdir string, timeout time.Duration) (string, int) {
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	shell := DetectShell()
	c := exec.CommandContext(cctx, shell, "-c", command)
	if workdir != "" {
		c.Dir = workdir
	}
	c.Env = pipeEnv(nil)
	out, err := c.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		return string(out) + "\nprovision runner error: " + err.Error(), 1
	}
	return string(out), 0
}

// MaybeProvision detects a missing dependency in the exec output and
// applies the policy. It returns the report lines to append to the
// tool result ("" when disabled or nothing detected). In auto mode it
// runs the install when network is available; in suggest mode (or
// when offline) it only reports the commands. It never emits
// upgrade/remove commands.
func (p *Provisioner) MaybeProvision(ctx context.Context, command, output, workdir string) string {
	var report strings.Builder
	seen := map[string]bool{}
	// Scan each diagnostic independently so one missing dependency cannot mask
	// another in a compound command or a session buffer.
	for _, line := range strings.Split(output, "\n") {
		dep := DetectMissing(command, line)
		if dep == nil {
			continue
		}
		key := string(dep.Kind) + ":" + dep.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		report.WriteString(p.provisionOne(ctx, command, line, workdir))
	}
	return report.String()
}

var provisionMu sync.Mutex

func (p *Provisioner) provisionOne(ctx context.Context, command, output, workdir string) string {
	// Serialize install/verify sequences, including apt cache transitions.
	provisionMu.Lock()
	defer provisionMu.Unlock()
	mode := p.mode()
	if mode == ProvisionOff {
		return ""
	}
	dep := DetectMissing(command, output)
	if dep == nil {
		return ""
	}
	plan := PlanInstall(dep, p.lookPath())
	if dep.Kind == KindPythonMod && plan != nil {
		if py := provisionPythonInterpreter(command, output); py != "" {
			dist := shellQuote(plan.Package)
			plan.Verify = shellQuote(py) + " -c " + shellQuote("import "+dep.Name)
			if _, err := p.lookPath()("uv"); err == nil {
				plan.Commands = []string{"uv pip install --quiet --break-system-packages --python " + shellQuote(py) + " " + dist}
			} else {
				plan.Commands = []string{shellQuote(py) + " -m pip install --quiet --break-system-packages " + dist}
			}
		}
	}
	if plan == nil {
		return ""
	}
	// Java coordinates are suggest-only: installing them needs a
	// build-file edit, not a shell install.
	suggestOnly := dep.Kind == KindJavaPackage
	if mode == ProvisionSuggest || suggestOnly {
		return suggestReport(dep, plan, modeReason(mode, p, ctx))
	}
	// Auto mode: check network before spending minutes on an install
	// that cannot succeed.
	if !p.networkOK(ctx) {
		return suggestReport(dep, plan, "network unavailable (auto-provision needs network; ran nothing)")
	}
	run := p.Run
	if run == nil {
		timeout := p.timeout()
		run = func(c context.Context, cmd, dir string) (string, int) {
			return defaultRunProvisionCommand(c, cmd, dir, timeout)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "provision: detected missing %s '%s' (evidence: %s).\n", dep.Kind, dep.Name, dep.Evidence)
	fmt.Fprintf(&b, "provision: plan: %s installs %s via: %s (verify: %s).\n", plan.Manager, plan.Package, strings.Join(plan.Commands, " && "), plan.Verify)
	for _, cmd := range plan.Commands {
		if isForbiddenProvisionCommand(cmd) {
			fmt.Fprintf(&b, "provision: refused to run %q (never upgrade/remove).\n", cmd)
			return b.String()
		}
		isUpdate := strings.Contains(cmd, "apt-get update") || strings.Contains(cmd, "apt update")
		start := time.Now()
		out, code := run(ctx, cmd, workdirForPlan(plan, workdir))
		// Minimal system interpreters often lack pip. Bootstrap pip for the
		// same interpreter, then retry the original install once.
		if code != 0 && dep.Kind == KindPythonMod && strings.Contains(out, "No module named pip") {
			py := provisionPythonInterpreter(command, output)
			if py == "" {
				_, py = pythonInstaller(p.lookPath())
			}
			bootstrapOut, bootstrapCode := run(ctx, shellQuote(py)+" -m ensurepip", workdir)
			if bootstrapCode != 0 {
				if _, err := p.lookPath()("apt-get"); err == nil {
					pipPlan := aptPlan("pip3", "python3-pip", "apt-get", p.lookPath())
					for _, bootstrap := range pipPlan.Commands {
						bootstrapOut, bootstrapCode = run(ctx, bootstrap, workdir)
						if bootstrapCode != 0 {
							break
						}
						if strings.Contains(bootstrap, " update ") {
							MarkAptUpdated()
						}
					}
				}
			}
			fmt.Fprintf(&b, "provision: pip bootstrap exit %d: %s\n", bootstrapCode, clipProvisionOutput(bootstrapOut))
			if bootstrapCode == 0 {
				out, code = run(ctx, cmd, workdir)
			}
		}
		dur := time.Since(start)
		clipped := clipProvisionOutput(out)
		if code != 0 {
			if isUpdate {
				fmt.Fprintf(&b, "provision: apt update failed (exit %d, %.1fs): %s\n", code, dur.Seconds(), clipped)
			} else {
				fmt.Fprintf(&b, "provision: install failed (exit %d, %.1fs): %s\n", code, dur.Seconds(), clipped)
			}
			fmt.Fprintf(&b, "provision: try manually: %s then verify with `%s` and re-run your command.\n", cmd, plan.Verify)
			return b.String()
		}
		if isUpdate {
			MarkAptUpdated()
			fmt.Fprintf(&b, "provision: apt update ok (exit 0, %.1fs). output tail: %s\n", dur.Seconds(), clipped)
		} else {
			fmt.Fprintf(&b, "provision: installed %s via %s (exit 0, %.1fs). output tail: %s\n", plan.Package, plan.Manager, dur.Seconds(), clipped)
		}
	}
	verified, code := run(ctx, plan.Verify, workdirForPlan(plan, workdir))
	if code != 0 {
		fmt.Fprintf(&b, "provision: verification failed (exit %d): %s; command: %s\n", code, clipProvisionOutput(verified), plan.Verify)
		return b.String()
	}
	fmt.Fprintf(&b, "provision: verified working command: `%s`. re-run your original command explicitly:\n%s\n", plan.Verify, command)
	return b.String()
}

// MarkAptUpdatedIfApt records the update-once flag when the plan
// carries an apt update, so later plans skip it.
func MarkAptUpdatedIfApt(plan *InstallPlan) {
	if plan == nil || plan.Manager != "apt" {
		return
	}
	for _, c := range plan.Commands {
		if strings.Contains(c, "apt-get update") || strings.Contains(c, "apt update") {
			MarkAptUpdated()
			return
		}
	}
}

func modeReason(mode ProvisionMode, p *Provisioner, ctx context.Context) string {
	if mode == ProvisionSuggest {
		return "auto-install is off (" + string(AutoProvisionEnv) + "=suggest; ran nothing)"
	}
	return "ran nothing"
}

func suggestReport(dep *MissingDep, plan *InstallPlan, reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "provision: detected missing %s '%s' (evidence: %s).\n", dep.Kind, dep.Name, dep.Evidence)
	fmt.Fprintf(&b, "provision: suggest: run `%s` then verify with `%s` and re-run your command. %s.\n", strings.Join(plan.Commands, " && "), plan.Verify, reason)
	if plan.Note != "" {
		fmt.Fprintf(&b, "provision: note: %s.\n", plan.Note)
	}
	return b.String()
}

func workdirForPlan(plan *InstallPlan, workdir string) string {
	// npm/cargo/go installs are project-local; run them in the task
	// directory when the model supplied one.
	switch plan.Manager {
	case "npm", "yarn", "pnpm", "cargo", "go", "pip", "uv":
		return workdir
	}
	return ""
}

func clipProvisionOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "(no output)"
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	s := strings.Join(lines, " | ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}

// isForbiddenProvisionCommand guarantees the never-upgrade/remove
// policy: only install/add commands may run.
func isForbiddenProvisionCommand(cmd string) bool {
	l := strings.ToLower(" " + cmd + " ")
	for _, s := range []string{
		" upgrade ", " dist-upgrade ", " full-upgrade ",
		" remove ", " purge ", " autoremove ",
		" uninstall ", " --upgrade ",
	} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// provisionPythonInterpreter prefers interpreter diagnostics, then the
// interpreter token in the original command, including compound/heredoc forms.
func provisionPythonInterpreter(command, output string) string {
	re := regexp.MustCompile(`(?m)(?:^|[\s;&|])((?:/[A-Za-z0-9_.+/-]+/|\.?/?[A-Za-z0-9_.+/-]*/)?python(?:[0-9]+(?:\.[0-9]+)?)?)(?:[\s:]|$)`)
	for _, text := range []string{output, command} {
		if m := re.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	}
	return ""
}
