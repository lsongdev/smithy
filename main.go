package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	var address, port string
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Projects")
	flag.StringVar(&root, "root", root, "repos root dir")
	flag.StringVar(&address, "address", "127.0.0.1", "listen address")
	flag.StringVar(&port, "port", "3456", "listen port")
	flag.Parse()
	if err := os.MkdirAll(root, 0o750); err != nil {
		slog.Error("create repository root", "error", err)
		os.Exit(1)
	}

	sc := NewSmithy(root)
	sc.WriteToken = os.Getenv("SMITHY_WRITE_TOKEN")
	if sc.WriteToken != "" && !isLoopbackAddress(address) {
		slog.Warn("write authentication is served over plain HTTP on a non-loopback address; use a TLS reverse proxy", "address", address)
	}
	if err := sc.LoadTemplates(); err != nil {
		slog.Error("load templates", "error", err)
		os.Exit(1)
	}
	if err := sc.LoadAllRepositories(); err != nil {
		slog.Error("load repositories", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              net.JoinHostPort(address, port),
		Handler:           NewHandler(sc),
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	slog.Info("Smithy listening", "address", server.Addr)
	if err := serve(server); err != nil {
		slog.Error("serve Smithy", "error", err)
		os.Exit(1)
	}
}

func isLoopbackAddress(address string) bool {
	if address == "localhost" {
		return true
	}
	ip := net.ParseIP(address)
	return ip != nil && ip.IsLoopback()
}

func NewHandler(sc *Smithy) http.Handler {
	return http.NewCrossOriginProtection().Handler(NewMux(sc))
}

func serve(server *http.Server) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	err := <-errCh
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func NewMux(sc *Smithy) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", sc.IndexView)
	mux.HandleFunc("GET /new", sc.NewProject)
	mux.HandleFunc("POST /new", sc.RequireWriteAuth(sc.NewProject))
	mux.HandleFunc("GET /import", sc.ImportProject)
	mux.HandleFunc("POST /import", sc.RequireWriteAuth(sc.ImportProject))
	mux.HandleFunc("POST /reload", sc.RequireWriteAuth(sc.Reload))
	mux.HandleFunc("GET /{repo}", sc.RepoView)
	mux.HandleFunc("GET /{repo}/refs", sc.RefsView)
	mux.HandleFunc("GET /{repo}/log", sc.LogView)
	mux.HandleFunc("GET /{repo}/log/{ref}", sc.LogView)
	mux.HandleFunc("GET /{repo}/patch/{hash}", sc.PatchView)
	mux.HandleFunc("GET /{repo}/commit/{hash}", sc.CommitView)
	mux.HandleFunc("GET /{repo}/tree", sc.TreeView)
	mux.HandleFunc("GET /{repo}/tree/{ref}", sc.TreeView)
	mux.HandleFunc("GET /{repo}/tree/{ref}/{path...}", sc.TreeView)
	mux.HandleFunc("GET /{repo}/info/refs", sc.getInfoRefs)
	mux.HandleFunc("POST /{repo}/git-upload-pack", sc.uploadPack)
	mux.HandleFunc("POST /{repo}/git-receive-pack", sc.RequireWriteAuth(sc.receivePack))
	return mux
}
