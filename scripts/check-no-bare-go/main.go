// Command check-no-bare-go flags goroutine launches outside internal/safego.
// Long-lived goroutines use safego.Go (Sections 3.2 and 5.3).
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func check(root string, output io.Writer) error {
	fset := token.NewFileSet()
	offenders := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == "internal/safego/safego.go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if launch, ok := node.(*ast.GoStmt); ok {
				fmt.Fprintf(output, "bare go statement in %s:%d\n", rel, fset.Position(launch.Pos()).Line)
				offenders = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		return err
	}
	if offenders {
		return fmt.Errorf("launch long-lived goroutines via safego.Go (internal/safego)")
	}
	fmt.Fprintln(output, "OK: no bare go statements outside internal/safego.")
	return nil
}

func main() {
	// Run from the repository root, including from PowerShell without Bash.
	if err := check(".", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}
