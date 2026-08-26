package application

import (
	"path/filepath"
	"testing"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func TestCompleteReleaseWorkflow(t *testing.T) {
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	svc := NewService(repo, policy.NewScanner())
	detail, err := svc.Create(CreateCaseCommand{ArchiveCode: "OH-001", Title: "社区记忆", EditorName: "整理员", TargetPublishDate: "2030-01-01"}, "k-create")
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	detail, err = svc.FreezeConsents(id, detail.Case.Version, FreezeConsentsCommand{Consents: []domain.ParticipantConsent{{ParticipantName: "李明", IdentityDisclosure: false, RestrictedTopics: []string{"迁居"}, LocationPrecision: "city", EvidenceDigest: "纸质授权书第3页"}}}, "k-consent")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.AddSegment(id, detail.Case.Version, AddSegmentCommand{StartMillis: 0, EndMillis: 10000, Summary: "李明谈迁居经历", MentionedParticipants: []string{"李明"}, TopicTags: []string{"迁居"}, LocationTag: "exact:旧居门牌"}, "k-segment")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Scan(id, detail.Case.Version, "k-scan")
	if err != nil {
		t.Fatal(err)
	}
	for detail.Case.OpenFindingCount() > 0 {
		var open domain.ConflictFinding
		for _, finding := range detail.Case.Findings {
			if finding.Status == domain.FindingOpen {
				open = finding
				break
			}
		}
		detail, err = svc.Remediate(id, detail.Case.Version, RemediateCommand{FindingID: open.ID, RemediationType: domain.RemediationSupplemental, BeforeNote: open.Reason, AfterNote: "补充授权已归档"}, "k-remediate-"+open.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	detail, err = svc.SubmitReview(id, detail.Case.Version, SubmitReviewCommand{ReviewerName: "伦理复核员"}, "k-review")
	if err != nil {
		t.Fatal(err)
	}
	verified := make([]string, 0)
	for _, finding := range detail.Case.Findings {
		if finding.Status == domain.FindingResolved {
			verified = append(verified, finding.ID)
		}
	}
	detail, err = svc.Approve(id, detail.Case.Version, ApproveCommand{VerifiedFindingIDs: verified}, "k-approve")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Case.Status != domain.StatusReleased || detail.Case.Credential == nil {
		t.Fatalf("unexpected final state: %#v", detail.Case)
	}
	ok, message, err := svc.Verify(id)
	if err != nil || !ok {
		t.Fatalf("verify: %v %s %v", ok, message, err)
	}
}

func TestExpectedVersionAndIdempotency(t *testing.T) {
	repo, _ := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	defer repo.Close()
	svc := NewService(repo, policy.NewScanner())
	command := CreateCaseCommand{ArchiveCode: "A", Title: "标题", EditorName: "甲", TargetPublishDate: "2030-01-01"}
	one, err := svc.Create(command, "same-key")
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.Create(command, "same-key")
	if err != nil || one.Case.ID != two.Case.ID {
		t.Fatalf("idempotency failed: %v", err)
	}
	_, err = svc.FreezeConsents(one.Case.ID, one.Case.Version+1, FreezeConsentsCommand{}, "wrong-version")
	if err != domain.ErrVersionConflict {
		t.Fatalf("expected version conflict, got %v", err)
	}
}
