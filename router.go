package main

import (
	"context"
	"log"
	"net/http"
	"regexp"
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
	for _, route := range router.routes {
		re := route.pattern
		match := re.FindStringSubmatch(r.URL.Path)
		if len(match) > 0 {
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
	// No matching route found
	http.NotFound(w, r)
}
