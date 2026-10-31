package codeintel

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Node kinds of the code and document graph (Section 10.10): files,
// modules, symbols (functions, methods, types, tests), documents and
// their sections.
const (
	NodeFile        = "file"
	NodeModule      = "module"
	NodeSymbol      = "symbol"
	NodeDocument    = "document"
	NodeSection     = "section"
	NodeRequirement = "requirement"
)

// Symbol sub-kinds.
const (
	SymbolFunc   = "function"
	SymbolMethod = "method"
	SymbolType   = "type"
	SymbolTest   = "test"
	SymbolOther  = "other"
)

// Edge kinds (Section 10.10): import, call, define, reference,
// test-to-code and document-to-code.
const (
	EdgeDefines     = "defines"
	EdgeImports     = "imports"
	EdgeCalls       = "calls"
	EdgeReferences  = "references"
	EdgeTests       = "tests"
	EdgeCites       = "cites"
	EdgeImplements  = "implements"
	EdgeRequirement = "requirement"
)

// Node is one vertex of the graph. IDs are deterministic so a rebuild
// from the same tree produces the same graph:
//
//	file:<relpath>                      a source or document file
//	mod:<importpath>                    a module (Go import path et al.)
//	sym:<relpath>:<name>#<line>         a symbol definition
//	sec:<relpath>:<slug>#<line>         a document section
//
// Line is 1-based; EndLine is inclusive. Hash carries the file sha256
// for file nodes and is empty otherwise.
type Node struct {
	ID      string
	Kind    string
	SubKind string
	Path    string
	Name    string
	Line    int
	EndLine int
	Hash    string
}

// Edge is one directed relation between node IDs.
type Edge struct {
	Src  string
	Kind string
	Dst  string
}

// FileID returns the deterministic node ID of a project-relative file.
func FileID(rel string) string { return "file:" + rel }

// SymbolID returns the deterministic node ID of a symbol definition.
func SymbolID(rel, name string, line int) string {
	return fmt.Sprintf("sym:%s:%s#%d", rel, name, line)
}

// SectionID returns the deterministic node ID of a document section.
func SectionID(rel, slug string, line int) string {
	return fmt.Sprintf("sec:%s:%s#%d", rel, slug, line)
}

// ModuleID returns the deterministic node ID of a module.
func ModuleID(importPath string) string { return "mod:" + importPath }

// Schema version of codegraph.db. The graph is rebuildable by design
// (Section 4.2), so upgrades wipe and reindex rather than migrate.
const graphSchemaVersion = 1

const graphSchema = `
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS files(
  path TEXT PRIMARY KEY,
  lang TEXT NOT NULL DEFAULT '',
  hash TEXT NOT NULL DEFAULT '',
  indexed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS nodes(
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  subkind TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  line INTEGER NOT NULL DEFAULT 0,
  end_line INTEGER NOT NULL DEFAULT 0,
  hash TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_nodes_path ON nodes(path);
CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind, subkind);
CREATE TABLE IF NOT EXISTS edges(
  src TEXT NOT NULL,
  kind TEXT NOT NULL,
  dst TEXT NOT NULL,
  PRIMARY KEY(src, kind, dst)
);
CREATE INDEX IF NOT EXISTS idx_edges_dst ON edges(dst);
-- Name uses behind the dynamic edges (calls, references, tests,
-- cites). Static edges (defines, imports, implements, requirement)
-- live in edges; dynamic ones resolve live from this table plus the
-- project name index, so incremental updates never leave stale
-- cross-file links.
CREATE TABLE IF NOT EXISTS nameuses(
  holder TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  PRIMARY KEY(holder, kind, name)
);
CREATE INDEX IF NOT EXISTS idx_nameuses_name ON nameuses(name);
`

// Name-use kinds behind the dynamic edges.
const (
	UseCalls      = "calls"
	UseReferences = "references"
	UseCites      = "cites"
)

// NameUse records that holder (a symbol or section node ID) mentions
// the bare name in a calls, references or cites position.
type NameUse struct {
	Holder string
	Kind   string
	Name   string
}

// dynamicKinds are resolved live from nameuses, never stored in edges.
var dynamicKinds = map[string]bool{
	EdgeCalls: true, EdgeReferences: true, EdgeTests: true, EdgeCites: true,
}

// staticKinds persist in the edges table.
var staticKinds = map[string]bool{
	EdgeDefines: true, EdgeImports: true, EdgeImplements: true, EdgeRequirement: true,
}

// Graph is the code and document graph of one project (Section 10.10).
// The in-memory index is authoritative for reads; every mutation also
// lands in codegraph.db in a short mutex-guarded transaction, so a
// restart reloads the same state and crash recovery only reindexes
// files whose hash changed. Reads never touch SQL.
type Graph struct {
	mu sync.RWMutex

	db   *sql.DB
	path string

	files map[string]fileRow
	nodes map[string]Node
	// out maps src -> static edges; back maps dst -> static edges.
	// Dynamic edges (calls, references, tests, cites) resolve live
	// from uses via OutEdges/InEdges.
	out  map[string][]Edge
	back map[string][]Edge
	// symByName indexes symbol nodes by bare name for call resolution.
	symByName map[string][]string
	// uses maps holder node ID -> its name uses; useHolders maps a
	// bare name -> holder node IDs mentioning it.
	uses       map[string][]NameUse
	useHolders map[string]map[string]bool
}

// addUseLocked indexes one name use (caller holds the lock).
func (g *Graph) addUseLocked(u NameUse) {
	g.uses[u.Holder] = append(g.uses[u.Holder], u)
	if g.useHolders[u.Name] == nil {
		g.useHolders[u.Name] = map[string]bool{}
	}
	g.useHolders[u.Name][u.Holder] = true
}

// removeHolderUsesLocked drops every use of holder (caller holds the lock).
func (g *Graph) removeHolderUsesLocked(holder string) {
	for _, u := range g.uses[holder] {
		if holders := g.useHolders[u.Name]; holders != nil {
			delete(holders, holder)
			if len(holders) == 0 {
				delete(g.useHolders, u.Name)
			}
		}
	}
	delete(g.uses, holder)
}

type fileRow struct {
	lang      string
	hash      string
	indexedAt int64
}

// OpenGraph opens (creating when missing) the project graph database at
// dbPath and loads it into memory. dbPath is usually
// Layout.CodeGraphPath(projectID): projects/<id>/codegraph.db.
func OpenGraph(dbPath string) (*Graph, error) {
	db, err := sql.Open(graphDriverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("codeintel: open graph db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("codeintel: graph db wal: %w", err)
	}
	if _, err := db.Exec(`PRAGMA synchronous=NORMAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("codeintel: graph db sync: %w", err)
	}
	if _, err := db.Exec(graphSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("codeintel: graph db schema: %w", err)
	}
	var version int
	err = db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	switch {
	case err == sql.ErrNoRows:
		if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES(?)`, graphSchemaVersion); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("codeintel: stamp graph schema: %w", err)
		}
	case err != nil:
		_ = db.Close()
		return nil, fmt.Errorf("codeintel: read graph schema: %w", err)
	case version != graphSchemaVersion:
		if err := wipeGraph(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	g := &Graph{
		db: db, path: dbPath,
		files:      map[string]fileRow{},
		nodes:      map[string]Node{},
		out:        map[string][]Edge{},
		back:       map[string][]Edge{},
		symByName:  map[string][]string{},
		uses:       map[string][]NameUse{},
		useHolders: map[string]map[string]bool{},
	}
	if err := g.load(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return g, nil
}

func wipeGraph(db *sql.DB) error {
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS files`, `DROP TABLE IF EXISTS nodes`,
		`DROP TABLE IF EXISTS edges`, `DROP TABLE IF EXISTS schema_version`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("codeintel: wipe graph db: %w", err)
		}
	}
	if _, err := db.Exec(graphSchema); err != nil {
		return fmt.Errorf("codeintel: recreate graph schema: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES(?)`, graphSchemaVersion); err != nil {
		return fmt.Errorf("codeintel: stamp graph schema: %w", err)
	}
	return nil
}

func (g *Graph) load() error {
	rows, err := g.db.Query(`SELECT path, lang, hash, indexed_at FROM files`)
	if err != nil {
		return fmt.Errorf("codeintel: load files: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p, lang, hash string
		var at int64
		if err := rows.Scan(&p, &lang, &hash, &at); err != nil {
			return fmt.Errorf("codeintel: scan file: %w", err)
		}
		g.files[p] = fileRow{lang: lang, hash: hash, indexedAt: at}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("codeintel: files rows: %w", err)
	}
	nrows, err := g.db.Query(`SELECT id, kind, subkind, path, name, line, end_line, hash FROM nodes`)
	if err != nil {
		return fmt.Errorf("codeintel: load nodes: %w", err)
	}
	defer nrows.Close()
	for nrows.Next() {
		var n Node
		if err := nrows.Scan(&n.ID, &n.Kind, &n.SubKind, &n.Path, &n.Name, &n.Line, &n.EndLine, &n.Hash); err != nil {
			return fmt.Errorf("codeintel: scan node: %w", err)
		}
		g.nodes[n.ID] = n
		if n.Kind == NodeSymbol && n.Name != "" {
			g.symByName[n.Name] = append(g.symByName[n.Name], n.ID)
		}
	}
	if err := nrows.Err(); err != nil {
		return fmt.Errorf("codeintel: nodes rows: %w", err)
	}
	erows, err := g.db.Query(`SELECT src, kind, dst FROM edges`)
	if err != nil {
		return fmt.Errorf("codeintel: load edges: %w", err)
	}
	defer erows.Close()
	for erows.Next() {
		var e Edge
		if err := erows.Scan(&e.Src, &e.Kind, &e.Dst); err != nil {
			return fmt.Errorf("codeintel: scan edge: %w", err)
		}
		if dynamicKinds[e.Kind] {
			continue // legacy rows: dynamic edges resolve live now
		}
		g.out[e.Src] = append(g.out[e.Src], e)
		g.back[e.Dst] = append(g.back[e.Dst], e)
	}
	if err := erows.Err(); err != nil {
		return fmt.Errorf("codeintel: edges rows: %w", err)
	}
	urows, err := g.db.Query(`SELECT holder, kind, name FROM nameuses`)
	if err != nil {
		return fmt.Errorf("codeintel: load uses: %w", err)
	}
	defer urows.Close()
	for urows.Next() {
		var u NameUse
		if err := urows.Scan(&u.Holder, &u.Kind, &u.Name); err != nil {
			return fmt.Errorf("codeintel: scan use: %w", err)
		}
		g.addUseLocked(u)
	}
	return urows.Err()
}

// Close releases the graph database handle.
func (g *Graph) Close() error {
	if g.db == nil {
		return nil
	}
	return g.db.Close()
}

// DBPath returns the backing database path.
func (g *Graph) DBPath() string { return g.path }

// FileHash returns the stored hash of an indexed file, or "" when the
// file is not indexed. It drives incremental reindexing: files whose
// content hash did not change are skipped (Section 10.10).
func (g *Graph) FileHash(rel string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.files[rel].hash
}

// IndexedFiles returns the indexed project-relative paths, sorted.
func (g *Graph) IndexedFiles() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, 0, len(g.files))
	for p := range g.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Node returns the node with ID, if present.
func (g *Graph) Node(id string) (Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

// SymbolsNamed returns symbol node IDs sharing a bare name, sorted.
// Over-approximation is deliberate: call resolution links to every
// same-named definition and test selection stays conservative.
func (g *Graph) SymbolsNamed(name string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := append([]string(nil), g.symByName[name]...)
	sort.Strings(out)
	return out
}

// FileSymbols returns the symbol nodes defined in a file, ordered by line.
func (g *Graph) FileSymbols(rel string) []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []Node
	for _, e := range g.out[FileID(rel)] {
		if e.Kind != EdgeDefines {
			continue
		}
		if n, ok := g.nodes[e.Dst]; ok {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// OutEdges returns the outgoing edges of a node, ordered by kind+dst.
// Static edges come from the store; dynamic edges (calls, references,
// tests, cites) resolve live from the holder's name uses against the
// project name index.
func (g *Graph) OutEdges(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.outEdgesLocked(id)
}

func (g *Graph) outEdgesLocked(id string) []Edge {
	out := append([]Edge(nil), g.out[id]...)
	out = append(out, g.resolveUsesLocked(id)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Dst < out[j].Dst
	})
	return out
}

// InEdges returns the incoming edges of a node, ordered by kind+src.
// For named nodes (symbols) this includes the dynamic inbound uses of
// every holder mentioning the node's name.
func (g *Graph) InEdges(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.inEdgesLocked(id)
}

func (g *Graph) inEdgesLocked(id string) []Edge {
	out := append([]Edge(nil), g.back[id]...)
	n, ok := g.nodes[id]
	if ok && n.Name != "" {
		for holder := range g.useHolders[n.Name] {
			hn, ok := g.nodes[holder]
			if !ok {
				continue
			}
			for _, u := range g.uses[holder] {
				if u.Name != n.Name {
					continue
				}
				kind := u.Kind
				switch kind {
				case UseCalls:
					kind = EdgeCalls
				case UseReferences:
					kind = EdgeReferences
				case UseCites:
					kind = EdgeCites
				default:
					continue
				}
				out = append(out, Edge{Src: holder, Kind: kind, Dst: id})
				if isTestNode(hn) && (kind == EdgeCalls || kind == EdgeReferences) {
					if !isTestNode(n) {
						out = append(out, Edge{Src: holder, Kind: EdgeTests, Dst: id})
					}
				}
			}
		}
	}
	dedupe := map[Edge]bool{}
	uniq := out[:0]
	for _, e := range out {
		if dedupe[e] {
			continue
		}
		dedupe[e] = true
		uniq = append(uniq, e)
	}
	out = uniq
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Src < out[j].Src
	})
	return out
}

// resolveUsesLocked expands the name uses of holder into dynamic
// edges. Test holders additionally link to non-test targets with
// tests edges (Section 10.10: test-to-code). Caller holds the lock.
func (g *Graph) resolveUsesLocked(holder string) []Edge {
	hn, ok := g.nodes[holder]
	if !ok {
		return nil
	}
	seen := map[Edge]bool{}
	var out []Edge
	add := func(kind, dst string) {
		e := Edge{Src: holder, Kind: kind, Dst: dst}
		if seen[e] || dst == holder {
			return
		}
		seen[e] = true
		out = append(out, e)
	}
	for _, u := range g.uses[holder] {
		kind := ""
		switch u.Kind {
		case UseCalls:
			kind = EdgeCalls
		case UseReferences:
			kind = EdgeReferences
		case UseCites:
			kind = EdgeCites
		default:
			continue
		}
		for _, dst := range g.symByName[u.Name] {
			target, ok := g.nodes[dst]
			if !ok {
				continue
			}
			add(kind, dst)
			if isTestNode(hn) && !isTestNode(target) &&
				(kind == EdgeCalls || kind == EdgeReferences) {
				add(EdgeTests, dst)
			}
		}
	}
	return out
}

func isTestNode(n Node) bool {
	return n.Kind == NodeSymbol && n.SubKind == SymbolTest
}

// Stats reports node and edge counts by kind.
func (g *Graph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	st := GraphStats{Files: len(g.files)}
	for _, n := range g.nodes {
		switch n.Kind {
		case NodeFile:
			st.FileNodes++
		case NodeModule:
			st.Modules++
		case NodeSymbol:
			st.Symbols++
			if n.SubKind == SymbolTest {
				st.TestSymbols++
			}
		case NodeDocument:
			st.Documents++
		case NodeSection:
			st.Sections++
		}
	}
	for _, edges := range g.out {
		st.Edges += len(edges)
	}
	return st
}

// GraphStats aggregates graph sizes for the code_graph stats action.
type GraphStats struct {
	Files       int
	FileNodes   int
	Modules     int
	Symbols     int
	TestSymbols int
	Documents   int
	Sections    int
	Edges       int
}

// FileUpdate is the parsed view of one file handed to UpsertFile.
type FileUpdate struct {
	// Rel is the project-relative slash path.
	Rel string
	// Lang is the detected language ("go", "python", ... or "doc").
	Lang string
	// Hash is the content sha256 hex; stored for incremental updates.
	Hash string
	// Nodes and Edges replace everything previously known for Rel.
	// Only static edge kinds (defines, imports, implements,
	// requirement) belong in Edges; dynamic ones resolve from Uses.
	Nodes []Node
	Edges []Edge
	// Uses replaces the name uses of every holder node of Rel.
	Uses []NameUse
}

// UpsertFile replaces the graph contribution of one file and persists
// it in a single short transaction. Nodes and edges referencing other
// files are kept as-is: only rows whose node belongs to Rel (file
// node, its defined symbols/sections) are replaced; edges with Src in
// the replaced set are deleted and re-added. Edges from other files
// pointing at removed symbols are pruned.
func (g *Graph) UpsertFile(upd FileUpdate) error {
	now := time.Now().Unix()
	g.mu.Lock()
	defer g.mu.Unlock()

	fileID := FileID(upd.Rel)
	keep := map[string]bool{fileID: true}
	var fileNode *Node
	var rest []Node
	for i, n := range upd.Nodes {
		if n.ID == fileID {
			fileNode = &upd.Nodes[i]
			continue
		}
		rest = append(rest, n)
	}
	if fileNode == nil {
		fileNode = &Node{ID: fileID, Kind: NodeFile, Path: upd.Rel, Hash: upd.Hash}
	} else if fileNode.Hash == "" {
		fileNode.Hash = upd.Hash
	}
	all := append([]Node{*fileNode}, rest...)
	for _, n := range all {
		keep[n.ID] = true
	}

	// Drop replaced nodes from memory.
	if old := g.fileNodeIDs(upd.Rel); len(old) > 0 {
		for _, id := range old {
			if keep[id] {
				continue
			}
			delete(g.nodes, id)
		}
		// Rebuild the name index entries pointing at dropped nodes.
		for name, ids := range g.symByName {
			kept := ids[:0]
			for _, id := range ids {
				if _, ok := g.nodes[id]; ok {
					kept = append(kept, id)
				}
			}
			if len(kept) == 0 {
				delete(g.symByName, name)
			} else {
				g.symByName[name] = kept
			}
		}
	}
	for _, n := range all {
		g.nodes[n.ID] = n
		if n.Kind == NodeSymbol && n.Name != "" {
			found := false
			for _, id := range g.symByName[n.Name] {
				if id == n.ID {
					found = true
					break
				}
			}
			if !found {
				g.symByName[n.Name] = append(g.symByName[n.Name], n.ID)
			}
		}
	}

	// Drop outgoing static edges of replaced file/symbol nodes, then
	// re-add the static subset of the update.
	for id := range keep {
		for _, e := range g.out[id] {
			g.removeBackEdge(e)
		}
		delete(g.out, id)
	}
	var static []Edge
	for _, e := range dedupeEdges(upd.Edges) {
		if staticKinds[e.Kind] {
			static = append(static, e)
		}
	}
	for _, e := range static {
		g.out[e.Src] = append(g.out[e.Src], e)
		g.back[e.Dst] = append(g.back[e.Dst], e)
	}
	// Replace the name uses of this file's holders; dynamic edges
	// resolve live from the new uses, so no stale cross-file link
	// can survive an incremental update.
	for id := range keep {
		g.removeHolderUsesLocked(id)
	}
	seenUse := map[NameUse]bool{}
	var uses []NameUse
	for _, u := range upd.Uses {
		if u.Holder == "" || u.Name == "" {
			continue
		}
		switch u.Kind {
		case UseCalls, UseReferences, UseCites:
		default:
			continue
		}
		if !keep[u.Holder] {
			continue
		}
		if seenUse[u] {
			continue
		}
		seenUse[u] = true
		uses = append(uses, u)
		g.addUseLocked(u)
	}
	// Prune edges left dangling by replaced nodes.
	g.pruneDangling()
	g.files[upd.Rel] = fileRow{lang: upd.Lang, hash: upd.Hash, indexedAt: now}

	return g.persistFileLocked(upd.Rel, upd.Lang, upd.Hash, all, static, uses)
}

// fileNodeIDs lists node IDs belonging to a file: its file node plus
// defined symbols and sections.
func (g *Graph) fileNodeIDs(rel string) []string {
	var out []string
	fid := FileID(rel)
	if _, ok := g.nodes[fid]; ok {
		out = append(out, fid)
	}
	for _, e := range g.out[fid] {
		if e.Kind == EdgeDefines {
			out = append(out, e.Dst)
		}
	}
	return out
}

func (g *Graph) removeBackEdge(e Edge) {
	edges := g.back[e.Dst]
	for i, x := range edges {
		if x.Src == e.Src && x.Kind == e.Kind {
			g.back[e.Dst] = append(edges[:i], edges[i+1:]...)
			if len(g.back[e.Dst]) == 0 {
				delete(g.back, e.Dst)
			}
			return
		}
	}
}

// pruneDangling drops edges whose endpoint node no longer exists.
func (g *Graph) pruneDangling() {
	for src, edges := range g.out {
		kept := edges[:0]
		for _, e := range edges {
			if _, ok := g.nodes[e.Src]; !ok {
				g.removeBackEdge(e)
				continue
			}
			if _, ok := g.nodes[e.Dst]; !ok {
				g.removeBackEdge(e)
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(g.out, src)
		} else {
			g.out[src] = kept
		}
	}
}

func dedupeEdges(in []Edge) []Edge {
	seen := map[Edge]bool{}
	out := make([]Edge, 0, len(in))
	for _, e := range in {
		if e.Src == "" || e.Dst == "" || e.Kind == "" {
			continue
		}
		if e.Src == e.Dst {
			continue
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

func (g *Graph) persistFileLocked(rel, lang, hash string, nodes []Node, edges []Edge, uses []NameUse) error {
	tx, err := g.db.Begin()
	if err != nil {
		return fmt.Errorf("codeintel: begin persist: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	// Determine replaced node IDs from the database side.
	oldIDs := map[string]bool{}
	rows, err := tx.Query(`SELECT id FROM nodes WHERE id = ? OR path = ?`, FileID(rel), rel)
	if err != nil {
		return fmt.Errorf("codeintel: list old nodes: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("codeintel: scan old node: %w", err)
		}
		oldIDs[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("codeintel: old nodes rows: %w", err)
	}
	keep := map[string]bool{}
	for _, n := range nodes {
		keep[n.ID] = true
	}
	for id := range oldIDs {
		if keep[id] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM nodes WHERE id = ?`, id); err != nil {
			return fmt.Errorf("codeintel: delete node: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM edges WHERE src = ? OR dst = ?`, id, id); err != nil {
			return fmt.Errorf("codeintel: delete edges of %s: %w", id, err)
		}
	}
	for _, n := range nodes {
		if _, err := tx.Exec(
			`INSERT INTO nodes(id, kind, subkind, path, name, line, end_line, hash)
			 VALUES(?,?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, subkind=excluded.subkind,
			   path=excluded.path, name=excluded.name, line=excluded.line,
			   end_line=excluded.end_line, hash=excluded.hash`,
			n.ID, n.Kind, n.SubKind, n.Path, n.Name, n.Line, n.EndLine, n.Hash,
		); err != nil {
			return fmt.Errorf("codeintel: upsert node: %w", err)
		}
	}
	// Replace outgoing static edges of kept nodes wholesale. Dynamic
	// edges are never stored: they resolve live from nameuses.
	for id := range keep {
		if _, err := tx.Exec(`DELETE FROM edges WHERE src = ?`, id); err != nil {
			return fmt.Errorf("codeintel: clear edges: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM nameuses WHERE holder = ?`, id); err != nil {
			return fmt.Errorf("codeintel: clear uses: %w", err)
		}
	}
	for _, e := range edges {
		if _, err := tx.Exec(
			`INSERT INTO edges(src, kind, dst) VALUES(?,?,?)
			 ON CONFLICT(src, kind, dst) DO NOTHING`,
			e.Src, e.Kind, e.Dst,
		); err != nil {
			return fmt.Errorf("codeintel: insert edge: %w", err)
		}
	}
	for _, u := range uses {
		if _, err := tx.Exec(
			`INSERT INTO nameuses(holder, kind, name) VALUES(?,?,?)
			 ON CONFLICT(holder, kind, name) DO NOTHING`,
			u.Holder, u.Kind, u.Name,
		); err != nil {
			return fmt.Errorf("codeintel: insert use: %w", err)
		}
	}
	// Prune edges dangling at removed targets.
	if _, err := tx.Exec(`DELETE FROM edges WHERE dst NOT IN (SELECT id FROM nodes)`); err != nil {
		return fmt.Errorf("codeintel: prune edges: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO files(path, lang, hash, indexed_at) VALUES(?,?,?,?)
		 ON CONFLICT(path) DO UPDATE SET lang=excluded.lang, hash=excluded.hash,
		   indexed_at=excluded.indexed_at`,
		rel, lang, hash, time.Now().Unix(),
	); err != nil {
		return fmt.Errorf("codeintel: upsert file: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codeintel: commit file: %w", err)
	}
	return nil
}

// RemoveFile drops every node and edge belonging to a file, in memory
// and in codegraph.db. Missing files are a no-op.
func (g *Graph) RemoveFile(rel string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	ids := g.fileNodeIDs(rel)
	if _, ok := g.files[rel]; !ok && len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		delete(g.nodes, id)
		for _, e := range g.out[id] {
			g.removeBackEdge(e)
		}
		delete(g.out, id)
		g.removeHolderUsesLocked(id)
	}
	for name, list := range g.symByName {
		kept := list[:0]
		for _, id := range list {
			if _, ok := g.nodes[id]; ok {
				kept = append(kept, id)
			}
		}
		if len(kept) == 0 {
			delete(g.symByName, name)
		} else {
			g.symByName[name] = kept
		}
	}
	g.pruneDangling()
	delete(g.files, rel)

	tx, err := g.db.Begin()
	if err != nil {
		return fmt.Errorf("codeintel: begin remove: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM nodes WHERE id = ?`, id); err != nil {
			return fmt.Errorf("codeintel: delete node: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM edges WHERE src = ? OR dst = ?`, id, id); err != nil {
			return fmt.Errorf("codeintel: delete edges: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM nameuses WHERE holder = ?`, id); err != nil {
			return fmt.Errorf("codeintel: delete uses: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE path = ?`, rel); err != nil {
		return fmt.Errorf("codeintel: delete file: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM edges WHERE dst NOT IN (SELECT id FROM nodes)`); err != nil {
		return fmt.Errorf("codeintel: prune edges: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codeintel: commit remove: %w", err)
	}
	return nil
}

// RequirementID returns the deterministic node ID of a requirement.
func RequirementID(reqID string) string { return "req:" + reqID }

// AttachRequirement links a file or symbol node to a requirement
// (Section 10.10: files implementing a requirement; Section 8.3
// impact area). The task wave records these links; code_graph
// implementers reads them back.
func (g *Graph) AttachRequirement(reqID, nodeID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[nodeID]; !ok {
		return fmt.Errorf("codeintel: unknown node %q", nodeID)
	}
	rid := RequirementID(reqID)
	if _, ok := g.nodes[rid]; !ok {
		g.nodes[rid] = Node{ID: rid, Kind: NodeRequirement, Name: reqID}
		if err := g.insertNodeRow(rid); err != nil {
			return err
		}
	}
	e := Edge{Src: rid, Kind: EdgeRequirement, Dst: nodeID}
	for _, x := range g.out[rid] {
		if x == e {
			return nil
		}
	}
	g.out[rid] = append(g.out[rid], e)
	g.back[nodeID] = append(g.back[nodeID], e)
	if _, err := g.db.Exec(
		`INSERT INTO edges(src, kind, dst) VALUES(?,?,?)
		 ON CONFLICT(src, kind, dst) DO NOTHING`,
		e.Src, e.Kind, e.Dst,
	); err != nil {
		return fmt.Errorf("codeintel: attach requirement: %w", err)
	}
	return nil
}

func (g *Graph) insertNodeRow(id string) error {
	n := g.nodes[id]
	if _, err := g.db.Exec(
		`INSERT INTO nodes(id, kind, subkind, path, name, line, end_line, hash)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		n.ID, n.Kind, n.SubKind, n.Path, n.Name, n.Line, n.EndLine, n.Hash,
	); err != nil {
		return fmt.Errorf("codeintel: insert node: %w", err)
	}
	return nil
}

// Implementers returns the files and symbols linked to a requirement,
// ordered by node ID. Empty when nothing was attached yet.
func (g *Graph) Implementers(reqID string) []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []Node
	for _, e := range g.out[RequirementID(reqID)] {
		if e.Kind != EdgeRequirement {
			continue
		}
		if n, ok := g.nodes[e.Dst]; ok {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Slugify renders a document heading as a stable section slug.
func Slugify(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	var b strings.Builder
	prevDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
