package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func testRepository(t *testing.T, sc *Smithy, name, fileName string, contents []byte) (RepositoryWithName, string) {
	t.Helper()
	dir := filepath.Join(sc.Root, name)
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(fileName); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	rwn := RepositoryWithName{Name: name, Path: dir, Repository: repo}
	sc.AddRepository(rwn)
	return rwn, hash.String()
}

func TestTreeViewDoesNotLoadOversizedBlob(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	if err := sc.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	_, hash := testRepository(t, sc, "repo", "large.txt", bytes.Repeat([]byte("x"), int(maxBlobBytes)+1))
	request := httptest.NewRequest(http.MethodGet, "/repo/tree?ref="+hash+"&path=large.txt", nil)
	response := httptest.NewRecorder()

	NewMux(sc).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "File is too large to display") {
		t.Fatalf("response does not report oversized file")
	}
}

func TestCommitViewReturnsNotFoundForInvalidRevision(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	if err := sc.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	testRepository(t, sc, "repo", "README", []byte("test"))
	response := httptest.NewRecorder()

	NewMux(sc).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repo/commit/not-a-hash", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestRepositoryViewsRenderWithTypedData(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	if err := sc.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	_, hash := testRepository(t, sc, "repo", "README.md", []byte("# Test\n"))
	paths := []string{
		"/repo",
		"/repo/refs",
		"/repo/tree?ref=" + hash,
		"/repo/log?ref=" + hash,
		"/repo/commit/" + hash,
	}
	for _, requestPath := range paths {
		response := httptest.NewRecorder()
		NewMux(sc).ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if response.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want %d", requestPath, response.Code, http.StatusOK)
		}
	}
}

func TestEmptyRepositoryPageRenders(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	if err := sc.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(sc.Root, "empty.git")
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	sc.AddRepository(RepositoryWithName{Name: "empty.git", Path: dir, Repository: repo})
	response := httptest.NewRecorder()

	NewMux(sc).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/empty.git", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestLogLimitValidation(t *testing.T) {
	for _, test := range []struct {
		query string
		want  int
		ok    bool
	}{
		{"", defaultLogLimit, true},
		{"?limit=1", 1, true},
		{"?limit=100", maxLogLimit, true},
		{"?limit=0", 0, false},
		{"?limit=101", 0, false},
		{"?limit=nope", 0, false},
	} {
		request := httptest.NewRequest(http.MethodGet, "/repo/log"+test.query, nil)
		got, err := logLimit(request)
		if (err == nil) != test.ok || got != test.want {
			t.Errorf("query %q: got (%d, %v), want (%d, ok=%v)", test.query, got, err, test.want, test.ok)
		}
	}
}

func TestAdminFormBodyLimit(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	request := httptest.NewRequest(http.MethodPost, "/new", strings.NewReader("name="+strings.Repeat("x", maxAdminFormBytes)))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	sc.NewProject(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}
