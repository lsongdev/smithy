package main

import (
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

var (
	offset        = 5
	PAGE_SIZE int = 500
)

//go:embed templates
var templatefiles embed.FS

type GitCommand struct {
	procInput io.Reader
	args      []string
}

type H = map[string]interface{}

func (sc *Smithy) LoadTemplates() error {
	t := template.New("")
	files, err := templatefiles.ReadDir("templates")
	if err != nil {
		return err
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".html") {
			continue
		}
		f, err := templatefiles.Open("templates/" + file.Name())
		if err != nil {
			return err
		}
		contents, err := io.ReadAll(f)
		if err != nil {
			return err
		}

		_, err = t.New(file.Name()).Parse(string(contents))
		if err != nil {
			return err
		}
	}
	sc.template = t
	return nil
}

func (sc *Smithy) GetParam(r *http.Request, name string) (out string) {
	return r.Context().Value(ParamsKey).(map[string]string)[name]
}

func (sc *Smithy) Render(w http.ResponseWriter, name string, data H) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	sc.template.ExecuteTemplate(w, name+".html", data)
}

func (sc *Smithy) Error(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	sc.Render(w, "error", H{
		"Error": err.Error(),
	})
}

func (sc *Smithy) Reload(w http.ResponseWriter, r *http.Request) {
	if err := sc.LoadAllRepositories(); err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	fmt.Fprintf(w, "done")
}

func (sc *Smithy) RequireWriteAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sc.WriteToken == "" {
			http.Error(w, "write operations are disabled", http.StatusServiceUnavailable)
			return
		}

		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, password, ok := r.BasicAuth(); ok {
			provided = password
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(sc.WriteToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="smithy"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (sc *Smithy) IndexView(w http.ResponseWriter, r *http.Request) {
	repos := sc.GetRepositories()
	// commits, _ := repo.CommitObjects()
	// lastCommit, _ := commits.Next()
	sc.Render(w, "index", H{
		"Repos": repos,
	})
}

func (sc *Smithy) NewProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sc.Render(w, "new", H{})
		return
	}
	if err := r.ParseForm(); err != nil {
		sc.Error(w, http.StatusBadRequest, err)
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
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	sc.AddRepository(RepositoryWithName{Name: repoName, Path: repoPath, Repository: repo})
	fmt.Fprint(w, repoName)
}

func (sc *Smithy) ImportProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sc.Render(w, "import", H{})
		return
	}
	if err := r.ParseForm(); err != nil {
		sc.Error(w, http.StatusBadRequest, err)
		return
	}
	name := r.FormValue("name")
	bare := r.FormValue("bare")
	address := r.FormValue("git")
	repoPath, err := sc.RepositoryPath(name)
	if err != nil {
		sc.Error(w, http.StatusBadRequest, err)
		return
	}
	isBare := bare == "on"
	repo, err := git.PlainCloneContext(r.Context(), repoPath, isBare, &git.CloneOptions{
		URL: address,
	})
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	rwn := RepositoryWithName{
		Name:       name,
		Repository: repo,
		Path:       repoPath,
	}
	sc.AddRepository(rwn)
	fmt.Fprint(w, name)
}

func (sc *Smithy) RepoView(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
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

	main, revision, err := FindMainBranch(repo.Repository)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf(`%s default branch is "%s"`, repoName, main)
	commitObj, err := repo.Repository.CommitObject(*revision)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	readme, err := GetReadmeFromCommit(commitObj)
	var formattedReadme string
	if err != nil {
		formattedReadme = ""
	} else {
		readmeContents, err := readme.Contents()
		if err != nil {
			formattedReadme = ""
		} else {
			formattedReadme = FormatMarkdown(readmeContents)
		}
	}

	sc.Render(w, "repo", H{
		"RepoName": repoName,
		"Branches": branches,
		"Tags":     tags,
		"Readme":   template.HTML(formattedReadme),
		"Repo":     repo,
	})
}

func (sc *Smithy) RefsView(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
		return
	}

	branches, err := ListBranches(repo.Repository)
	if err != nil {
		branches = []*plumbing.Reference{}
	}

	tags, err := ListTags(repo.Repository)
	if err != nil {
		tags = []*plumbing.Reference{}
	}

	sc.Render(w, "refs", map[string]any{
		"RepoName": repoName,
		"Branches": branches,
		"Tags":     tags,
	})
}

func (sc *Smithy) TreeView(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
		return
	}

	var err error
	refName := r.URL.Query().Get("ref")
	if refName == "" {
		refName = sc.GetParam(r, "ref")
	}
	if refName == "" {
		refName, _, err = FindMainBranch(repo.Repository)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
	}

	revision, err := repo.Repository.ResolveRevision(plumbing.Revision(refName))
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	treePath := r.URL.Query().Get("path")
	if treePath == "" {
		treePath = sc.GetParam(r, "path")
	}
	parentPath := filepath.Dir(treePath)
	commitObj, err := repo.Repository.CommitObject(*revision)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	tree, err := commitObj.Tree()
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	// We're looking at the root of the project.  Show a list of files.
	if treePath == "" {
		sc.Render(w, "tree", H{
			"RepoName": repoName,
			"RefName":  refName,
			"Files":    tree.Entries,
			"Path":     treePath,
		})
		return
	}

	out, err := tree.FindEntry(treePath)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	// We found a subtree.
	if !out.Mode.IsFile() {
		subTree, err := tree.Tree(treePath)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		sc.Render(w, "tree", H{
			"RepoName":   repoName,
			"ParentPath": parentPath,
			"RefName":    refName,
			"SubTree":    out.Name,
			"Path":       treePath,
			"Files":      subTree.Entries,
		})
		return
	}

	file, err := tree.File(treePath)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	contents, err := file.Contents()
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	sc.Render(w, "blob", H{
		"RepoName":   repoName,
		"RefName":    refName,
		"File":       out,
		"ParentPath": parentPath,
		"Path":       treePath,
		"Contents":   contents,
	})
}

func (sc *Smithy) LogView(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
		return
	}

	refName := r.URL.Query().Get("ref")
	if refName == "" {
		refName = sc.GetParam(r, "ref")
	}
	if refName == "" {
		defaultBranchName, _, err := FindMainBranch(repo.Repository)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/log?ref=%s", repoName, url.QueryEscape(defaultBranchName)), http.StatusFound)
		return
	}

	revision, err := repo.Repository.ResolveRevision(plumbing.Revision(refName))
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	var commits []Commit
	cIter, err := repo.Repository.Log(&git.LogOptions{From: *revision, Order: git.LogOrderCommitterTime})
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	for i := 1; i <= PAGE_SIZE; i++ {
		commit, err := cIter.Next()
		if err == io.EOF {
			break
		}

		lines := strings.Split(commit.Message, "\n")

		c := Commit{
			Commit:    commit,
			Subject:   lines[0],
			ShortHash: commit.Hash.String()[:8],
		}
		commits = append(commits, c)
	}

	sc.Render(w, "log", H{
		"RepoName": repoName,
		"RefName":  refName,
		"Commits":  commits,
	})
}

func (sc *Smithy) CommitView(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")

	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
		return
	}
	commitID := sc.GetParam(r, "hash")
	if commitID == "" {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Commit not found"))
		return
	}
	commitHash := plumbing.NewHash(commitID)
	commitObj, err := repo.Repository.CommitObject(commitHash)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	changes, err := GetChanges(commitObj)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	formattedChanges, err := FormatChanges(changes)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	sc.Render(w, "commit", H{
		"RepoName": repoName,
		"Commit":   commitObj,
		"Changes":  template.HTML(formattedChanges),
	})
}

func (sc *Smithy) PatchView(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
		return
	}

	commitID := sc.GetParam(r, "hash")
	if commitID == "" {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Commit not found: %s", commitID))
		return
	}

	commitHash := plumbing.NewHash(commitID)
	commitObj, err := repo.Repository.CommitObject(commitHash)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	var patch string
	if commitObj.NumParents() == 0 {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Commit Parents not found"))
		return
	} else {
		parentCommit, err := commitObj.Parent(0)

		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}

		patchObj, err := parentCommit.Patch(commitObj)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		patch = patchObj.String()
	}

	const commitFormatDate = "Mon, 2 Jan 2006 15:04:05 -0700"
	commitHashStr := fmt.Sprintf("From %s Mon Sep 17 00:00:00 2001", commitObj.Hash)
	from := fmt.Sprintf("From: %s <%s>", commitObj.Author.Name, commitObj.Author.Email)
	date := fmt.Sprintf("Date: %s", commitObj.Author.When.Format(commitFormatDate))
	subject := fmt.Sprintf("Subject: [PATCH] %s", commitObj.Message)

	stats, err := commitObj.Stats()
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	fmt.Fprintf(w, "%s\n%s\n%s\n%s\n---\n%s\n%s", commitHashStr, from, date, subject, stats.String(), patch)
}

func (sc *Smithy) WriteGitToHTTP(w http.ResponseWriter, r *http.Request, gitCommand GitCommand) error {
	cmd := exec.CommandContext(r.Context(), "git", gitCommand.args...)
	log.Printf("WriteGitToHttp: %v", cmd)
	if gitCommand.procInput != nil {
		cmd.Stdin = gitCommand.procInput
	}
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (sc *Smithy) getInfoRefs(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		http.NotFound(w, r)
		return
	}
	log.Printf("getInfoRefs for %s", repo.Path)
	service := r.URL.Query().Get("service")
	var serviceName string
	switch service {
	case "git-upload-pack":
		serviceName = "upload-pack"
	case "git-receive-pack":
		sc.RequireWriteAuth(sc.getReceivePackInfoRefs)(w, r)
		return
	default:
		http.Error(w, "unsupported git service", http.StatusBadRequest)
		return
	}
	sc.writeInfoRefs(w, r, repo, service, serviceName)
}

func (sc *Smithy) getReceivePackInfoRefs(w http.ResponseWriter, r *http.Request) {
	repo, exists := sc.FindRepo(sc.GetParam(r, "repo"))
	if !exists {
		http.NotFound(w, r)
		return
	}
	sc.writeInfoRefs(w, r, repo, "git-receive-pack", "receive-pack")
}

func (sc *Smithy) writeInfoRefs(w http.ResponseWriter, r *http.Request, repo RepositoryWithName, service, serviceName string) {
	w.Header().Set("Content-Type", "application/x-git-"+serviceName+"-advertisement")
	str := "# service=" + service
	fmt.Fprintf(w, "%.4x%s\n", len(str)+offset, str)
	fmt.Fprintf(w, "0000")
	c := GitCommand{
		args: []string{serviceName, "--stateless-rpc", "--advertise-refs", repo.Path},
	}
	if err := sc.WriteGitToHTTP(w, r, c); err != nil {
		log.Printf("info refs failed: %v", err)
	}
}

func (sc *Smithy) uploadPack(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		http.NotFound(w, r)
		return
	}
	log.Printf("uploadPack for %s", repo.Path)
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	c := GitCommand{
		procInput: r.Body,
		args:      []string{"upload-pack", "--stateless-rpc", repo.Path},
	}
	if err := sc.WriteGitToHTTP(w, r, c); err != nil {
		log.Printf("upload-pack failed: %v", err)
	}
}

func (sc *Smithy) receivePack(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		sc.Error(w, http.StatusNotFound, fmt.Errorf("Repository not found"))
		return
	}
	log.Printf("receivePack for %s", repo.Path)
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	c := GitCommand{
		procInput: r.Body,
		args:      []string{"receive-pack", "--stateless-rpc", repo.Path},
	}
	if err := sc.WriteGitToHTTP(w, r, c); err != nil {
		log.Printf("receive-pack failed: %v", err)
	}
}
