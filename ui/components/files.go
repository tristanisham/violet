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
	ignore "github.com/sabhiram/go-gitignore"
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
	files := []string{}
	if err := r.walk(ctx, "", nil, &files); err != nil {
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
	scope   string
	matcher *ignore.GitIgnore
	negated bool
}

func readFileIgnoreRules(ctx context.Context, filename, scope string) ([]fileIgnoreRule, error) {
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat .gitignore %q: %w", filename, err)
	}
	// Never open a symlink or special file named .gitignore.
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read .gitignore %q: %w", filename, err)
	}
	var rules []fileIgnoreRule
	for _, line := range strings.Split(string(contents), "\n") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.Trim(line, " ")
		negated := strings.HasPrefix(line, "!")
		if negated {
			line = strings.TrimPrefix(line, "!")
		}
		if line == "" {
			continue
		}
		// A slash within a pattern anchors it to this ignore file's directory.
		// Keep the final slash: matching directories with a slash distinguishes
		// directory-only rules from ordinary files with the same name.
		if strings.Contains(strings.TrimSuffix(line, "/"), "/") && !strings.HasPrefix(line, "/") {
			line = "/" + line
		}
		// Compile negations as positive patterns: MatchesPathHow cannot report
		// a lone negation matching an inherited rule from a parent file.
		rules = append(rules, fileIgnoreRule{scope, ignore.CompileIgnoreLines(line), negated})
	}
	return rules, nil
}

func fileIgnored(name string, directory bool, rules []fileIgnoreRule) bool {
	if directory {
		name += "/"
	}
	ignored := false
	for _, rule := range rules {
		if strings.HasPrefix(name, rule.scope) && rule.matcher.MatchesPath(strings.TrimPrefix(name, rule.scope)) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func (r *FileResolver) walk(ctx context.Context, relative string, rules []fileIgnoreRule, files *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Join(r.root, filepath.FromSlash(relative))
	entries, err := os.ReadDir(directory)
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
	local, err := readFileIgnoreRules(ctx, filepath.Join(directory, ".gitignore"), scope)
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
			if err := r.walk(ctx, name, rules, files); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsPermission(err) {
				continue
			}
			return err
		}
		if info.Mode().IsRegular() {
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
