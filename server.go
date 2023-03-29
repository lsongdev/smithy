package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
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
	procInput *bytes.Reader
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
	sc.LoadAllRepositories()
	fmt.Fprintf(w, "done")
}

func (sc *Smithy) IndexView(w http.ResponseWriter, r *http.Request) {
	repos := sc.GetRepositories()
	sc.Render(w, "index", H{
		"Repos": repos,
	})
}

func (sc *Smithy) NewProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sc.Render(w, "new", H{})
		return
	}
	r.ParseForm()
	repoName := r.FormValue("name")
	repoPath := filepath.Join(sc.Root, repoName)
	_, err := git.PlainInit(repoPath, true)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
	}
	fmt.Fprint(w, repoName)
}

func (sc *Smithy) ImportProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sc.Render(w, "import", H{})
		return
	}
	r.ParseForm()
	name := r.FormValue("name")
	bare := r.FormValue("bare")
	address := r.FormValue("git")
	repoPath := filepath.Join(sc.Root, name)
	isBare := bare == "on"
	repo, err := git.PlainClone(repoPath, isBare, &git.CloneOptions{
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
	sc.Reload(w, r)
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
	refName := sc.GetParam(r, "ref")
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

	treePath := sc.GetParam(r, "path")
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

	refName := sc.GetParam(r, "ref")
	if refName == "" {
		defaultBranchName, _, err := FindMainBranch(repo.Repository)
		if err != nil {
			sc.Error(w, http.StatusInternalServerError, err)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/log/%s", repoName, defaultBranchName), http.StatusFound)
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

func (sc *Smithy) WriteGitToHttp(w http.ResponseWriter, gitCommand GitCommand) {
	cmd := exec.Command("git", gitCommand.args...)
	stdout, err := cmd.StdoutPipe()
	log.Printf("WriteGitToHttp: %v", cmd)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}

	if gitCommand.procInput != nil {
		cmd.Stdin = gitCommand.procInput
	}

	if err := cmd.Start(); err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	nbytes, err := io.Copy(w, stdout)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, fmt.Errorf("Error writing to socket: %v", err))
	} else {
		log.Printf("Bytes written: %d", nbytes)
	}
}

func (sc *Smithy) getInfoRefs(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, _ := sc.FindRepo(repoName)
	log.Printf("getInfoRefs for %s", repo.Path)
	service := r.URL.Query().Get("service")
	serviceName := strings.Replace(service, "git-", "", 1)
	w.Header().Set("Content-Type", "application/x-git-"+serviceName+"-advertisement")
	str := "# service=git-" + serviceName
	fmt.Fprintf(w, "%.4x%s\n", len(str)+offset, str)
	fmt.Fprintf(w, "0000")
	c := GitCommand{
		args: []string{serviceName, "--stateless-rpc", "--advertise-refs", repo.Path},
	}
	sc.WriteGitToHttp(w, c)
}

func (sc *Smithy) uploadPack(w http.ResponseWriter, r *http.Request) {
	repoName := sc.GetParam(r, "repo")
	repo, _ := sc.FindRepo(repoName)
	log.Printf("uploadPack for %s", repo.Path)
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	c := GitCommand{
		procInput: bytes.NewReader(requestBody),
		args:      []string{"upload-pack", "--stateless-rpc", repo.Path},
	}
	sc.WriteGitToHttp(w, c)
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
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		sc.Error(w, http.StatusInternalServerError, err)
		return
	}
	c := GitCommand{
		procInput: bytes.NewReader(requestBody),
		args:      []string{"receive-pack", "--stateless-rpc", repo.Path},
	}
	sc.WriteGitToHttp(w, c)
}
