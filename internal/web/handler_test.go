package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	return NewHandler(application.NewService(repo, policy.NewScanner())).Routes()
}

func TestIndexAndCreateAPI(t *testing.T) {
	h := testHandler(t)
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<body>") {
		t.Fatalf("index: %d", page.Code)
	}
	body, _ := json.Marshal(application.CreateCaseCommand{ArchiveCode: "W-1", Title: "网页测试", EditorName: "整理员", TargetPublishDate: "2030-01-01"})
	request := httptest.NewRequest(http.MethodPost, "/api/cases", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "web-create")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing security headers")
	}
}

func TestWriteRequiresJSONAndIdempotencyKey(t *testing.T) {
	h := testHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/api/cases", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing key should be rejected first: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/cases", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Idempotency-Key", "test")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("media type: %d", response.Code)
	}
}
