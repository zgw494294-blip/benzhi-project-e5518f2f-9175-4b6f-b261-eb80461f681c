package application

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
	"oral-history-clearance/internal/policy"
)

type Service struct {
	mu      sync.Mutex
	repo    *journal.Repository
	scanner *policy.Scanner
	now     func() time.Time
}

type Detail struct {
	Case             *domain.ReleaseCase           `json:"case"`
	Todos            []Todo                        `json:"todos"`
	ReviewProgress   *ReviewProgressView           `json:"reviewProgress,omitempty"`
	BatchRemediation []domain.BatchRemediationItem `json:"batchRemediation,omitempty"`
}

type ReviewProgressView struct {
	Round                int64             `json:"round"`
	VerifiedFindingIDs   []string          `json:"verifiedFindingIds"`
	UnverifiedFindingIDs []string          `json:"unverifiedFindingIds"`
	Notes                map[string]string `json:"notes,omitempty"`
	LastSavedAt          time.Time         `json:"lastSavedAt,omitempty"`
}

type Todo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewService(repo *journal.Repository, scanner *policy.Scanner) *Service {
	s := &Service{repo: repo, scanner: scanner, now: time.Now}
	// 注册在原子提交边界内最终确定凭据的回调。Approve 在读锁外读取的下一序号
	// 可能被并发提交占用，因此凭据的 EventSequence、CanonicalDigest 及派生标识符
	// 必须在 Commit 写锁内锚定到实际分配的序号上，确保凭据事件序号始终等于签发
	// 该凭据的日志记录序号，校验也不会误认其他记录为锚点。
	repo.SetCredentialFinalizer(func(c *domain.ReleaseCase, sequence int64) error {
		if c.Credential == nil {
			return nil
		}
		digest := policy.Digest(c, c.Credential.ReviewerName, sequence)
		c.Credential.EventSequence = sequence
		c.Credential.CanonicalDigest = digest
		c.Credential.ID = "credential-" + digest[:16]
		return nil
	})
	repo.SetResponseMarshaler(func(c *domain.ReleaseCase) (json.RawMessage, error) {
		projected := project(c)
		return json.Marshal(projected)
	})
	return s
}

func (s *Service) Create(command CreateCaseCommand, key string) (Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fingerprint, err := commandFingerprint("create", command)
	if err != nil {
		return Detail{}, err
	}
	if prior, ok, err := s.lookup(key, fingerprint); ok || err != nil {
		return prior, err
	}
	normalizedArchive := domain.NormalizeArchiveCode(command.ArchiveCode)
	if existingID, occupied := s.repo.FindByArchiveCode(normalizedArchive); occupied {
		return Detail{}, &domain.ArchiveCodeConflictError{ArchiveCode: normalizedArchive, ExistingCaseID: existingID}
	}
	id, err := newID("case")
	if err != nil {
		return Detail{}, err
	}
	c, err := domain.NewReleaseCase(id, command.ArchiveCode, command.Title, command.EditorName, command.TargetPublishDate, s.now())
	if err != nil {
		return Detail{}, err
	}
	detail := project(c)
	response, err := json.Marshal(detail)
	if err != nil {
		return Detail{}, err
	}
	if _, err := s.repo.Commit(c, 0, key, fingerprint, response); err != nil {
		return Detail{}, err
	}
	return detail, nil
}

func (s *Service) FreezeConsents(caseID string, expected int64, command FreezeConsentsCommand, key string) (Detail, error) {
	return s.change("freeze-consents", caseID, expected, command, key, func(c *domain.ReleaseCase) error { return c.FreezeConsents(command.Consents, s.now()) })
}

func (s *Service) AddSegment(caseID string, expected int64, command AddSegmentCommand, key string) (Detail, error) {
	return s.change("add-segment", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		return c.AddSegment(domain.RecordingSegment{StartMillis: command.StartMillis, EndMillis: command.EndMillis, Summary: command.Summary, MentionedParticipants: command.MentionedParticipants, TopicTags: command.TopicTags, LocationTag: command.LocationTag}, s.now())
	})
}

func (s *Service) Scan(caseID string, expected int64, key string) (Detail, error) {
	return s.change("scan", caseID, expected, struct{}{}, key, func(c *domain.ReleaseCase) error {
		plan, err := policy.NewFullScanPlan(c)
		if err != nil {
			return err
		}
		findings, ids, err := s.scanner.Execute(c, plan)
		if err != nil {
			return err
		}
		return c.ApplyScan(findings, true, ids, s.now())
	})
}

func (s *Service) Remediate(caseID string, expected int64, command RemediateCommand, key string) (Detail, error) {
	return s.change("remediate", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		segmentID, err := c.Remediate(command.FindingID, command.RemediationType, command.BeforeNote, command.AfterNote, s.now())
		if err != nil {
			return err
		}
		ids := map[string]bool{segmentID: true}
		plan, err := policy.NewTargetedScanPlan(c, ids)
		if err != nil {
			return err
		}
		findings, plannedIDs, err := s.scanner.Execute(c, plan)
		if err != nil {
			return err
		}
		return c.ApplyScan(findings, false, plannedIDs, s.now())
	})
}

func (s *Service) SubmitReview(caseID string, expected int64, command SubmitReviewCommand, key string) (Detail, error) {
	return s.change("submit-review", caseID, expected, command, key, func(c *domain.ReleaseCase) error { return c.SubmitReview(command.ReviewerName, s.now()) })
}

func (s *Service) ReturnReview(caseID string, expected int64, command ReturnReviewCommand, key string) (Detail, error) {
	return s.change("return-review", caseID, expected, command, key, func(c *domain.ReleaseCase) error { return c.ReturnReview(command.FindingIDs, command.Note, s.now()) })
}

func (s *Service) Approve(caseID string, expected int64, command ApproveCommand, key string) (Detail, error) {
	return s.change("approve", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		if err := policy.PlanRevisionCurrent(c); err != nil {
			return err
		}
		if c.Review == nil {
			return domain.ErrInvalidState
		}
		sequence := s.repo.NextSequence()
		segments := c.IncludedSegments()
		ids := make([]string, len(segments))
		for i := range segments {
			ids[i] = segments[i].ID
		}
		digest := policy.Digest(c, c.Review.ReviewerName, sequence)
		credential := domain.ReleaseCredential{ID: "credential-" + digest[:16], CaseID: c.ID, ConsentRevision: c.ConsentRevision, IncludedSegmentIDs: ids, ReviewerName: c.Review.ReviewerName, ApprovedAt: s.now().UTC(), EventSequence: sequence, CanonicalDigest: digest}
		return c.Approve(credential, command.VerifiedFindingIDs, s.now())
	})
}

func (s *Service) change(operation, caseID string, expected int64, command any, key string, mutate func(*domain.ReleaseCase) error) (Detail, error) {
	return s.changeProjected(operation, caseID, expected, command, key, func(c *domain.ReleaseCase, _ *Detail) error { return mutate(c) })
}

func (s *Service) changeProjected(operation, caseID string, expected int64, command any, key string, mutate func(*domain.ReleaseCase, *Detail) error) (Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected <= 0 {
		return Detail{}, fmt.Errorf("%w: expectedVersion 必须为正数", domain.ErrValidation)
	}
	fingerprint, err := commandFingerprint(operation+":"+caseID, struct {
		Expected int64 `json:"expectedVersion"`
		Command  any   `json:"command"`
	}{expected, command})
	if err != nil {
		return Detail{}, err
	}
	if prior, ok, err := s.lookup(key, fingerprint); ok || err != nil {
		return prior, err
	}
	c, err := s.repo.Get(caseID)
	if err != nil {
		return Detail{}, err
	}
	if c.Version != expected {
		return Detail{}, domain.ErrVersionConflict
	}
	detail := Detail{}
	if err := mutate(c, &detail); err != nil {
		return Detail{}, err
	}
	projected := project(c)
	detail.Case, detail.Todos, detail.ReviewProgress = projected.Case, projected.Todos, projected.ReviewProgress
	response, err := json.Marshal(detail)
	if err != nil {
		return Detail{}, err
	}
	if _, err := s.repo.Commit(c, expected, key, fingerprint, response); err != nil {
		return Detail{}, err
	}
	// 提交边界内可能修正了凭据的事件序号和摘要，返回给调用方的 detail 必须从
	// 最终聚合重新投影，使其与写入日志记录及幂等缓存的聚合快照保持一致。
	finalized := project(c)
	detail.Case, detail.Todos, detail.ReviewProgress = finalized.Case, finalized.Todos, finalized.ReviewProgress
	return detail, nil
}

func (s *Service) lookup(key, fingerprint string) (Detail, bool, error) {
	if strings.TrimSpace(key) == "" {
		return Detail{}, false, fmt.Errorf("%w: Idempotency-Key 不能为空", domain.ErrValidation)
	}
	raw, ok, err := s.repo.LookupIdempotency(key, fingerprint)
	if err != nil {
		return Detail{}, ok, err
	}
	if !ok {
		return Detail{}, false, nil
	}
	var detail Detail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return Detail{}, true, fmt.Errorf("恢复幂等响应: %w", err)
	}
	return detail, true, nil
}

func (s *Service) Get(caseID string) (Detail, error) {
	c, err := s.repo.Get(caseID)
	if err != nil {
		return Detail{}, err
	}
	return project(c), nil
}

func (s *Service) List() []Detail {
	cases := s.repo.List()
	sort.Slice(cases, func(i, j int) bool { return cases[i].UpdatedAt.After(cases[j].UpdatedAt) })
	out := make([]Detail, len(cases))
	for i := range cases {
		out[i] = project(cases[i])
	}
	return out
}

func (s *Service) Verify(caseID string) (bool, string, error) {
	c, err := s.repo.Get(caseID)
	if err != nil {
		return false, "", err
	}
	integrity, err := s.repo.VerifyIntegrity()
	if err != nil {
		return false, "事件日志完整性校验失败", err
	}
	if c.Credential != nil && c.Credential.EventSequence > integrity.RecordCount {
		return false, "凭据事件序号超出日志链尾", nil
	}
	ok, message := policy.VerifyCredential(c)
	return ok, message, nil
}

func project(c *domain.ReleaseCase) Detail {
	d := Detail{Case: c.Clone()}
	if c.Review != nil && c.Review.Decision == "pending" {
		verified := map[string]bool{}
		for _, id := range c.Review.VerifiedFindingIDs {
			verified[id] = true
		}
		var missing []string
		for _, finding := range c.Findings {
			if finding.Status == domain.FindingResolved && !verified[finding.ID] {
				missing = append(missing, finding.ID)
			}
		}
		sort.Strings(missing)
		d.ReviewProgress = &ReviewProgressView{Round: c.Review.Round, VerifiedFindingIDs: append([]string(nil), c.Review.VerifiedFindingIDs...), UnverifiedFindingIDs: missing, Notes: c.Review.VerificationNotes, LastSavedAt: c.Review.LastProgressSavedAt}
	}
	switch c.Status {
	case domain.StatusDraft:
		if len(c.Consents) == 0 {
			d.Todos = append(d.Todos, Todo{"freeze_consents", "登记并冻结本轮受访者授权边界"})
		}
		if len(c.Segments) == 0 {
			d.Todos = append(d.Todos, Todo{"add_segments", "添加不重叠的录音片段目录"})
		}
		if len(c.Consents) > 0 && len(c.Segments) > 0 {
			d.Todos = append(d.Todos, Todo{"scan", "运行确定性授权冲突扫描"})
		}
	case domain.StatusRemediation:
		d.Todos = append(d.Todos, Todo{"remediate", fmt.Sprintf("处理 %d 项未关闭冲突", c.OpenFindingCount())})
	case domain.StatusReady:
		d.Todos = append(d.Todos, Todo{"submit_review", "提交伦理复核"})
	case domain.StatusReview:
		d.Todos = append(d.Todos, Todo{"review_decision", "复核员退回指定冲突或批准发布"})
	case domain.StatusReleased:
		d.Todos = append(d.Todos, Todo{"verify", "本地复算并校验发布放行凭据"})
	}
	return d
}

func commandFingerprint(operation string, command any) (string, error) {
	b, err := json.Marshal(struct {
		Operation string `json:"operation"`
		Command   any    `json:"command"`
	}{operation, command})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func newID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成标识: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}
