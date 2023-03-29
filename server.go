package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/song940/gan"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
)

//go:embed templates
var templatefiles embed.FS

const PAGE_SIZE int = 500

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

type Commit struct {
	Commit    *object.Commit
	Subject   string
	ShortHash string
}

func (c *Commit) FormattedDate() string {
	return c.Commit.Author.When.Format("2006-01-02")
	// return c.Commit.Author.When.Format(time.RFC822)
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
		"README.md",
		"README",
		"README.markdown",
		"readme.md",
		"readme.markdown",
		"readme",
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

func RenderSyntaxHighlighting(file *object.File) (string, error) {
	contents, err := file.Contents()
	if err != nil {
		return "", err
	}
	lexer := lexers.Match(file.Name)
	if lexer == nil {
		// If the lexer is nil, we weren't able to find one based on the file
		// extension.  We can render it as plain text.
		return fmt.Sprintf("<pre>%s</pre>", contents), nil
	}

	style := styles.Get("autumn")

	if style == nil {
		style = styles.Fallback
	}

	formatter := html.New(
		html.WithClasses(true),
		html.WithLineNumbers(true),
		html.LineNumbersInTable(true),
		html.LinkableLineNumbers(true, "L"),
	)

	iterator, err := lexer.Tokenise(nil, contents)
	if err != nil {
		return "", err
	}

	buf := bytes.NewBuffer(nil)
	err = formatter.Format(buf, style, iterator)

	if err != nil {
		return fmt.Sprintf("<pre>%s</pre>", contents), nil
	}

	return buf.String(), nil
}

func Http404(ctx *gan.Context) {
	ctx.Response().WithStatus(404).Text("Not Found")
}

func Http500(ctx *gan.Context) {
	ctx.Response().WithStatus(500).Text("Error")
}

func (sc *Smithy) IndexView(ctx *gan.Context) {
	repos := sc.GetRepositories()
	sc.Render(ctx, "index", gan.H{
		"Repos": repos,
	})
}

func findMainBranch(repo *git.Repository) (string, *plumbing.Hash, error) {
	branches, _ := ListBranches(repo)
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

func (sc *Smithy) NewProjectView(ctx *gan.Context) {
	sc.Render(ctx, "new", gan.H{})
}

func (sc *Smithy) NewProject(ctx *gan.Context) {
	repoName := ctx.Request().GetFormValue("name")
	repoPath := filepath.Join(sc.Root, repoName)
	_, err := git.PlainInit(repoPath, true)
	if err != nil {
		Http500(ctx)
	}
	ctx.Response().Text(repoName)
}

func (sc *Smithy) RepoGit(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	log.Println("RepoGit", repoName)
}

func (sc *Smithy) RepoView(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		Http404(ctx)
		return
	}

	branches, err := ListBranches(repo.Repository)
	if err != nil {
		Http500(ctx)
		return
	}

	tags, err := ListTags(repo.Repository)
	if err != nil {
		Http500(ctx)
		return
	}

	main, revision, err := findMainBranch(repo.Repository)
	if err != nil {
		Http500(ctx)
		return
	}
	log.Printf(`%s default branch is "%s"`, repoName, main)
	commitObj, err := repo.Repository.CommitObject(*revision)
	if err != nil {
		Http500(ctx)
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

	sc.Render(ctx, "repo", gan.H{
		"RepoName": repoName,
		"Branches": branches,
		"Tags":     tags,
		"Readme":   template.HTML(formattedReadme),
		"Repo":     repo,
	})
}

func (sc *Smithy) RefsView(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		Http404(ctx)
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

	sc.Render(ctx, "refs", map[string]any{
		"RepoName": repoName,
		"Branches": branches,
		"Tags":     tags,
	})
}

func (sc *Smithy) TreeView(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		Http404(ctx)
		return
	}

	var err error
	refNameString := ctx.GetParam("ref")
	if refNameString == "" {
		refNameString, _, err = findMainBranch(repo.Repository)
		if err != nil {
			ctx.Response().RenderError(err)
			Http404(ctx)
			return
		}
	}

	revision, err := repo.Repository.ResolveRevision(plumbing.Revision(refNameString))
	if err != nil {
		Http404(ctx)
		return
	}

	treePath := ctx.GetParam("path")
	parentPath := filepath.Dir(treePath)
	commitObj, err := repo.Repository.CommitObject(*revision)

	if err != nil {
		Http404(ctx)
		return
	}

	tree, err := commitObj.Tree()

	if err != nil {
		Http404(ctx)
		return
	}

	// We're looking at the root of the project.  Show a list of files.
	if treePath == "" {
		sc.Render(ctx, "tree", gan.H{
			"RepoName": repoName,
			"RefName":  refNameString,
			"Files":    tree.Entries,
			"Path":     treePath,
		})
		return
	}

	out, err := tree.FindEntry(treePath)
	if err != nil {
		Http404(ctx)
		return
	}

	// We found a subtree.
	if !out.Mode.IsFile() {
		subTree, err := tree.Tree(treePath)
		if err != nil {
			Http404(ctx)
			return
		}

		sc.Render(ctx, "tree", map[string]any{
			"RepoName":   repoName,
			"ParentPath": parentPath,
			"RefName":    refNameString,
			"SubTree":    out.Name,
			"Path":       treePath,
			"Files":      subTree.Entries,
		})
		return
	}

	// Now do a regular file
	file, err := tree.File(treePath)
	if err != nil {
		Http404(ctx)
		return
	}
	contents, err := file.Contents()
	syntaxHighlighted, _ := RenderSyntaxHighlighting(file)
	if err != nil {
		Http404(ctx)
		return
	}
	sc.Render(ctx, "blob", map[string]any{
		"RepoName":            repoName,
		"RefName":             refNameString,
		"File":                out,
		"ParentPath":          parentPath,
		"Path":                treePath,
		"Contents":            contents,
		"ContentsHighlighted": template.HTML(syntaxHighlighted),
	})
}

func (sc *Smithy) LogView(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		Http404(ctx)
		return
	}

	refName := ctx.GetParam("ref")
	if refName == "" {
		defaultBranchName, _, err := findMainBranch(repo.Repository)
		if err != nil {
			ctx.Response().RenderError(err)
			Http404(ctx)
			return
		}
		path := ctx.Request().Path + "/" + defaultBranchName
		ctx.Response().Redirect(path, http.StatusTemporaryRedirect)
		return
	}

	revision, err := repo.Repository.ResolveRevision(plumbing.Revision(refName))
	if err != nil {
		Http404(ctx)
		return
	}

	var commits []Commit
	cIter, err := repo.Repository.Log(&git.LogOptions{From: *revision, Order: git.LogOrderCommitterTime})
	if err != nil {
		Http500(ctx)
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

	sc.Render(ctx, "log", gan.H{
		"RepoName": repoName,
		"RefName":  refName,
		"Commits":  commits,
	})
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

func (sc *Smithy) CommitView(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")

	repo, exists := sc.FindRepo(repoName)
	if !exists {
		Http404(ctx)
		return
	}

	commitID := ctx.GetParam("hash")
	if commitID == "" {
		Http404(ctx)
		return
	}
	commitHash := plumbing.NewHash(commitID)
	commitObj, err := repo.Repository.CommitObject(commitHash)
	if err != nil {
		Http404(ctx)
		return
	}

	changes, err := GetChanges(commitObj)
	if err != nil {
		Http404(ctx)
		return
	}

	formattedChanges, err := FormatChanges(changes)
	if err != nil {
		Http404(ctx)
		return
	}

	sc.Render(ctx, "commit", gan.H{
		"RepoName": repoName,
		"Commit":   commitObj,
		"Changes":  template.HTML(formattedChanges),
	})
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

func (sc *Smithy) PatchView(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, exists := sc.FindRepo(repoName)
	if !exists {
		Http404(ctx)
		return
	}

	commitID := ctx.GetParam("hash")

	if commitID == "" {
		Http404(ctx)
		return
	}

	commitHash := plumbing.NewHash(commitID)
	commitObj, err := repo.Repository.CommitObject(commitHash)
	if err != nil {
		Http404(ctx)
		return
	}

	// TODO: If this is the first commit, we can't build the diff (#281)
	// Therefore, we have two options: either build the diff manually or
	// patch go-git
	var patch string
	if commitObj.NumParents() == 0 {
		Http500(ctx)
		return
	} else {
		parentCommit, err := commitObj.Parent(0)

		if err != nil {
			Http500(ctx)
			return
		}

		patchObj, err := parentCommit.Patch(commitObj)
		if err != nil {
			Http500(ctx)
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
		Http500(ctx)
		return
	}

	ctx.Response().Text("%s\n%s\n%s\n%s\n---\n%s\n%s",
		commitHashStr, from, date, subject, stats.String(), patch)
}

func (sc *Smithy) Render(ctx *gan.Context, name string, data gan.H) {
	ctx.Response().Render(name+".html", data)
}

func loadTemplates() (*template.Template, error) {
	t := template.New("")
	files, err := templatefiles.ReadDir("templates")
	if err != nil {
		return t, err
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".html") {
			continue
		}
		f, err := templatefiles.Open("templates/" + file.Name())
		if err != nil {
			return t, err
		}
		contents, err := io.ReadAll(f)
		if err != nil {
			return t, err
		}

		_, err = t.New(file.Name()).Parse(string(contents))
		if err != nil {
			return t, err
		}
	}
	return t, nil
}

var (
	offset = 5
)

type GitCommand struct {
	procInput *bytes.Reader
	args      []string
}

func WriteGitToHttp(w http.ResponseWriter, gitCommand GitCommand) {
	cmd := exec.Command("git", gitCommand.args...)
	stdout, err := cmd.StdoutPipe()
	log.Printf("WriteGitToHttp: %v", cmd)
	if err != nil {
		w.WriteHeader(404)
		log.Fatal("Error:", err)
		return
	}

	if gitCommand.procInput != nil {
		cmd.Stdin = gitCommand.procInput
	}

	if err := cmd.Start(); err != nil {
		w.WriteHeader(404)
		log.Fatal("Error:", err)
		return
	}

	nbytes, err := io.Copy(w, stdout)
	if err != nil {
		log.Fatal("Error writing to socket", err)
	} else {
		log.Printf("Bytes written: %d", nbytes)
	}
}

func getServiceName(r *gan.Request) string {
	service := r.GetQueryValue("service")
	return strings.Replace(service, "git-", "", 1)
}

func (sc *Smithy) getInfoRefs(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, _ := sc.FindRepo(repoName)
	repoPath := repo.Path + ""
	// repoPath := "/tmp/repos/myapp"
	log.Printf("getInfoRefs for %s", repoPath)
	w := ctx.Response().GetRawResponse()
	serviceName := getServiceName(ctx.Request())
	log.Println("serviceName:", serviceName)
	w.Header().Set("Content-Type", "application/x-git-"+serviceName+"-advertisement")
	str := "# service=git-" + serviceName
	fmt.Fprintf(w, "%.4x%s\n", len(str)+offset, str)
	fmt.Fprintf(w, "0000")
	c := GitCommand{
		args: []string{serviceName, "--stateless-rpc", "--advertise-refs", repoPath},
	}
	WriteGitToHttp(w, c)
}

func (sc *Smithy) uploadPack(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, _ := sc.FindRepo(repoName)
	repoPath := repo.Path + ""
	// repoPath := "/tmp/repos/myapp"
	log.Printf("uploadPack for %s", repoPath)
	r := ctx.Request().GetRawRequest()
	w := ctx.Response().GetRawResponse()
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(404)
		log.Fatal("Error:", err)
		return
	}
	c := GitCommand{
		procInput: bytes.NewReader(requestBody),
		args:      []string{"upload-pack", "--stateless-rpc", repoPath},
	}
	WriteGitToHttp(w, c)
}

func (sc *Smithy) receivePack(ctx *gan.Context) {
	repoName := ctx.GetParam("repo")
	repo, _ := sc.FindRepo(repoName)
	repoPath := repo.Path + ""
	log.Printf("receivePack for %s", repoPath)
	r := ctx.Request().GetRawRequest()
	w := ctx.Response().GetRawResponse()
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")

	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(404)
		log.Fatal("Error:", err)
		return
	}
	c := GitCommand{
		procInput: bytes.NewReader(requestBody),
		args:      []string{"receive-pack", "--stateless-rpc", repoPath},
	}
	WriteGitToHttp(w, c)
}
