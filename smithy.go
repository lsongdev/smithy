package main

import (
	"os"
	"path"
	"sort"

	"github.com/go-git/go-git/v5"
)

type Smithy struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	repos       map[string]RepositoryWithName
}

func NewSmithy() Smithy {
	return Smithy{
		Title:       "Liu Song’s Projects",
		Description: "Publish your git repositories with ease",
		Port:        3456,
		Host:        "localhost",
	}
}

func (sc *Smithy) LoadAllRepositories(root string) error {
	files, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	sc.repos = make(map[string]RepositoryWithName)
	for _, f := range files {
		repoPath := path.Join(root, f.Name())
		r, err := git.PlainOpen(repoPath)
		if err != nil {
			// Ignore directories that aren't git repositories
			continue
		}
		key := f.Name()
		rwn := RepositoryWithName{Name: f.Name(), Repository: r}
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
