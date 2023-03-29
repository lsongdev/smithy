package main

import (
	"flag"
	"net/http"
	"os"
	"path"
)

func main() {
	var port string
	home, _ := os.UserHomeDir()
	root := path.Join(home, "Projects")
	flag.StringVar(&root, "root", root, "repos root dir")
	flag.StringVar(&port, "port", "3456", "listen port")
	flag.Parse()

	sc := NewSmithy(root)
	sc.LoadTemplates()
	sc.LoadAllRepositories()

	routes := []Route{
		{pattern: r(`^/$`), handler: sc.IndexView},
		{pattern: r(`^/new$`), handler: sc.NewProject},
		{pattern: r(`^/import$`), handler: sc.ImportProject},
		{pattern: r(`^/reload$`), handler: sc.Reload},
		{pattern: r(`^/(?P<repo>[^/]+)$`), handler: sc.RepoView},
		{pattern: r(`^/(?P<repo>[^/]+)/refs$`), handler: sc.RefsView},
		{pattern: r(`^/(?P<repo>[^/]+)/log$`), handler: sc.LogView},
		{pattern: r(`^/(?P<repo>[^/]+)/log/(?P<ref>[^/]+)?$`), handler: sc.LogView},
		{pattern: r(`^/(?P<repo>[^/]+)/patch/(?P<hash>[^/]+)$`), handler: sc.PatchView},
		{pattern: r(`^/(?P<repo>[^/]+)/commit/(?P<hash>[^/]+)`), handler: sc.CommitView},
		{pattern: r(`^/(?P<repo>[^/]+)/tree$`), handler: sc.TreeView},
		{pattern: r(`^/(?P<repo>[^/]+)/tree/(?P<ref>[^/]+)$`), handler: sc.TreeView},
		{pattern: r(`^/(?P<repo>[^/]+)/tree/(?P<ref>[^/]+)?/(?P<path>.*)`), handler: sc.TreeView},
		{pattern: r(`^/(?P<repo>[^/]+)/info/refs$`), handler: sc.getInfoRefs},
		{pattern: r(`^/(?P<repo>[^/]+)/git-upload-pack$`), handler: sc.uploadPack},
		{pattern: r(`^/(?P<repo>[^/]+)/git-receive-pack$`), handler: sc.receivePack},
	}

	router := NewRouter(routes)
	http.ListenAndServe(":"+port, router)
}
