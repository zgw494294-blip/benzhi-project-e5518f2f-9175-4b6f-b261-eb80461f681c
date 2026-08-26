package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"strings"

	"oral-history-clearance/internal/application"
)

//go:embed assets/*
var assetFiles embed.FS

type Handler struct {
	service *application.Service
	assets  fs.FS
}

func NewHandler(service *application.Service) *Handler {
	assets, _ := fs.Sub(assetFiles, "assets")
	return &Handler{service: service, assets: assets}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.problem(w, http.StatusNotFound, "not_found", "页面不存在")
		return
	}
	h.serveAsset(w, r, "index.html")
}

func (h *Handler) Asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		h.problem(w, http.StatusNotFound, "not_found", "资源不存在")
		return
	}
	h.serveAsset(w, r, name)
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	b, err := fs.ReadFile(h.assets, name)
	if err != nil {
		h.problem(w, http.StatusNotFound, "not_found", "资源不存在")
		return
	}
	contentType := mime.TypeByExtension(extension(name))
	if name == "index.html" {
		contentType = "text/html; charset=utf-8"
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func extension(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}
