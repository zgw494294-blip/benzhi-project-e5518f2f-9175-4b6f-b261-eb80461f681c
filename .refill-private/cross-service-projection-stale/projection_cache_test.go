package crossserviceprojectionstale_test

import (
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func TestCrossServiceGetRefreshesCommittedProjection(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	writer := application.NewService(repo, policy.NewScanner())
	reader := application.NewService(repo, policy.NewScanner())
	created, err := writer.Create(application.CreateCaseCommand{
		ArchiveCode:       "OH-CACHE-1",
		Title:             "修订前标题",
		EditorName:        "整理员甲",
		TargetPublishDate: "2030-01-01",
	}, "create-cache-case")
	if err != nil {
		t.Fatal(err)
	}

	primed, err := reader.Get(created.Case.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := writer.ReviseProfile(created.Case.ID, created.Case.Version, application.ReviseProfileCommand{
		Title:             "修订后标题",
		EditorName:        created.Case.EditorName,
		TargetPublishDate: created.Case.TargetPublishDate,
	}, "revise-cache-case")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(created.Case.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != updated.Case.Version || stored.Title != updated.Case.Title {
		t.Fatalf("test setup did not commit revision: stored=%d/%q updated=%d/%q", stored.Version, stored.Title, updated.Case.Version, updated.Case.Title)
	}

	observed, err := reader.Get(created.Case.ID)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Case.Version != updated.Case.Version || observed.Case.Title != updated.Case.Title {
		t.Fatalf("reader returned stale projection: primed=%d/%q observed=%d/%q committed=%d/%q", primed.Case.Version, primed.Case.Title, observed.Case.Version, observed.Case.Title, updated.Case.Version, updated.Case.Title)
	}
}
