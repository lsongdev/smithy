package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

//go:embed templates
var templateFiles embed.FS

type repositoryPage struct {
	RepoName string
	Commit   *object.Commit
}

type indexPage struct {
	Repos []RepositoryWithName
}

type repoPage struct {
	repositoryPage
	Branches       []*plumbing.Reference
	Tags           []*plumbing.Reference
	Readme         template.HTML
	ReadmeTooLarge bool
	Repo           RepositoryWithName
}

type refsPage struct {
	repositoryPage
	Branches []*plumbing.Reference
	Tags     []*plumbing.Reference
}

type treePage struct {
	repositoryPage
	ParentPath string
	RefName    string
	SubTree    string
	Path       string
	Files      []object.TreeEntry
}

type blobPage struct {
	repositoryPage
	RefName    string
	File       *object.TreeEntry
	ParentPath string
	Path       string
	Contents   string
	TooLarge   bool
}

type logPage struct {
	repositoryPage
	RefName   string
	Commits   []Commit
	Truncated bool
	Limit     int
}

type commitPage struct {
	repositoryPage
	Changes      template.HTML
	DiffTooLarge bool
}

type errorPage struct {
	Status string
	Error  string
}

func (sc *Smithy) LoadTemplates() error {
	t, err := template.New("").Option("missingkey=error").ParseFS(templateFiles, "templates/layout.html")
	if err != nil {
		return err
	}
	files, err := fs.Glob(templateFiles, "templates/*.html")
	if err != nil {
		return err
	}
	for _, file := range files {
		if file == "templates/layout.html" {
			continue
		}
		data, err := fs.ReadFile(templateFiles, file)
		if err != nil {
			return err
		}
		if _, err := t.Parse(string(data)); err != nil {
			return err
		}
	}
	sc.template = t
	return nil
}

func (sc *Smithy) Render(w http.ResponseWriter, name string, data any) {
	if err := sc.render(w, http.StatusOK, name, data); err != nil {
		slog.Error("render template", "template", name, "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (sc *Smithy) Error(w http.ResponseWriter, code int, err error) {
	message := err.Error()
	if code >= http.StatusInternalServerError {
		slog.Error("request failed", "status", code, "error", err)
		message = http.StatusText(code)
	}
	data := errorPage{Status: fmt.Sprintf("%d %s", code, http.StatusText(code)), Error: message}
	if renderErr := sc.render(w, code, "error", data); renderErr != nil {
		slog.Error("render error page", "error", renderErr)
		http.Error(w, message, code)
	}
}

func (sc *Smithy) render(w http.ResponseWriter, status int, name string, data any) error {
	if sc.template == nil {
		return fmt.Errorf("templates are not loaded")
	}
	t, err := sc.template.Clone()
	if err != nil {
		return err
	}
	pageData, err := templateFiles.ReadFile("templates/" + name + ".html")
	if err != nil {
		return err
	}
	if _, err := t.Parse(string(pageData)); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}
