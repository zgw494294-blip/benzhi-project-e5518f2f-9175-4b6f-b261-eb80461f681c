package canceled_scan_commit_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
	"oral-history-clearance/internal/web"
)

type cancelOnFinalRead struct {
	reader *bytes.Reader
	cancel context.CancelFunc
}

func (r *cancelOnFinalRead) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && r.reader.Len() == 0 && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return n, err
}

func TestCanceledScanDoesNotCommit(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	service := application.NewService(repo, policy.NewScanner())
	detail, err := service.Create(application.CreateCaseCommand{
		ArchiveCode:       "CANCEL-1",
		Title:             "取消扫描事务",
		EditorName:        "整理员",
		TargetPublishDate: "2030-01-01",
	}, "cancel-create")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = service.FreezeConsents(detail.Case.ID, detail.Case.Version, application.FreezeConsentsCommand{Consents: []domain.ParticipantConsent{{
		ParticipantName:    "周宁",
		IdentityDisclosure: true,
		LocationPrecision:  "exact",
		EvidenceDigest:     "授权证据-取消测试",
	}}}, "cancel-consents")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = service.AddSegment(detail.Case.ID, detail.Case.Version, application.AddSegmentCommand{
		StartMillis:           0,
		EndMillis:             1000,
		Summary:               "可公开片段",
		MentionedParticipants: []string{"周宁"},
	}, "cancel-segment")
	if err != nil {
		t.Fatal(err)
	}

	versionBefore := detail.Case.Version
	sequenceBefore := repo.Sequence()
	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelOnFinalRead{
		reader: bytes.NewReader([]byte(`{"expectedVersion":3}`)),
		cancel: cancel,
	}
	request := httptest.NewRequest(http.MethodPost, "/api/cases/"+detail.Case.ID+"/scan", io.Reader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "cancel-scan")
	response := httptest.NewRecorder()
	web.NewHandler(service).Routes().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("取消请求应返回失败，实际状态码为 %d", response.Code)
	}
	stored, err := service.Get(detail.Case.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Case.Version != versionBefore || repo.Sequence() != sequenceBefore {
		t.Fatalf("取消请求仍提交扫描：version %d -> %d，sequence %d -> %d", versionBefore, stored.Case.Version, sequenceBefore, repo.Sequence())
	}
}
