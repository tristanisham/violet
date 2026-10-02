package components

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fileResolverFixture(t *testing.T, files map[string]string) (*FileResolver, string) {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resolver, err := NewFileResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolver, root
}

func assertFileCandidates(t *testing.T, resolver *FileResolver, query string, want []string) {
	t.Helper()
	got, err := resolver.Candidates(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates(%q) = %v, want %v", query, got, want)
	}
}

func TestNewFileResolver(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewFileResolver("")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.root != cwd || !filepath.IsAbs(resolver.root) {
		t.Fatalf("root = %q, want absolute CWD %q", resolver.root, cwd)
	}
	resolver, err = NewFileResolver(".")
	if err != nil || resolver.root != cwd {
		t.Fatalf("relative root: resolver = %v, error = %v", resolver, err)
	}
	_, root := fileResolverFixture(t, map[string]string{"file": ""})
	for _, name := range []string{"file", "missing"} {
		if _, err := NewFileResolver(filepath.Join(root, name)); err == nil {
			t.Fatalf("NewFileResolver(%q) should fail", name)
		}
	}
}

func TestFileResolverIgnores(t *testing.T) {
	resolver, root := fileResolverFixture(t, map[string]string{
		".gitignore": "# comment\n\n*.log\n!keep.log\n/root-only.txt\ncache/\nnode_modules/\nscoped/*.tmp\n",
		"keep.log":   "", "drop.log": "", "root-only.txt": "", "cache": "",
		"src/.gitignore":  "!rescued.log\n/local-only.txt\nnested/*.tmp\n!nested/keep.tmp\n",
		"src/rescued.log": "", "src/drop.log": "", "src/keep.log": "",
		"src/root-only.txt": "", "src/local-only.txt": "", "src/cache/a.txt": "",
		"src/deep/local-only.txt": "", "src/nested/drop.tmp": "", "src/nested/keep.tmp": "",
		"src/deep/nested/visible.tmp": "", "scoped/drop.tmp": "", "deep/scoped/visible.tmp": "",
		"sibling/rescued.log": "", "sibling/local-only.txt": "",
		"node_modules/package/index.js": "", "node_modules/.gitignore": "!index.js\n",
		".git/config": "", "src/.git/config": "",
	})
	// The fixture has no Git repository; ignores still apply. An unreadable
	// ignore file inside a pruned directory must never be opened.
	if err := os.Chmod(filepath.Join(root, "node_modules", ".gitignore"), 0); err != nil {
		t.Fatal(err)
	}
	assertFileCandidates(t, resolver, "", []string{
		".gitignore", "cache", "deep/scoped/visible.tmp", "keep.log", "sibling/local-only.txt",
		"src/.gitignore", "src/deep/local-only.txt", "src/deep/nested/visible.tmp",
		"src/keep.log", "src/nested/keep.tmp", "src/rescued.log", "src/root-only.txt",
	})
}

func TestFileResolverNegationsAndDirectoryRules(t *testing.T) {
	resolver, _ := fileResolverFixture(t, map[string]string{
		".gitignore":  "*.txt\n!allow/\nblocked/\n!blocked/keep.txt\n/root-dir/\n!last.txt\nlast.txt\n",
		"allow/a.txt": "", "allow/deep/b.txt": "", "drop.txt": "",
		"blocked/keep.txt": "", "blocked/.gitignore": "!keep.txt\n",
		"root-dir/a.go": "", "sub/root-dir/a.go": "", "last.txt": "",
		"sub/.gitignore": "!last.txt\n", "sub/last.txt": "",
	})
	assertFileCandidates(t, resolver, "", []string{
		".gitignore", "sub/.gitignore", "sub/last.txt", "sub/root-dir/a.go",
	})
}

func TestFileResolverReadsIgnoresBeforeChildren(t *testing.T) {
	resolver, root := fileResolverFixture(t, map[string]string{
		".gitignore": "000-blocked/\n", "000-blocked/.gitignore": "!a.txt\n",
		"000-blocked/a.txt": "", "visible.txt": "",
	})
	if err := os.Chmod(filepath.Join(root, "000-blocked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "000-blocked"), 0o755) })
	assertFileCandidates(t, resolver, "", []string{".gitignore", "visible.txt"})
}

func TestFileResolverGlobs(t *testing.T) {
	resolver, _ := fileResolverFixture(t, map[string]string{
		"main.go": "", "README.md": "", "src/main.go": "", "src/map.go": "",
		"src/deep/more.go": "", "src/main.txt": "",
	})
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"**/*.go", []string{"main.go", "src/deep/more.go", "src/main.go", "src/map.go"}},
		{"*.go", []string{"main.go", "src/deep/more.go", "src/main.go", "src/map.go"}},
		{"src/*.go", []string{"src/main.go", "src/map.go"}},
		{"src/**/*.go", []string{"src/deep/more.go", "src/main.go", "src/map.go"}},
		{"ma?.go", []string{"src/map.go"}},
		{"[m]ain.go", []string{"main.go", "src/main.go"}},
		{"./src/*.go", []string{"src/main.go", "src/map.go"}},
		{"*.GO", []string{}},
	} {
		t.Run(test.query, func(t *testing.T) { assertFileCandidates(t, resolver, test.query, test.want) })
	}
	if _, err := resolver.Candidates(context.Background(), "["); err == nil {
		t.Fatal("malformed glob should return an error")
	}
	empty, _ := fileResolverFixture(t, nil)
	if _, err := empty.Resolve(context.Background(), "["); err == nil {
		t.Fatal("malformed glob in an empty tree should not fall back")
	}
}

func TestFileResolverFuzzyOrdering(t *testing.T) {
	resolver, _ := fileResolverFixture(t, map[string]string{
		"main.go": "", "a/MAIN.GO": "", "b/main.go": "", "amain.go.bak": "",
		"zmain.go": "", "m-a-i-n.g-o": "", "unrelated.txt": "",
		"é-file.go": "",
	})
	assertFileCandidates(t, resolver, "MaIn.Go", []string{
		"main.go", "a/MAIN.GO", "b/main.go", "amain.go.bak", "zmain.go", "m-a-i-n.g-o",
	})
	assertFileCandidates(t, resolver, "ÉGO", []string{"é-file.go"})
	assertFileCandidates(t, resolver, "absent", []string{})
}

func TestFileResolverResolve(t *testing.T) {
	resolver, root := fileResolverFixture(t, map[string]string{"a.go": "", "dir/b.go": ""})
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"*.go", []string{"a.go", "dir/b.go"}},
		{"a.go", []string{"a.go"}},
		{"missing[0-9].go", []string{"missing[0-9].go"}},
		{"../outside.go", []string{"../outside.go"}},
		{filepath.Join(root, "a.go"), []string{filepath.Join(root, "a.go")}},
	} {
		got, err := resolver.Resolve(context.Background(), test.query)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("Resolve(%q) = %v, %v; want %v", test.query, got, err, test.want)
		}
	}
	for _, query := range []string{"../a.go", "dir/../../a.go", "**/../*.go", filepath.Join(root, "a.go")} {
		assertFileCandidates(t, resolver, query, []string{})
	}
}

func TestFileResolverOmitsSymlinks(t *testing.T) {
	resolver, root := fileResolverFixture(t, map[string]string{"real/a.go": "", "visible.txt": ""})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.go"), []byte("*.go\n*.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"linked-file": filepath.Join(root, "visible.txt"), "linked-directory": filepath.Join(root, "real"),
		"outside": outside, "dangling": filepath.Join(root, "missing"),
		".gitignore": filepath.Join(outside, "outside.go"),
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	assertFileCandidates(t, resolver, "", []string{"real/a.go", "visible.txt"})
	if _, err := NewFileResolver(filepath.Join(root, "linked-directory")); err == nil {
		t.Fatal("a symlink root should be rejected")
	}
}

func TestFileResolverPermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files and directories regardless of permission bits")
	}
	resolver, root := fileResolverFixture(t, map[string]string{"locked/a.go": "", "visible.go": ""})
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	// Regular candidates need only metadata, not permission to read contents.
	if err := os.Chmod(filepath.Join(root, "visible.go"), 0); err != nil {
		t.Fatal(err)
	}
	assertFileCandidates(t, resolver, "", []string{"visible.go"})
	ignoreFile := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignoreFile, []byte("*.go\n"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Candidates(context.Background(), ""); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unreadable .gitignore error = %v, want permission error", err)
	}
}

type fileResolverCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *fileResolverCancelContext) Err() error {
	c.checks--
	if c.checks <= 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestFileResolverCancellation(t *testing.T) {
	resolver, _ := fileResolverFixture(t, map[string]string{
		".gitignore": strings.Repeat("*.ignored\n", 30), "a.go": "", "nested/b.go": "",
	})
	for _, checks := range []int{1, 10, 38, 42} {
		ctx, cancel := context.WithCancel(context.Background())
		counted := &fileResolverCancelContext{Context: ctx, cancel: cancel, checks: checks}
		got, err := resolver.Resolve(counted, "*.go")
		cancel()
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("cancellation after %d checks: got %v, error %v", checks, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Candidates(ctx, "../outside"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled invalid query: error = %v", err)
	}
}

func TestFileResolverRefreshesIgnores(t *testing.T) {
	resolver, root := fileResolverFixture(t, map[string]string{"a.go": ""})
	assertFileCandidates(t, resolver, "*.go", []string{"a.go"})
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertFileCandidates(t, resolver, "*.go", []string{})
}

func TestFileResolverIgnorePatternSyntax(t *testing.T) {
	resolver, _ := fileResolverFixture(t, map[string]string{
		".gitignore": "file?.txt\n leading.txt\ntrailing\\ \n(a).txt\n\\#secret\n\\!secret\n",
		"file1.txt":  "", "file12.txt": "", " leading.txt": "", "leading.txt": "",
		"trailing ": "", "trailing": "", "(a).txt": "", "a.txt": "",
		"#secret": "", "!secret": "",
	})
	assertFileCandidates(t, resolver, "", []string{".gitignore", "a.txt", "file12.txt", "leading.txt", "trailing"})
}

func TestFileResolverRejectsReplacedRoot(t *testing.T) {
	resolver, root := fileResolverFixture(t, map[string]string{"inside.go": ""})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if files, err := resolver.Candidates(context.Background(), ""); err == nil {
		t.Fatalf("followed replaced root: %v", files)
	}
}
