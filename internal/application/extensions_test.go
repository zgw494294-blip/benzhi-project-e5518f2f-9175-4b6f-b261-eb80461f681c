package application

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func TestArchiveUniquenessAndAtomicBatchImport(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	svc := NewService(repo, policy.NewScanner())
	detail, err := svc.Create(CreateCaseCommand{ArchiveCode: " OH-2026-014 ", Title: "测试", EditorName: "甲", TargetPublishDate: "2030-01-01"}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Case.ArchiveCode != "OH-2026-014" {
		t.Fatalf("archive code not normalized: %q", detail.Case.ArchiveCode)
	}
	sequence := repo.Sequence()
	_, err = svc.Create(CreateCaseCommand{ArchiveCode: "oh-2026-014", Title: "重复", EditorName: "乙", TargetPublishDate: "2030-01-01"}, "create-2")
	var conflict *domain.ArchiveCodeConflictError
	if !errors.As(err, &conflict) || conflict.ExistingCaseID != detail.Case.ID || repo.Sequence() != sequence {
		t.Fatalf("duplicate archive: %#v sequence=%d", err, repo.Sequence())
	}
	detail, err = svc.FreezeConsents(detail.Case.ID, detail.Case.Version, FreezeConsentsCommand{Consents: []domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: true, LocationPrecision: "city", EvidenceDigest: "证据"}}}, "consent")
	if err != nil {
		t.Fatal(err)
	}
	bad := []domain.SegmentInput{{Row: 1, StartMillis: 0, EndMillis: 100, Summary: "第一段", MentionedParticipants: []string{"甲"}}, {Row: 2, StartMillis: 50, EndMillis: 150, Summary: "第二段", MentionedParticipants: []string{"未知"}}}
	preview, err := svc.PreviewSegmentBatch(detail.Case.ID, bad)
	if err != nil || preview.Valid || len(preview.Problems) < 2 {
		t.Fatalf("bad preview: %#v %v", preview, err)
	}
	version, count, sequence := detail.Case.Version, len(detail.Case.Segments), repo.Sequence()
	_, err = svc.ImportSegmentBatch(detail.Case.ID, version, ImportSegmentsCommand{Segments: bad}, "bad-import")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation: %v", err)
	}
	stored, _ := svc.Get(detail.Case.ID)
	if stored.Case.Version != version || len(stored.Case.Segments) != count || repo.Sequence() != sequence {
		t.Fatal("invalid batch changed persisted state")
	}
	good := []domain.SegmentInput{{Row: 2, StartMillis: 200, EndMillis: 300, Summary: "后段", MentionedParticipants: []string{"甲"}}, {Row: 1, StartMillis: 0, EndMillis: 100, Summary: "前段", MentionedParticipants: []string{"甲"}}}
	detail, err = svc.ImportSegmentBatch(detail.Case.ID, version, ImportSegmentsCommand{Segments: good}, "good-import")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Case.Segments) != 2 || detail.Case.Segments[0].Sequence != 1 || detail.Case.Segments[0].Summary != "前段" {
		t.Fatalf("unexpected order: %#v", detail.Case.Segments)
	}
	replayed, err := svc.ImportSegmentBatch(detail.Case.ID, version, ImportSegmentsCommand{Segments: good}, "good-import")
	if err != nil || len(replayed.Case.Segments) != 2 {
		t.Fatalf("idempotent import: %v", err)
	}
}

func TestConsentRevisionStalesScanAndCredentialExportSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	repo, err := journal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, policy.NewScanner())
	detail, err := svc.Create(CreateCaseCommand{ArchiveCode: "OH-R", Title: "修订测试", EditorName: "甲", TargetPublishDate: "2030-01-01"}, "create")
	if err != nil {
		t.Fatal(err)
	}
	consents := []domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: true, LocationPrecision: "exact", EvidenceDigest: "证据"}}
	detail, err = svc.FreezeConsents(detail.Case.ID, detail.Case.Version, FreezeConsentsCommand{Consents: consents}, "consents")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.ImportSegmentBatch(detail.Case.ID, detail.Case.Version, ImportSegmentsCommand{Segments: []domain.SegmentInput{{Row: 1, StartMillis: 0, EndMillis: 100, Summary: "公开段", MentionedParticipants: []string{"甲"}, LocationTag: "city:南京"}}}, "segments")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Scan(detail.Case.ID, detail.Case.Version, "scan")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Case.Status != domain.StatusReady {
		t.Fatalf("status=%s", detail.Case.Status)
	}
	preview, err := svc.PreviewConsentRevision(detail.Case.ID, []domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: true, LocationPrecision: "province", EvidenceDigest: "新证据"}})
	if err != nil || !preview.Valid || len(preview.Differences) != 2 {
		t.Fatalf("consent preview: %#v %v", preview, err)
	}
	before := detail.Case.Version
	detail, err = svc.ReviseConsents(detail.Case.ID, before, ReviseConsentsCommand{Consents: []domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: true, LocationPrecision: "province", EvidenceDigest: "新证据"}}, Reason: "授权范围收紧", BaseConsentRevision: preview.BaseConsentRevision}, "revise")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Case.Version != before+1 || !detail.Case.NeedsFullScan || detail.Case.Status != domain.StatusDraft || detail.Case.ScanCurrent() {
		t.Fatalf("revision did not stale scan: %#v", detail.Case)
	}
	detail, err = svc.Scan(detail.Case.ID, detail.Case.Version, "rescan")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Case.Status != domain.StatusRemediation || detail.Case.OpenFindingCount() != 1 {
		t.Fatalf("expected location conflict: %#v", detail.Case.Findings)
	}
	// 使用静音完成整改，再验证凭据导出跨重启保持字节稳定。
	f := detail.Case.Findings[0]
	detail, err = svc.Remediate(detail.Case.ID, detail.Case.Version, RemediateCommand{FindingID: f.ID, RemediationType: domain.RemediationMute, BeforeNote: f.Reason, AfterNote: "静音"}, "remediate")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.SubmitReview(detail.Case.ID, detail.Case.Version, SubmitReviewCommand{ReviewerName: "复核员"}, "submit")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.SaveReviewProgress(detail.Case.ID, detail.Case.Version, SaveReviewProgressCommand{VerifiedFindingIDs: []string{f.ID}}, "progress")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Approve(detail.Case.ID, detail.Case.Version, ApproveCommand{}, "approve")
	if err != nil {
		t.Fatal(err)
	}
	one, _, err := svc.ExportCredential(detail.Case.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := journal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	two, _, err := NewService(restored, policy.NewScanner()).ExportCredential(detail.Case.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatal("credential export bytes changed after restart")
	}
}
