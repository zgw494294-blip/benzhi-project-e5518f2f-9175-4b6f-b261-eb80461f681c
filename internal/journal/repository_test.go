package journal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"oral-history-clearance/internal/domain"
)

func TestRepositoryRestoresHashChainAndIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	repo, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := domain.NewReleaseCase("case-1", "A-1", "测试", "整理员", "2030-01-01", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]string{"caseId": c.ID})
	if _, err := repo.Commit(c, 0, "key-1", "fp-1", response); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.Get(c.ID)
	if err != nil || got.Version != c.Version {
		t.Fatalf("restore: %#v %v", got, err)
	}
	if _, ok, err := restored.LookupIdempotency("key-1", "fp-1"); err != nil || !ok {
		t.Fatalf("idempotency: %v %v", ok, err)
	}
}

func TestRepositoryRejectsIncompleteTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"schemaVersion\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected corrupt tail error")
	}
}
