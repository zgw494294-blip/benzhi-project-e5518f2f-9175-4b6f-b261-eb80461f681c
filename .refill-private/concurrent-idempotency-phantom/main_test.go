package concurrent_idempotency_phantom_test

import (
	"crypto/rand"
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

type controlledReader struct {
	entered chan chan byte
}

func (r *controlledReader) Read(p []byte) (int, error) {
	release := make(chan byte)
	r.entered <- release
	fill := <-release
	for i := range p {
		p[i] = fill
	}
	return len(p), nil
}

type createResult struct {
	detail application.Detail
	err    error
}

func TestConcurrentCreateIdempotencyReturnsCanonicalResponse(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	reader := &controlledReader{entered: make(chan chan byte, 2)}
	originalReader := rand.Reader
	rand.Reader = reader
	defer func() { rand.Reader = originalReader }()

	serviceOne := application.NewService(repo, policy.NewScanner())
	serviceTwo := application.NewService(repo, policy.NewScanner())
	command := application.CreateCaseCommand{
		ArchiveCode:       "OH-CONCURRENT-IDEMPOTENCY",
		Title:             "并发幂等测试",
		EditorName:        "整理员",
		TargetPublishDate: "2030-01-01",
	}
	results := make(chan createResult, 2)
	go func() {
		detail, createErr := serviceOne.Create(command, "same-key")
		results <- createResult{detail: detail, err: createErr}
	}()
	firstRelease := <-reader.entered
	go func() {
		detail, createErr := serviceTwo.Create(command, "same-key")
		results <- createResult{detail: detail, err: createErr}
	}()
	secondRelease := <-reader.entered

	firstRelease <- 0x11
	first := <-results
	if first.err != nil {
		t.Fatalf("first create failed: %v", first.err)
	}
	secondRelease <- 0x22
	second := <-results
	if second.err != nil {
		t.Fatalf("idempotent replay failed: %v", second.err)
	}
	if first.detail.Case.ID != second.detail.Case.ID {
		t.Fatalf("concurrent replay returned phantom case: first=%s second=%s", first.detail.Case.ID, second.detail.Case.ID)
	}
	if len(repo.List()) != 1 {
		t.Fatalf("idempotent create persisted %d cases", len(repo.List()))
	}
}
