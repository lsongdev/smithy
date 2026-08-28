package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	request = request.WithContext(newContextWithParams(request.Context(), map[string]string{"repo": "repo.git"}))
	response := httptest.NewRecorder()
	sc.getInfoRefs(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
