package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterMatchesMethodAndReportsAllowedMethods(t *testing.T) {
	router := NewRouter([]Route{
		{method: http.MethodGet, pattern: r(`^/resource$`), handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }},
		{method: http.MethodPost, pattern: r(`^/resource$`), handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }},
	})

	request := httptest.NewRequest(http.MethodPut, "/resource", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != "GET, POST" {
		t.Fatalf("Allow = %q, want %q", got, "GET, POST")
	}
}

func TestRouterRequiresFullPathMatch(t *testing.T) {
	router := NewRouter([]Route{{
		method:  http.MethodGet,
		pattern: r(`^/commit/(?P<hash>[^/]+)$`),
		handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	}})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/commit/abc/extra", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
