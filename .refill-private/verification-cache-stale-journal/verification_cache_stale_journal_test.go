package verification_cache_stale_journal_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func TestVerificationCacheRevalidatesTamperedJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	repo, err := journal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	svc := application.NewService(repo, policy.NewScanner())

	detail, err := svc.Create(application.CreateCaseCommand{
		ArchiveCode:       "OH-CACHE-INTEGRITY",
		Title:             "日志缓存失效复现",
		EditorName:        "整理员",
		TargetPublishDate: "2030-01-01",
	}, "cache-integrity-create")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.FreezeConsents(detail.Case.ID, detail.Case.Version, application.FreezeConsentsCommand{Consents: []domain.ParticipantConsent{{
		ParticipantName:    "受访者",
		IdentityDisclosure: true,
		LocationPrecision:  "exact",
		EvidenceDigest:     "授权证据",
	}}}, "cache-integrity-consents")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.AddSegment(detail.Case.ID, detail.Case.Version, application.AddSegmentCommand{
		StartMillis:           0,
		EndMillis:             1000,
		Summary:               "可公开片段",
		MentionedParticipants: []string{"受访者"},
	}, "cache-integrity-segment")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Scan(detail.Case.ID, detail.Case.Version, "cache-integrity-scan")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.SubmitReview(detail.Case.ID, detail.Case.Version, application.SubmitReviewCommand{ReviewerName: "复核员"}, "cache-integrity-review")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Approve(detail.Case.ID, detail.Case.Version, application.ApproveCommand{}, "cache-integrity-approve")
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.VerifyDetailed(detail.Case.ID)
	if err != nil || !first.Valid {
		t.Fatalf("initial verification failed: valid=%v err=%v", first.Valid, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	modified := bytes.Replace(raw, []byte(`"kind":"aggregate.committed"`), []byte(`"kind":"aggregate.tampered!"`), 1)
	if bytes.Equal(raw, modified) {
		t.Fatal("journal record marker not found")
	}
	if err := os.WriteFile(path, modified, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = svc.VerifyDetailed(detail.Case.ID)
	if !errors.Is(err, journal.ErrCorruptJournal) {
		t.Fatalf("expected journal corruption after cached verification, got %v", err)
	}
}
