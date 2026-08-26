package main

import (
	"net/http"
	"time"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
	"oral-history-clearance/internal/web"
)

func buildHandler(repo *journal.Repository) http.Handler {
	service := application.NewService(repo, policy.NewScanner())
	return web.NewHandler(service).Routes()
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       12 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}
