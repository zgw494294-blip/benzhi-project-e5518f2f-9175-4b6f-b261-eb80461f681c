package domain

import (
	"errors"
	"testing"
	"time"
)

func draftWithConsent(t *testing.T) *ReleaseCase {
	t.Helper()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	c, err := NewReleaseCase("case-test", "OH-T", "测试发布案", "整理员", "2030-01-01", now)
	if err != nil {
		t.Fatal(err)
	}
	err = c.FreezeConsents([]ParticipantConsent{{ParticipantName: "甲", LocationPrecision: "city", EvidenceDigest: "证据"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSegmentsCannotOverlap(t *testing.T) {
	c := draftWithConsent(t)
	now := time.Now()
	if err := c.AddSegment(RecordingSegment{StartMillis: 0, EndMillis: 100, Summary: "第一段"}, now); err != nil {
		t.Fatal(err)
	}
	err := c.AddSegment(RecordingSegment{StartMillis: 99, EndMillis: 200, Summary: "重叠段"}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestRemediationMustMatchRule(t *testing.T) {
	c := draftWithConsent(t)
	now := time.Now()
	if err := c.AddSegment(RecordingSegment{StartMillis: 0, EndMillis: 100, Summary: "限制话题"}, now); err != nil {
		t.Fatal(err)
	}
	seed := FindingSeed{ID: "finding-1", SegmentID: c.Segments[0].ID, RuleCode: "TOPIC:迁居", Reason: "命中限制话题"}
	if err := c.ApplyScan([]FindingSeed{seed}, true, nil, now); err != nil {
		t.Fatal(err)
	}
	_, err := c.Remediate(seed.ID, RemediationPseudonym, "原说明", "改用化名", now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected incompatible remediation error, got %v", err)
	}
}
