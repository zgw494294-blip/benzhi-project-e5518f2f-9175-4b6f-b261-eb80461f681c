package idempotency_response_cache_alias_test

import (
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func TestIdempotencyReplayOwnsCachedResponse(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("打开仓储: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	service := application.NewService(repo, policy.NewScanner())
	command := application.CreateCaseCommand{
		ArchiveCode:       "OH-CACHE-001",
		Title:             "馆藏原始标题",
		EditorName:        "整理员",
		TargetPublishDate: "2030-06-01",
	}
	first, err := service.Create(command, "create-cache-alias")
	if err != nil {
		t.Fatalf("首次创建: %v", err)
	}
	first.Case.Title = "调用方临时改写"

	replayed, err := service.Create(command, "create-cache-alias")
	if err != nil {
		t.Fatalf("重放创建: %v", err)
	}
	if replayed.Case.Title != command.Title {
		t.Fatalf("幂等重放返回了被调用方污染的缓存标题: got %q want %q", replayed.Case.Title, command.Title)
	}

	persisted, err := application.NewService(repo, policy.NewScanner()).Create(command, "create-cache-alias")
	if err != nil {
		t.Fatalf("从持久化幂等记录重放: %v", err)
	}
	if persisted.Case.Title != command.Title {
		t.Fatalf("日志中的规范响应也被污染: got %q want %q", persisted.Case.Title, command.Title)
	}
}
