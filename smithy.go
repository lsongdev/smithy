package main

import (
	"os"
	"path"
	"sort"

	"github.com/go-git/go-git/v5"
)

type Smithy struct {
	Root        string
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	repos       map[string]RepositoryWithName
}

func NewSmithy(root string) Smithy {
	return Smithy{
		Root:        root,
		Title:       "Liu Song’s Projects",
		Description: "Publish your git repositories with ease",
	}
}

func (sc *Smithy) LoadAllRepositories() error {
	files, err := os.ReadDir(sc.Root)
	if err != nil {
		return err
	}
	sc.repos = make(map[string]RepositoryWithName)
	for _, f := range files {
		repoPath := path.Join(sc.Root, f.Name())
		r, err := git.PlainOpen(repoPath)
		if err != nil {
			// Ignore directories that aren't git repositories
			continue
		}
		key := f.Name()
		rwn := RepositoryWithName{Name: f.Name(), Repository: r, Path: repoPath}
		sc.repos[key] = rwn
	}
	return nil
}

func (sc *Smithy) GetRepositories() []RepositoryWithName {
	var repos []RepositoryWithName
	for _, repo := range sc.repos {
		repos = append(repos, repo)
	}
	sort.Sort(RepositoryByName(repos))
	return repos
}

func (sc *Smithy) FindRepo(slug string) (RepositoryWithName, bool) {
	value, exists := sc.repos[slug]
	return value, exists
}
