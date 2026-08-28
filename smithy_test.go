package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestRepositoryPathRejectsTraversalAndInvalidNames(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	invalid := []string{"", ".", "..", "../outside", "nested/repo", `nested\\repo`, "repo name"}
	for _, name := range invalid {
		if _, err := sc.RepositoryPath(name); err == nil {
			t.Errorf("RepositoryPath(%q) unexpectedly succeeded", name)
		}
	}

	got, err := sc.RepositoryPath("valid_repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(sc.Root, "valid_repo.git"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestFindMainBranchUsesSymbolicHEAD(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("README"); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	branch := plumbing.NewBranchReferenceName("feature/default")
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branch, hash)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branch)); err != nil {
		t.Fatal(err)
	}

	name, revision, err := FindMainBranch(repo)
	if err != nil {
		t.Fatal(err)
	}
	if name != "feature/default" || *revision != hash {
		t.Fatalf("got (%q, %s), want (%q, %s)", name, revision, "feature/default", hash)
	}
}

func TestRepositorySnapshotSupportsConcurrentReloadAndRead(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	if _, err := git.PlainInit(filepath.Join(sc.Root, "repo.git"), true); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			if err := sc.LoadAllRepositories(); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
		}
	}()
	for i := 0; i < 1000; i++ {
		_ = sc.GetRepositories()
		_, _ = sc.FindRepo("repo.git")
	}
	<-done
}
