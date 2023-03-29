// smithy --- the git forge
// Copyright (C) 2020   Honza Pokorny <honza@pokorny.ca>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"os"
	"path"
	"sort"

	"github.com/go-git/go-git/v5"
)

// type RepoConfig struct {
// 	Path        string
// 	Slug        string
// 	Title       string
// 	Description string
// 	Exclude     bool
// }

type GitConfig struct {
	Root string `yaml:"root"`
	// Repos []RepoConfig `yaml:",omitempty"`
	// ReposBySlug is an extrapolaed value
	reposBySlug map[string]RepositoryWithName

	// staticReposBySlug is a map of the `repos` values
	// staticReposBySlug map[string]RepoConfig
}

type SmithyConfig struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Git         GitConfig
	Templates   struct {
		Dir string
	}
}

// func (sc *SmithyConfig) findStaticRepo(slug string) (RepoConfig, bool) {
// 	value, exists := sc.Git.staticReposBySlug[slug]
// 	return value, exists
// }

func (sc *SmithyConfig) FindRepo(slug string) (RepositoryWithName, bool) {
	value, exists := sc.Git.reposBySlug[slug]
	return value, exists
}

func (sc *SmithyConfig) GetRepositories() []RepositoryWithName {
	var repos []RepositoryWithName
	for _, repo := range sc.Git.reposBySlug {
		repos = append(repos, repo)
	}
	sort.Sort(RepositoryByName(repos))
	return repos
}

func (sc *SmithyConfig) LoadAllRepositories() error {
	files, err := os.ReadDir(sc.Git.Root)
	if err != nil {
		return err
	}
	sc.Git.reposBySlug = make(map[string]RepositoryWithName)
	for _, f := range files {
		repoPath := path.Join(sc.Git.Root, f.Name())
		r, err := git.PlainOpen(repoPath)
		if err != nil {
			// Ignore directories that aren't git repositories
			continue
		}
		key := f.Name()
		rwn := RepositoryWithName{Name: f.Name(), Repository: r}
		sc.Git.reposBySlug[key] = rwn
	}
	return nil
}

func NewConfig() SmithyConfig {
	return SmithyConfig{
		Title:       "Liu Song’s Projects",
		Description: "Publish your git repositories with ease",
		Port:        3456,
		Host:        "localhost",
		Git: GitConfig{
			Root: "/Users/Lsong/Projects",
		},
	}
}
