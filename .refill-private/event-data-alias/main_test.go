package event_data_alias_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
)

func TestRepositoryGetDoesNotAliasDomainEventData(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	c, err := domain.NewReleaseCase("case-alias", "OH-ALIAS", "缓存隔离测试", "整理员", "2030-01-01", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(c, 0, "create", "fingerprint", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	first, err := repo.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	first.Events[0].Data["archiveCode"] = "POISONED"
	second, err := repo.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Events[0].Data["archiveCode"]; got != "OH-ALIAS" {
		t.Fatalf("mutating a returned clone polluted repository cache: archiveCode=%v", got)
	}
}
