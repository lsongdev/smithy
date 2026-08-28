package main

import (
	"context"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

type ParamsType string

const (
	ParamsKey ParamsType = "params"
)

func r(s string) *regexp.Regexp {
	reg, _ := regexp.Compile(s)
	return reg
}

type Route struct {
	method  string
	pattern *regexp.Regexp
	handler http.HandlerFunc
}

type Router struct {
	routes []Route
}

func NewRouter(routes []Route) *Router {
	return &Router{routes: routes}
}

func newContextWithParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, ParamsKey, params)
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println(r.Method, r.URL.Path)
	allowed := make(map[string]struct{})
	for _, route := range router.routes {
		re := route.pattern
		match := re.FindStringSubmatch(r.URL.Path)
		if len(match) > 0 {
			allowed[route.method] = struct{}{}
			if route.method != r.Method {
				continue
			}
			// Extract parameter values from the URL
			params := make(map[string]string)
			for i, name := range re.SubexpNames() {
				if i != 0 && name != "" {
					params[name] = match[i]
				}
			}
			// Call the handler with the extracted parameter values
			route.handler(w, r.WithContext(newContextWithParams(r.Context(), params)))
			return
		}
	}
	if len(allowed) > 0 {
		methods := make([]string, 0, len(allowed))
		for method := range allowed {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		w.Header().Set("Allow", strings.Join(methods, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	// No matching route found
	http.NotFound(w, r)
}
