package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const maxAdminFormBytes = 1 << 20

func (sc *Smithy) repository(w http.ResponseWriter, r *http.Request) (RepositoryWithName, bool) {
	repo, ok := sc.FindRepo(r.PathValue("repo"))
	if !ok {
		sc.Error(w, http.StatusNotFound, errors.New("repository not found"))
	}
	return repo, ok
}

func (sc *Smithy) revision(w http.ResponseWriter, repo *git.Repository, ref string) (*plumbing.Hash, bool) {
	revision, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("reference %q not found", ref))
		return nil, false
	}
	return revision, true
}

func (sc *Smithy) commit(w http.ResponseWriter, repo *git.Repository, value string) (*object.Commit, bool) {
	revision, ok := sc.revision(w, repo, value)
	if !ok {
		return nil, false
	}
	commit, err := repo.CommitObject(*revision)
	if err != nil {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("commit %q not found", value))
		return nil, false
	}
	return commit, true
}

func (sc *Smithy) parseAdminForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminFormBytes)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			sc.Error(w, http.StatusRequestEntityTooLarge, errors.New("form is too large"))
		} else {
			sc.Error(w, http.StatusBadRequest, err)
		}
		return false
	}
	return true
}

func (sc *Smithy) Reload(w http.ResponseWriter, _ *http.Request) {
	if err := sc.LoadAllRepositories(); err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (sc *Smithy) IndexView(w http.ResponseWriter, _ *http.Request) {
	sc.Render(w, "index", indexPage{Repos: sc.GetRepositories()})
}

func (sc *Smithy) NewProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sc.Render(w, "new", struct{}{})
		return
	}
	if !sc.parseAdminForm(w, r) {
		return
	}
	repoName := r.FormValue("name")
	repoPath, err := sc.RepositoryPath(repoName)
	if err != nil {
		sc.Error(w, http.StatusBadRequest, err)
		return
	}
	repo, err := git.PlainInit(repoPath, true)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryAlreadyExists) {
			sc.Error(w, http.StatusConflict, errors.New("repository already exists"))
			return
		}
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	sc.AddRepository(RepositoryWithName{Name: repoName, Path: repoPath, Repository: repo})
	http.Redirect(w, r, "/"+url.PathEscape(repoName), http.StatusSeeOther)
}

func (sc *Smithy) ImportProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sc.Render(w, "import", struct{}{})
		return
	}
	if !sc.parseAdminForm(w, r) {
		return
	}
	name := r.FormValue("name")
	address := strings.TrimSpace(r.FormValue("git"))
	if address == "" {
		sc.Error(w, http.StatusBadRequest, errors.New("Git URL is required"))
		return
	}
	repoPath, err := sc.RepositoryPath(name)
	if err != nil {
		sc.Error(w, http.StatusBadRequest, err)
		return
	}
	repo, err := git.PlainCloneContext(r.Context(), repoPath, r.FormValue("bare") == "on", &git.CloneOptions{URL: address})
	if err != nil {
		if errors.Is(err, git.ErrRepositoryAlreadyExists) {
			sc.Error(w, http.StatusConflict, errors.New("repository already exists"))
			return
		}
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	sc.AddRepository(RepositoryWithName{Name: name, Repository: repo, Path: repoPath})
	http.Redirect(w, r, "/"+url.PathEscape(name), http.StatusSeeOther)
}

func (sc *Smithy) RepoView(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.repository(w, r)
	if !ok {
		return
	}
	branches, err := ListBranches(repo.Repository)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	tags, err := ListTags(repo.Repository)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	data := repoPage{repositoryPage: repositoryPage{RepoName: repo.Name}, Branches: branches, Tags: tags, Repo: repo}
	mainBranch, revision, err := FindMainBranch(repo.Repository)
	if errors.Is(err, ErrNoBranches) {
		sc.Render(w, "repo", data)
		return
	}
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	slog.Debug("resolved default branch", "repository", repo.Name, "branch", mainBranch)
	commit, err := repo.Repository.CommitObject(*revision)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	if readme, err := GetReadmeFromCommit(commit); err == nil {
		if readme.Size > maxReadmeBytes {
			data.ReadmeTooLarge = true
		} else if contents, err := readme.Contents(); err != nil {
			slog.Error("read README", "repository", repo.Name, "error", err)
		} else if formatted, err := FormatMarkdown(contents); err != nil {
			slog.Error("render README", "repository", repo.Name, "error", err)
		} else {
			data.Readme = formatted
		}
	}
	sc.Render(w, "repo", data)
}

func (sc *Smithy) RefsView(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.repository(w, r)
	if !ok {
		return
	}
	branches, err := ListBranches(repo.Repository)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	tags, err := ListTags(repo.Repository)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	sc.Render(w, "refs", refsPage{repositoryPage: repositoryPage{RepoName: repo.Name}, Branches: branches, Tags: tags})
}

func requestRef(r *http.Request) string {
	if ref := r.URL.Query().Get("ref"); ref != "" {
		return ref
	}
	return r.PathValue("ref")
}

func (sc *Smithy) TreeView(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.repository(w, r)
	if !ok {
		return
	}
	refName := requestRef(r)
	if refName == "" {
		var err error
		refName, _, err = FindMainBranch(repo.Repository)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
	}
	revision, ok := sc.revision(w, repo.Repository, refName)
	if !ok {
		return
	}
	treePath := r.URL.Query().Get("path")
	if treePath == "" {
		treePath = r.PathValue("path")
	}
	if treePath != "" {
		clean := path.Clean(treePath)
		if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			sc.Error(w, http.StatusBadRequest, errors.New("invalid tree path"))
			return
		}
		treePath = clean
	}
	commit, err := repo.Repository.CommitObject(*revision)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	tree, err := commit.Tree()
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	base := repositoryPage{RepoName: repo.Name}
	if treePath == "" {
		sc.Render(w, "tree", treePage{repositoryPage: base, RefName: refName, Files: tree.Entries})
		return
	}
	entry, err := tree.FindEntry(treePath)
	if err != nil {
		sc.Error(w, http.StatusNotFound, errors.New("path not found"))
		return
	}
	parentPath := path.Dir(treePath)
	if parentPath == "." {
		parentPath = ""
	}
	if !entry.Mode.IsFile() {
		subtree, err := tree.Tree(treePath)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		sc.Render(w, "tree", treePage{repositoryPage: base, ParentPath: parentPath, RefName: refName, SubTree: entry.Name, Path: treePath, Files: subtree.Entries})
		return
	}
	file, err := tree.File(treePath)
	if err != nil {
		sc.Error(w, http.StatusNotFound, errors.New("file not found"))
		return
	}
	data := blobPage{repositoryPage: base, RefName: refName, File: entry, ParentPath: parentPath, Path: treePath}
	if file.Size > maxBlobBytes {
		data.TooLarge = true
	} else if data.Contents, err = file.Contents(); err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	sc.Render(w, "blob", data)
}

func logLimit(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return defaultLogLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxLogLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxLogLimit)
	}
	return limit, nil
}

func (sc *Smithy) LogView(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.repository(w, r)
	if !ok {
		return
	}
	refName := requestRef(r)
	if refName == "" {
		defaultBranch, _, err := FindMainBranch(repo.Repository)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/log?ref=%s", url.PathEscape(repo.Name), url.QueryEscape(defaultBranch)), http.StatusFound)
		return
	}
	limit, err := logLimit(r)
	if err != nil {
		sc.Error(w, http.StatusBadRequest, err)
		return
	}
	revision, ok := sc.revision(w, repo.Repository, refName)
	if !ok {
		return
	}
	iter, err := repo.Repository.Log(&git.LogOptions{From: *revision, Order: git.LogOrderCommitterTime})
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	defer iter.Close()
	commits := make([]Commit, 0, limit)
	truncated := false
	for len(commits) <= limit {
		commit, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		if len(commits) == limit {
			truncated = true
			break
		}
		subject, _, _ := strings.Cut(commit.Message, "\n")
		commits = append(commits, Commit{Commit: commit, Subject: subject, ShortHash: commit.Hash.String()[:8]})
	}
	sc.Render(w, "log", logPage{repositoryPage: repositoryPage{RepoName: repo.Name}, RefName: refName, Commits: commits, Truncated: truncated, Limit: limit})
}

func (sc *Smithy) CommitView(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.repository(w, r)
	if !ok {
		return
	}
	commit, ok := sc.commit(w, repo.Repository, r.PathValue("hash"))
	if !ok {
		return
	}
	changes, err := GetChanges(commit)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	data := commitPage{repositoryPage: repositoryPage{RepoName: repo.Name, Commit: commit}}
	formatted, err := FormatChanges(changes)
	if errors.Is(err, ErrContentTooLarge) {
		data.DiffTooLarge = true
	} else if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	} else {
		data.Changes = formatted
	}
	sc.Render(w, "commit", data)
}

func (sc *Smithy) PatchView(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.repository(w, r)
	if !ok {
		return
	}
	commit, ok := sc.commit(w, repo.Repository, r.PathValue("hash"))
	if !ok {
		return
	}
	if commit.NumParents() == 0 {
		sc.Error(w, http.StatusNotFound, errors.New("commit has no parent"))
		return
	}
	changes, err := GetChanges(commit)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err := ensureChangesWithinLimit(changes, maxDiffInputBytes); err != nil {
		sc.Error(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	parent, err := commit.Parent(0)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	patch, err := parent.Patch(commit)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	stats, err := commit.Stats()
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	const commitFormatDate = "Mon, 2 Jan 2006 15:04:05 -0700"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	fmt.Fprintf(w, "From %s Mon Sep 17 00:00:00 2001\nFrom: %s <%s>\nDate: %s\nSubject: [PATCH] %s\n---\n%s\n%s",
		commit.Hash, commit.Author.Name, commit.Author.Email, commit.Author.When.Format(commitFormatDate), commit.Message, stats.String(), patch.String())
}
