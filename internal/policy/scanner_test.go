package policy

import (
	"reflect"
	"testing"
	"time"

	"oral-history-clearance/internal/domain"
)

func TestScannerIsDeterministicAndCoversBoundaries(t *testing.T) {
	now := time.Now()
	c, _ := domain.NewReleaseCase("case-policy", "P-1", "规则测试", "整理员", "2028-01-01", now)
	if err := c.FreezeConsents([]domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: false, RestrictedTopics: []string{"迁居"}, LocationPrecision: "province", EmbargoUntil: "2029-01-01", EvidenceDigest: "证据"}}, now); err != nil {
		t.Fatal(err)
	}
	if err := c.AddSegment(domain.RecordingSegment{StartMillis: 0, EndMillis: 100, Summary: "敏感段", MentionedParticipants: []string{"甲"}, TopicTags: []string{"迁居"}, LocationTag: "exact:门牌"}, now); err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner()
	one := scanner.ScanAll(c)
	two := scanner.ScanAll(c)
	if !reflect.DeepEqual(one, two) {
		t.Fatal("scanner output is not deterministic")
	}
	if len(one) != 4 {
		t.Fatalf("expected four boundary findings, got %d: %#v", len(one), one)
	}
}

func TestScanPlanRejectsStaleRevision(t *testing.T) {
	now := time.Now()
	c, _ := domain.NewReleaseCase("case-plan", "P-2", "计划测试", "整理员", "2030-01-01", now)
	_ = c.FreezeConsents([]domain.ParticipantConsent{{ParticipantName: "甲", IdentityDisclosure: true, LocationPrecision: "exact", EvidenceDigest: "证据"}}, now)
	_ = c.AddSegment(domain.RecordingSegment{StartMillis: 0, EndMillis: 100, Summary: "普通段"}, now)
	plan, err := NewFullScanPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	plan.ContentRevision++
	if _, _, err := NewScanner().Execute(c, plan); err != domain.ErrScanStale {
		t.Fatalf("expected stale plan error, got %v", err)
	}
}
