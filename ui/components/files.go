package components

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// FileResolver searches beneath an absolute root without following symlinks.
// Returned paths are slash-separated and relative to that root (CWD by default).
// Each search reads the current directory tree and .gitignore files afresh.
type FileResolver struct {
	root string
}

func NewFileResolver(root string) (*FileResolver, error) {
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("file resolver root %q is not a directory", root)
	}
	return &FileResolver{root: root}, nil
}

// Bind each search to the checked directory, including when its path is replaced.
func openFileResolverRoot(name string) (*os.Root, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("file resolver root %q is not a directory", name)
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("file resolver root %q changed during search", name)
	}
	return root, nil
}

// Candidates ranks fuzzy matches by exact path, exact basename, substring,
// then subsequence, with lexical ties. Glob queries are case-sensitive and
// lexically sorted. Absolute paths and any .. component yield no candidates.
func (r *FileResolver) Candidates(ctx context.Context, query string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = filepath.ToSlash(query)
	if filepath.IsAbs(query) {
		return []string{}, nil
	}
	for _, part := range strings.Split(query, "/") {
		if part == ".." {
			return []string{}, nil
		}
	}
	for strings.HasPrefix(query, "./") {
		query = strings.TrimPrefix(query, "./")
	}
	glob := strings.ContainsAny(query, "*?[")
	if glob {
		if _, err := doublestar.Match(query, ""); err != nil {
			return nil, err
		}
	}
	root, err := openFileResolverRoot(r.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := []string{}
	if err := r.walk(ctx, root, "", nil, &files); err != nil {
		return nil, err
	}
	type match struct {
		name string
		rank int
	}
	matches := make([]match, 0, len(files))
	folded := strings.ToLower(query)
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rank := -1
		if glob {
			target := name
			if !strings.Contains(query, "/") {
				target = path.Base(name)
			}
			matched, err := doublestar.Match(query, target)
			if err != nil {
				return nil, err
			}
			if matched {
				rank = 0
			}
		} else {
			rank = fileMatchRank(name, folded)
		}
		if rank >= 0 {
			matches = append(matches, match{name, rank})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		return matches[i].name < matches[j].name
	})
	result := make([]string, len(matches))
	for i, match := range matches {
		result[i] = match.name
	}
	return result, ctx.Err()
}

// Resolve preserves the original query when nothing matches. Search errors,
// including cancellation and malformed glob patterns, never become fallbacks.
func (r *FileResolver) Resolve(ctx context.Context, query string) ([]string, error) {
	files, err := r.Candidates(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []string{query}, nil
	}
	return files, nil
}

type fileIgnoreRule struct {
	scope         string
	pattern       string
	negated       bool
	directoryOnly bool
	anchored      bool
}

func readFileIgnoreRules(ctx context.Context, root *os.Root, filename, scope string) ([]fileIgnoreRule, error) {
	info, err := root.Lstat(filename)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat .gitignore %q: %w", filename, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	contents, err := root.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read .gitignore %q: %w", filename, err)
	}
	var rules []fileIgnoreRule
	for _, line := range strings.Split(string(contents), "\n") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if rule := parseFileIgnoreRule(line, scope); rule.pattern != "" {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func parseFileIgnoreRule(line, scope string) fileIgnoreRule {
	line = strings.TrimSuffix(line, "\r")
	// Git preserves leading spaces and escaped trailing spaces.
	for strings.HasSuffix(line, " ") {
		backslashes := 0
		for i := len(line) - 2; i >= 0 && line[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 != 0 {
			break
		}
		line = strings.TrimSuffix(line, " ")
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return fileIgnoreRule{}
	}
	rule := fileIgnoreRule{scope: scope, negated: strings.HasPrefix(line, "!")}
	if rule.negated {
		line = strings.TrimPrefix(line, "!")
	}
	rule.directoryOnly = strings.HasSuffix(line, "/")
	line = strings.TrimSuffix(line, "/")
	rule.anchored = strings.Contains(line, "/")
	rule.pattern = strings.TrimPrefix(line, "/")
	return rule
}

func fileIgnored(name string, directory bool, rules []fileIgnoreRule) bool {
	// Ancestors were already checked before descent. Match this entry only:
	// re-including a directory must not re-include its ignored children.
	for i := len(rules) - 1; i >= 0; i-- {
		rule := rules[i]
		if rule.directoryOnly && !directory || !strings.HasPrefix(name, rule.scope) {
			continue
		}
		target := strings.TrimPrefix(name, rule.scope)
		if !rule.anchored {
			target = path.Base(target)
		}
		if matched, _ := doublestar.Match(rule.pattern, target); matched {
			return !rule.negated
		}
	}
	return false
}

func (r *FileResolver) walk(ctx context.Context, root *os.Root, relative string, rules []fileIgnoreRule, files *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := path.Join(".", relative)
	entries, err := fs.ReadDir(root.FS(), directory)
	if err != nil {
		if relative != "" && os.IsPermission(err) {
			return nil
		}
		return err
	}
	// Load local ignores before considering any child, regardless of lexical order.
	scope := ""
	if relative != "" {
		scope = relative + "/"
	}
	local, err := readFileIgnoreRules(ctx, root, path.Join(directory, ".gitignore"), scope)
	if err != nil {
		return err
	}
	rules = append(rules, local...)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 || (entry.IsDir() && entry.Name() == ".git") {
			continue
		}
		name := path.Join(relative, entry.Name())
		if fileIgnored(name, entry.IsDir(), rules) {
			continue
		}
		if entry.IsDir() {
			if err := r.walk(ctx, root, name, rules, files); err != nil {
				return err
			}
			continue
		}
		if entry.Type().IsRegular() {
			*files = append(*files, name)
		}
	}
	return ctx.Err()
}

func fileMatchRank(name, query string) int {
	if query == "" {
		return 0
	}
	name = strings.ToLower(name)
	switch {
	case name == query:
		return 0
	case path.Base(name) == query:
		return 1
	case strings.Contains(name, query):
		return 2
	}
	remaining := []rune(query)
	for _, char := range name {
		if char == remaining[0] {
			remaining = remaining[1:]
			if len(remaining) == 0 {
				return 3
			}
		}
	}
	return -1
}
