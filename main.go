package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	var address, port, writeToken string
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Projects")
	flag.StringVar(&root, "root", root, "repos root dir")
	flag.StringVar(&address, "address", "127.0.0.1", "listen address")
	flag.StringVar(&port, "port", "3456", "listen port")
	flag.StringVar(&writeToken, "write-token", os.Getenv("SMITHY_WRITE_TOKEN"), "token required for repository writes (or SMITHY_WRITE_TOKEN)")
	flag.Parse()

	sc := NewSmithy(root)
	sc.WriteToken = writeToken
	if err := sc.LoadTemplates(); err != nil {
		log.Fatal(err)
	}
	if err := sc.LoadAllRepositories(); err != nil {
		log.Fatal(err)
	}

	routes := []Route{
		{method: http.MethodGet, pattern: r(`^/$`), handler: sc.IndexView},
		{method: http.MethodGet, pattern: r(`^/new$`), handler: sc.NewProject},
		{method: http.MethodPost, pattern: r(`^/new$`), handler: sc.RequireWriteAuth(sc.NewProject)},
		{method: http.MethodGet, pattern: r(`^/import$`), handler: sc.ImportProject},
		{method: http.MethodPost, pattern: r(`^/import$`), handler: sc.RequireWriteAuth(sc.ImportProject)},
		{method: http.MethodPost, pattern: r(`^/reload$`), handler: sc.RequireWriteAuth(sc.Reload)},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)$`), handler: sc.RepoView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/refs$`), handler: sc.RefsView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/log$`), handler: sc.LogView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/log/(?P<ref>[^/]+)?$`), handler: sc.LogView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/patch/(?P<hash>[^/]+)$`), handler: sc.PatchView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/commit/(?P<hash>[^/]+)$`), handler: sc.CommitView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/tree$`), handler: sc.TreeView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/tree/(?P<ref>[^/]+)$`), handler: sc.TreeView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/tree/(?P<ref>[^/]+)?/(?P<path>.*)$`), handler: sc.TreeView},
		{method: http.MethodGet, pattern: r(`^/(?P<repo>[^/]+)/info/refs$`), handler: sc.getInfoRefs},
		{method: http.MethodPost, pattern: r(`^/(?P<repo>[^/]+)/git-upload-pack$`), handler: sc.uploadPack},
		{method: http.MethodPost, pattern: r(`^/(?P<repo>[^/]+)/git-receive-pack$`), handler: sc.RequireWriteAuth(sc.receivePack)},
	}

	router := NewRouter(routes)
	server := &http.Server{
		Addr:              net.JoinHostPort(address, port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("Smithy listening on http://%s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
