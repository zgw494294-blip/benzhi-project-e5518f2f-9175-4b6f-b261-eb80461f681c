package concurrent_integrity_hasher_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

func TestConcurrentIntegrityVerificationOwnsHasher(t *testing.T) {
	runtime.GOMAXPROCS(4)
	repo, err := journal.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	svc := application.NewService(repo, policy.NewScanner())
	detail, err := svc.Create(application.CreateCaseCommand{ArchiveCode: "OH-ROOT", Title: "并发完整性校验", EditorName: "整理员", TargetPublishDate: "2030-01-01"}, "workflow-create")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.FreezeConsents(detail.Case.ID, detail.Case.Version, application.FreezeConsentsCommand{Consents: []domain.ParticipantConsent{{ParticipantName: "受访者", IdentityDisclosure: true, LocationPrecision: "exact", EvidenceDigest: "授权证据"}}}, "workflow-consents")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.AddSegment(detail.Case.ID, detail.Case.Version, application.AddSegmentCommand{StartMillis: 0, EndMillis: 1000, Summary: "公开片段", MentionedParticipants: []string{"受访者"}}, "workflow-segment")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Scan(detail.Case.ID, detail.Case.Version, "workflow-scan")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.SubmitReview(detail.Case.ID, detail.Case.Version, application.SubmitReviewCommand{ReviewerName: "复核员"}, "workflow-review")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = svc.Approve(detail.Case.ID, detail.Case.Version, application.ApproveCommand{}, "workflow-approve")
	if err != nil {
		t.Fatal(err)
	}
	releasedCaseID := detail.Case.ID

	for i := 0; i < 58; i++ {
		caseID := fmt.Sprintf("case-%03d", i)
		c, createErr := domain.NewReleaseCase(caseID, fmt.Sprintf("OH-%03d", i), "并发完整性校验", "整理员", "2030-01-01", time.Unix(int64(i+1), 0))
		if createErr != nil {
			t.Fatal(createErr)
		}
		response, marshalErr := json.Marshal(map[string]string{"caseId": caseID})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, commitErr := repo.Commit(c, 0, "key-"+caseID, "fingerprint-"+caseID, response); commitErr != nil {
			t.Fatal(commitErr)
		}
	}

	const workers = 4
	start := make(chan struct{})
	results := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			ready.Done()
			<-start
			report, verifyErr := svc.VerifyDetailed(releasedCaseID)
			if verifyErr == nil && (!report.Valid || report.RecordCount != 64) {
				verifyErr = fmt.Errorf("校验报告异常: valid=%v recordCount=%d", report.Valid, report.RecordCount)
			}
			results <- verifyErr
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < workers; i++ {
		if verifyErr := <-results; verifyErr != nil {
			t.Errorf("并发只读校验不应污染哈希状态: %v", verifyErr)
		}
	}
}
