package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
)

const (
	packetLineOffset = 5
	maxGitStderr     = 16 << 10
)

type GitCommand struct {
	procInput io.Reader
	args      []string
}

type limitedBuffer struct {
	buf       bytes.Buffer
	remaining int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	_, _ = b.buf.Write(p)
	b.remaining -= len(p)
	return original, nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }

func (sc *Smithy) gitRepository(w http.ResponseWriter, r *http.Request) (RepositoryWithName, bool) {
	repo, ok := sc.FindRepo(r.PathValue("repo"))
	if !ok {
		http.NotFound(w, r)
	}
	return repo, ok
}

func (sc *Smithy) writeGitResponse(w http.ResponseWriter, r *http.Request, contentType string, prefix []byte, command GitCommand) {
	cmd := exec.CommandContext(r.Context(), sc.GitExecutable, command.args...)
	if command.procInput != nil {
		cmd.Stdin = command.procInput
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	stderr := &limitedBuffer{remaining: maxGitStderr}
	cmd.Stderr = stderr
	slog.Debug("run Git command", "executable", sc.GitExecutable, "args", strings.Join(command.args, " "))
	if err := cmd.Start(); err != nil {
		slog.Error("start Git command", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if len(prefix) > 0 {
		_, _ = w.Write(prefix)
	}
	_, copyErr := io.Copy(w, stdout)
	waitErr := cmd.Wait()
	if copyErr != nil {
		slog.Error("stream Git response", "error", copyErr)
	}
	if waitErr != nil {
		slog.Error("Git command failed", "error", waitErr, "stderr", strings.TrimSpace(stderr.String()))
	}
}

func (sc *Smithy) getInfoRefs(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.gitRepository(w, r)
	if !ok {
		return
	}
	service := r.URL.Query().Get("service")
	switch service {
	case "git-upload-pack":
		sc.writeInfoRefs(w, r, repo, service, "upload-pack")
	case "git-receive-pack":
		sc.RequireWriteAuth(func(w http.ResponseWriter, r *http.Request) {
			sc.writeInfoRefs(w, r, repo, service, "receive-pack")
		})(w, r)
	default:
		http.Error(w, "unsupported git service", http.StatusBadRequest)
	}
}

func (sc *Smithy) writeInfoRefs(w http.ResponseWriter, r *http.Request, repo RepositoryWithName, service, command string) {
	line := "# service=" + service
	prefix := []byte(fmt.Sprintf("%.4x%s\n0000", len(line)+packetLineOffset, line))
	sc.writeGitResponse(w, r, "application/x-git-"+command+"-advertisement", prefix, GitCommand{
		args: []string{command, "--stateless-rpc", "--advertise-refs", repo.Path},
	})
}

func (sc *Smithy) uploadPack(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.gitRepository(w, r)
	if !ok {
		return
	}
	sc.writeGitResponse(w, r, "application/x-git-upload-pack-result", nil, GitCommand{
		procInput: r.Body,
		args:      []string{"upload-pack", "--stateless-rpc", repo.Path},
	})
}

func (sc *Smithy) receivePack(w http.ResponseWriter, r *http.Request) {
	repo, ok := sc.gitRepository(w, r)
	if !ok {
		return
	}
	sc.writeGitResponse(w, r, "application/x-git-receive-pack-result", nil, GitCommand{
		procInput: r.Body,
		args:      []string{"receive-pack", "--stateless-rpc", repo.Path},
	})
}
