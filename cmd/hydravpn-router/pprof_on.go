//go:build pprof

package main

import (
	"net/http"
	"net/http/pprof"
	"time"
)

// startPprof serves pprof on localhost only; built into debug pre-releases
// (scripts/build.sh adds the "pprof" tag for versions containing "-debug").
func startPprof() {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Addr: "127.0.0.1:6060", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		logLine("info", "pprof listening on http://127.0.0.1:6060/debug/pprof/")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logLine("warn", "pprof disabled: "+err.Error())
		}
	}()
}
