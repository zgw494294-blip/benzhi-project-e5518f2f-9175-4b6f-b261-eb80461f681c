package return_review_partial_mutation_test

import (
	"testing"
	"time"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/policy"
)

func TestRejectedReviewReturnLeavesAggregateUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	c, err := domain.NewReleaseCase("case-return", "OH-RETURN", "复核退回测试", "整理员", "2030-01-01", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FreezeConsents([]domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: false, LocationPrecision: "exact", EvidenceDigest: "证据"}}, now); err != nil {
		t.Fatal(err)
	}
	if err := c.AddSegment(domain.RecordingSegment{StartMillis: 0, EndMillis: 100, Summary: "提及甲", MentionedParticipants: []string{"甲"}}, now); err != nil {
		t.Fatal(err)
	}
	scanner := policy.NewScanner()
	fullPlan, err := policy.NewFullScanPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	seeds, ids, err := scanner.Execute(c, fullPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyScan(seeds, true, ids, now); err != nil {
		t.Fatal(err)
	}
	findingID := c.Findings[0].ID
	segmentID, err := c.Remediate(findingID, domain.RemediationMute, "整改前", "已静音", now)
	if err != nil {
		t.Fatal(err)
	}
	targeted, err := policy.NewTargetedScanPlan(c, map[string]bool{segmentID: true})
	if err != nil {
		t.Fatal(err)
	}
	seeds, ids, err = scanner.Execute(c, targeted)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyScan(seeds, false, ids, now); err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitReview("复核员", now); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateInvariants(); err != nil {
		t.Fatalf("setup aggregate invalid: %v", err)
	}
	if err := c.ReturnReview([]string{findingID, "missing-finding"}, "需要补充说明", now); err == nil {
		t.Fatal("return with unknown finding unexpectedly succeeded")
	}
	if err := c.ValidateInvariants(); err != nil {
		t.Fatalf("rejected review return partially reopened findings: %v", err)
	}
	if c.Findings[0].Status != domain.FindingResolved || c.Findings[0].ResolvedAt == nil {
		t.Fatalf("rejected review return changed finding: %+v", c.Findings[0])
	}
}
