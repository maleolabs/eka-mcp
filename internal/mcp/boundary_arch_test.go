package mcp_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This is the consumer-boundary architecture test (ADR-014, ADR-030):
// eka-mcp is a CONSUMER of eka-core. Its public contract is
// github.com/maleolabs/eka-core/v2/api; the kernel-internal packages
// (runtime, store, workspace, sync) are private implementation details
// and must not appear in the NON-test source of this module.
//
// The test walks every non-test .go file of the module and fails if one
// imports a forbidden package. Test files are exempt: a test may import
// the internals to set up fixtures.

// forbiddenInternal are the eka-core kernel-internal import paths no
// non-test eka-mcp file may depend on.
var forbiddenInternal = []string{
	"github.com/maleolabs/eka-core/v2/runtime",
	"github.com/maleolabs/eka-core/v2/store",
	"github.com/maleolabs/eka-core/v2/workspace",
	"github.com/maleolabs/eka-core/v2/sync",
}

// knownCoreGap is intentionally empty: the boundary is complete. The
// single structural exception (contexts.New(*runtime.Runtime) in
// internal/eka/capability.go) was closed when eka-core's api gained a
// Contexts service, so no non-test file needs EKA internals. The test
// still fails if ANY non-test file couples — keep this map empty.
var knownCoreGap = map[string]string{}

func TestNoInternalCoreImportsInNonTestCode(t *testing.T) {
	root := moduleRoot(t)

	found := map[string][]string{} // relative path -> forbidden imports
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip VCS/build noise; never skip a real source dir in this module.
			if name := d.Name(); name == ".git" || name == "testdata" {
				return fs.SkipDir
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
		rel = filepath.ToSlash(rel)

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if isForbidden(p) {
				found[rel] = append(found[rel], p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module source: %v", err)
	}

	// Every forbidden coupling must be exactly the pinned known gap:
	// an unknown file fails, and a stale gap entry fails.
	var violations []string
	for file, imps := range found {
		if _, ok := knownCoreGap[file]; !ok {
			sort.Strings(imps)
			violations = append(violations, file+" -> "+strings.Join(imps, ", "))
		}
	}
	for file, reason := range knownCoreGap {
		if _, ok := found[file]; !ok {
			violations = append(violations, "stale knownCoreGap entry (gap is gone; delete it): "+file+" ("+reason+")")
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("non-test code must not import eka-core internals %v; offending files:\n  %s",
			forbiddenInternal, strings.Join(violations, "\n  "))
	}
}

// isForbidden reports whether import path p names a kernel-internal
// eka-core package (exactly, or a subpackage of it).
func isForbidden(p string) bool {
	for _, f := range forbiddenInternal {
		if p == f || strings.HasPrefix(p, f+"/") {
			return true
		}
	}
	return false
}

// moduleRoot walks up from the test's working directory to the dir
// holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("boundary test: go.mod not found above the test directory")
		}
		dir = parent
	}
}
