package shared_scanner_workspace_race_test

import (
	"fmt"
	"testing"
	"time"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/policy"
)

func TestSharedScannerWorkspaceIsRequestScoped(t *testing.T) {
	caseOne := scanCase(t, "case-one", 1)
	caseTwo := scanCase(t, "case-two", 2)
	planOne, err := policy.NewFullScanPlan(caseOne)
	if err != nil {
		t.Fatal(err)
	}
	planTwo, err := policy.NewFullScanPlan(caseTwo)
	if err != nil {
		t.Fatal(err)
	}

	scanner := policy.NewScanner()
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, input := range []struct {
		c    *domain.ReleaseCase
		plan policy.ScanPlan
	}{{caseOne, planOne}, {caseTwo, planTwo}} {
		input := input
		go func() {
			<-start
			_, _, executeErr := scanner.Execute(input.c, input.plan)
			done <- executeErr
		}()
	}
	close(start)
	for range 2 {
		if executeErr := <-done; executeErr != nil {
			t.Fatalf("并发扫描失败: %v", executeErr)
		}
	}

	_, firstScope, err := scanner.Execute(caseOne, planOne)
	if err != nil {
		t.Fatal(err)
	}
	_, secondScope, err := scanner.Execute(caseTwo, planTwo)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstScope) != len(planOne.SegmentIDs) || len(secondScope) != len(planTwo.SegmentIDs) {
		t.Fatalf("扫描范围被复用污染: first=%v second=%v", firstScope, secondScope)
	}
	for _, id := range planOne.SegmentIDs {
		if !firstScope[id] {
			t.Fatalf("首次扫描范围在第二次请求后丢失 %s: %v", id, firstScope)
		}
	}
}

func scanCase(t *testing.T, id string, segmentCount int) *domain.ReleaseCase {
	t.Helper()
	now := time.Date(2032, time.January, 2, 3, 4, 5, 0, time.UTC)
	c, err := domain.NewReleaseCase(id, "ARCHIVE-"+id, "扫描工作区测试", "整理员", "2033-01-01", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FreezeConsents([]domain.ParticipantConsent{{ParticipantName: "受访者", IdentityDisclosure: false, LocationPrecision: "none", EvidenceDigest: "evidence"}}, now); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < segmentCount; index++ {
		start := int64(index * 1000)
		if err := c.AddSegment(domain.RecordingSegment{StartMillis: start, EndMillis: start + 500, Summary: fmt.Sprintf("片段 %d", index+1), MentionedParticipants: []string{"受访者"}}, now); err != nil {
			t.Fatal(err)
		}
	}
	return c
}
