package credential_sequence_toctou_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

type marshalGate struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (g *marshalGate) MarshalJSON() ([]byte, error) {
	if g.armed.CompareAndSwap(true, false) {
		close(g.entered)
		<-g.release
	}
	return []byte(`"gate"`), nil
}

func commitVersion(t *testing.T, repo *journal.Repository, c *domain.ReleaseCase, expected int64, key string) {
	t.Helper()
	response := json.RawMessage(`{"ok":true}`)
	if _, err := repo.Commit(c, expected, key, "fingerprint-"+key, response); err != nil {
		t.Fatalf("commit %s: %v", key, err)
	}
}

type approveResult struct {
	detail application.Detail
	err    error
}

func TestApprovalAnchorsCredentialToOwnJournalRecord(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	gate := &marshalGate{entered: make(chan struct{}), release: make(chan struct{})}

	c, err := domain.NewReleaseCase("case-approval", "OH-APPROVAL", "凭据并发测试", "整理员", "2030-01-01", now)
	if err != nil {
		t.Fatal(err)
	}
	c.Events[0].Data["marshalGate"] = gate
	commitVersion(t, repo, c, 0, "create")
	if err := c.FreezeConsents([]domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: true, LocationPrecision: "exact", EvidenceDigest: "证据"}}, now); err != nil {
		t.Fatal(err)
	}
	commitVersion(t, repo, c, 1, "consents")
	if err := c.AddSegment(domain.RecordingSegment{StartMillis: 0, EndMillis: 100, Summary: "可公开片段", MentionedParticipants: []string{"甲"}}, now); err != nil {
		t.Fatal(err)
	}
	commitVersion(t, repo, c, 2, "segment")
	plan, err := policy.NewFullScanPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	seeds, ids, err := policy.NewScanner().Execute(c, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyScan(seeds, true, ids, now); err != nil {
		t.Fatal(err)
	}
	commitVersion(t, repo, c, 3, "scan")
	if err := c.SubmitReview("复核员", now); err != nil {
		t.Fatal(err)
	}
	commitVersion(t, repo, c, 4, "review")

	service := application.NewService(repo, policy.NewScanner())
	gate.armed.Store(true)
	approved := make(chan approveResult, 1)
	go func() {
		detail, approveErr := service.Approve(c.ID, c.Version, application.ApproveCommand{}, "approve")
		approved <- approveResult{detail: detail, err: approveErr}
	}()
	<-gate.entered

	interloper, err := domain.NewReleaseCase("case-interloper", "OH-INTERLOPER", "插入提交", "整理员", "2030-01-01", now)
	if err != nil {
		t.Fatal(err)
	}
	commitVersion(t, repo, interloper, 0, "interloper")
	close(gate.release)
	result := <-approved
	if result.err != nil {
		t.Fatalf("approve failed: %v", result.err)
	}
	if result.detail.Case.Credential == nil {
		t.Fatal("approval did not issue credential")
	}
	actualApprovalSequence := repo.Sequence()
	if result.detail.Case.Credential.EventSequence != actualApprovalSequence {
		t.Fatalf("credential anchored to sequence %d, but its own journal record is %d", result.detail.Case.Credential.EventSequence, actualApprovalSequence)
	}
	if report, verifyErr := service.VerifyDetailed(c.ID); verifyErr != nil || !report.Valid {
		t.Fatalf("credential should verify after concurrent commit: valid=%v err=%v report=%s", report.Valid, verifyErr, fmt.Sprint(report.Checks))
	}
}
