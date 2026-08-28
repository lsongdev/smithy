package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
)

type RepositoryWithName struct {
	Name       string
	Path       string
	Repository *git.Repository
}

type RepositoryByName []RepositoryWithName

func (r RepositoryByName) Len() int      { return len(r) }
func (r RepositoryByName) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r RepositoryByName) Less(i, j int) bool {
	res := strings.Compare(r[i].Name, r[j].Name)
	return res < 0
}

type ReferenceByName []*plumbing.Reference

func (r ReferenceByName) Len() int      { return len(r) }
func (r ReferenceByName) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r ReferenceByName) Less(i, j int) bool {
	res := strings.Compare(r[i].Name().String(), r[j].Name().String())
	return res < 0
}

type Smithy struct {
	Root       string
	WriteToken string
	repos      atomic.Value
	repoMu     sync.Mutex
	template   *template.Template
}

func NewSmithy(root string) *Smithy {
	sc := &Smithy{Root: filepath.Clean(root)}
	sc.repos.Store(map[string]RepositoryWithName{})
	return sc
}

func (sc *Smithy) AddRepository(rwn RepositoryWithName) {
	sc.repoMu.Lock()
	defer sc.repoMu.Unlock()
	current := sc.repositorySnapshot()
	next := make(map[string]RepositoryWithName, len(current)+1)
	for name, repo := range current {
		next[name] = repo
	}
	next[rwn.Name] = rwn
	sc.repos.Store(next)
}

func (sc *Smithy) LoadAllRepositories() (err error) {
	sc.repoMu.Lock()
	defer sc.repoMu.Unlock()
	files, err := os.ReadDir(sc.Root)
	if err != nil {
		return
	}
	repos := make(map[string]RepositoryWithName)
	for _, f := range files {
		repoPath := filepath.Join(sc.Root, f.Name())
		r, err := git.PlainOpen(repoPath)
		if err != nil {
			continue
		}
		key := f.Name()
		rwn := RepositoryWithName{
			Name:       f.Name(),
			Repository: r,
			Path:       repoPath,
		}
		repos[key] = rwn
	}
	sc.repos.Store(repos)
	return
}

func (sc *Smithy) repositorySnapshot() map[string]RepositoryWithName {
	return sc.repos.Load().(map[string]RepositoryWithName)
}

func (sc *Smithy) GetRepositories() []RepositoryWithName {
	var repos []RepositoryWithName
	for _, repo := range sc.repositorySnapshot() {
		repos = append(repos, repo)
	}
	sort.Sort(RepositoryByName(repos))
	return repos
}

func (sc *Smithy) FindRepo(slug string) (RepositoryWithName, bool) {
	value, exists := sc.repositorySnapshot()[slug]
	return value, exists
}

var validRepositoryName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (sc *Smithy) RepositoryPath(name string) (string, error) {
	if name == "" || name == "." || name == ".." || !validRepositoryName.MatchString(name) {
		return "", fmt.Errorf("invalid repository name %q", name)
	}

	target := filepath.Join(sc.Root, name)
	rel, err := filepath.Rel(sc.Root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository path escapes root")
	}
	return target, nil
}

type Commit struct {
	Commit    *object.Commit
	Subject   string
	ShortHash string
}

func (c *Commit) CommitDate() string {
	return c.Commit.Author.When.Format(time.DateTime)
}

func ReferenceCollector(it storer.ReferenceIter) ([]*plumbing.Reference, error) {
	var refs []*plumbing.Reference

	for {
		b, err := it.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return refs, err
		}

		refs = append(refs, b)
	}
	sort.Sort(ReferenceByName(refs))
	return refs, nil
}

func ListBranches(r *git.Repository) ([]*plumbing.Reference, error) {
	it, err := r.Branches()
	if err != nil {
		return []*plumbing.Reference{}, err
	}
	return ReferenceCollector(it)
}

func ListTags(r *git.Repository) ([]*plumbing.Reference, error) {
	it, err := r.Tags()
	if err != nil {
		return []*plumbing.Reference{}, err
	}
	return ReferenceCollector(it)
}

func GetReadmeFromCommit(commit *object.Commit) (*object.File, error) {
	options := []string{
		"readme",
		"README",
		"readme.md",
		"README.md",
		"readme.txt",
		"README.txt",
		"readme.markdown",
		"README.markdown",
	}

	for _, opt := range options {
		f, err := commit.File(opt)

		if err == nil {
			return f, nil
		}
	}
	return nil, errors.New("no valid readme")
}

func FormatMarkdown(input string) string {
	var buf bytes.Buffer
	markdown := goldmark.New(
		goldmark.WithExtensions(
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(
					html.WithClasses(true),
				),
			),
		),
	)
	if err := markdown.Convert([]byte(input), &buf); err != nil {
		return input
	}
	return buf.String()
}

func FindMainBranch(repo *git.Repository) (string, *plumbing.Hash, error) {
	head, err := repo.Reference(plumbing.HEAD, false)
	if err == nil && head.Type() == plumbing.SymbolicReference && head.Target().IsBranch() {
		branch := head.Target().Short()
		revision, resolveErr := repo.ResolveRevision(plumbing.Revision(head.Target().String()))
		if resolveErr == nil {
			return branch, revision, nil
		}
	}

	branches, _ := ListBranches(repo)

	if len(branches) == 0 {
		return "", nil, errors.New("no branches found")
	}

	var branch string
	for _, br := range branches {
		if br.Name().Short() == "main" || br.Name().Short() == "master" {
			branch = br.Name().Short()
			break
		}
	}
	if branch == "" {
		branch = branches[0].Name().Short()
	}
	revision, err := repo.ResolveRevision(plumbing.Revision(branch))
	return branch, revision, err
}

func GetChanges(commit *object.Commit) (object.Changes, error) {
	var changes object.Changes
	var parentTree *object.Tree

	parent, err := commit.Parent(0)
	if err == nil {
		parentTree, err = parent.Tree()
		if err != nil {
			return changes, err
		}
	}

	currentTree, err := commit.Tree()
	if err != nil {
		return changes, err
	}

	return object.DiffTree(parentTree, currentTree)

}

// PatchHTML returns an HTML representation of a patch
func PatchHTML(p object.Patch) string {
	buf := bytes.NewBuffer(nil)
	ue := NewUnifiedEncoder(buf, DefaultContextLines)
	err := ue.Encode(p)
	if err != nil {
		fmt.Println("PatchHTML error")
	}
	return buf.String()
}

// FormatChanges spits out something similar to `git diff`
func FormatChanges(changes object.Changes) (string, error) {
	var s []string
	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			return "", err
		}
		s = append(s, PatchHTML(*patch))
	}

	return strings.Join(s, "\n\n\n\n"), nil
}
