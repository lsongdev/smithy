package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMuxReportsMethodNotAllowed(t *testing.T) {
	mux := NewMux(NewSmithy(t.TempDir()))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/new", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	allow := response.Header().Get("Allow")
	if !strings.Contains(allow, http.MethodGet) || !strings.Contains(allow, http.MethodPost) {
		t.Fatalf("Allow = %q, want GET and POST", allow)
	}
}

func TestMuxDoesNotPrefixMatchCommitRoute(t *testing.T) {
	mux := NewMux(NewSmithy(t.TempDir()))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repo/commit/abc/extra", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestCrossOriginProtectionRejectsCrossSiteWrite(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	sc.WriteToken = "secret"
	request := httptest.NewRequest(http.MethodPost, "/reload", nil)
	request.SetBasicAuth("git", "secret")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()

	NewHandler(sc).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestCrossOriginProtectionAllowsNonBrowserGitStyleWrite(t *testing.T) {
	sc := NewSmithy(t.TempDir())
	sc.WriteToken = "secret"
	request := httptest.NewRequest(http.MethodPost, "/reload", nil)
	request.SetBasicAuth("git", "secret")
	response := httptest.NewRecorder()

	NewHandler(sc).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
