package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func (sc *Smithy) RequireWriteAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sc.WriteToken == "" {
			http.Error(w, "write operations are disabled", http.StatusServiceUnavailable)
			return
		}

		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, password, ok := r.BasicAuth(); ok {
			provided = password
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(sc.WriteToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="smithy"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
