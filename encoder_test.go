package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestPatchHTMLEscapesRepositoryControlledContent(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	name := `<img src=x onerror=alert(1)>.txt`
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	first, err := worktree.Commit("first", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<script>alert(1)</script>\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	second, err := worktree.Commit("second", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := repo.CommitObject(first)
	after, _ := repo.CommitObject(second)
	patch, err := before.Patch(after)
	if err != nil {
		t.Fatal(err)
	}

	html := PatchHTML(*patch)
	if strings.Contains(html, "<img") || strings.Contains(html, "<script>") {
		t.Fatalf("patch contains unescaped repository content: %s", html)
	}
	if !strings.Contains(html, "&lt;img") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("patch does not contain expected escaped content: %s", html)
	}
}
