package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
)

func TestRequireWriteAuth(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	handler := sc.RequireWriteAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodPost, "/new", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("without configured token: status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	sc.WriteToken = "secret"
	response = httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodPost, "/new", nil))
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("without credentials: status = %d, challenge = %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}

	request := httptest.NewRequest(http.MethodPost, "/new", nil)
	request.SetBasicAuth("git", "secret")
	response = httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("with valid credentials: status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestTemplatesLoadWithQueryEscapingHelpers(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	if err := sc.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	sc.IndexView(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestRenderDoesNotWritePartialTemplateOutput(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	sc.template = template.Must(template.New("broken.html").Parse(`partial {{.Missing}}`))
	response := httptest.NewRecorder()

	sc.Render(response, "broken", struct{}{})

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "partial") {
		t.Fatalf("response contains partial template output: %q", response.Body.String())
	}
}

func TestInfoRefsRejectsUnknownServiceBeforeStartingGit(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	repoPath := filepath.Join(sc.Root, "repo.git")
	repo, err := git.PlainInit(repoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	sc.AddRepository(RepositoryWithName{Name: "repo.git", Path: repoPath, Repository: repo})

	request := httptest.NewRequest(http.MethodGet, "/repo.git/info/refs?service=git-shell", nil)
	request.SetPathValue("repo", "repo.git")
	response := httptest.NewRecorder()
	sc.getInfoRefs(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestInfoRefsReturnsServerErrorWhenGitCannotStart(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	sc.GitExecutable = filepath.Join(t.TempDir(), "missing-git")
	repoPath := filepath.Join(sc.Root, "repo.git")
	repo, err := git.PlainInit(repoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	sc.AddRepository(RepositoryWithName{Name: "repo.git", Path: repoPath, Repository: repo})
	request := httptest.NewRequest(http.MethodGet, "/repo.git/info/refs?service=git-upload-pack", nil)
	request.SetPathValue("repo", "repo.git")
	response := httptest.NewRecorder()

	sc.getInfoRefs(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
