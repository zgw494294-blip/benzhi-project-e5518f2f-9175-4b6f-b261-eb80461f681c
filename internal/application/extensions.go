package application

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/policy"
)

func (s *Service) ReviseProfile(caseID string, expected int64, command ReviseProfileCommand, key string) (Detail, error) {
	return s.change("revise-profile", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		return c.ReviseProfile(command.Title, command.EditorName, command.TargetPublishDate, s.now())
	})
}

func (s *Service) PreviewConsentRevision(caseID string, consents []domain.ParticipantConsent) (ConsentPreview, error) {
	c, err := s.repo.Get(caseID)
	if err != nil {
		return ConsentPreview{}, err
	}
	preview := ConsentPreview{BaseConsentRevision: c.ConsentRevision}
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, consent := range consents {
		name := strings.TrimSpace(consent.ParticipantName)
		if name == "" {
			preview.Problems = append(preview.Problems, ConsentProblem{Field: "participantName", Code: "participant_required", Message: "受访者姓名不能为空"})
		} else if seen[name] {
			preview.Problems = append(preview.Problems, ConsentProblem{ParticipantName: name, Field: "participantName", Code: "participant_duplicate", Message: "受访者重复"})
		}
		seen[name], known[name] = true, true
		if strings.TrimSpace(consent.EvidenceDigest) == "" {
			preview.Problems = append(preview.Problems, ConsentProblem{ParticipantName: name, Field: "evidenceDigest", Code: "evidence_required", Message: "授权证据摘要不能为空"})
		}
		if consent.LocationPrecision != "none" && consent.LocationPrecision != "province" && consent.LocationPrecision != "city" && consent.LocationPrecision != "exact" {
			preview.Problems = append(preview.Problems, ConsentProblem{ParticipantName: name, Field: "locationPrecision", Code: "invalid_location_precision", Message: "地点精度无效"})
		}
		if consent.EmbargoUntil != "" {
			if _, dateErr := time.Parse("2006-01-02", consent.EmbargoUntil); dateErr != nil {
				preview.Problems = append(preview.Problems, ConsentProblem{ParticipantName: name, Field: "embargoUntil", Code: "invalid_embargo_date", Message: "禁用日期格式错误"})
			}
		}
	}
	for _, segment := range c.Segments {
		if segment.Deleted {
			continue
		}
		for _, name := range segment.MentionedParticipants {
			if !known[name] {
				preview.Problems = append(preview.Problems, ConsentProblem{ParticipantName: name, Field: "mentionedParticipants", Code: "removed_participant_referenced", Message: "片段引用拟移除受访者", SegmentID: segment.ID})
			}
		}
	}
	if len(preview.Problems) > 0 {
		sort.SliceStable(preview.Problems, func(i, j int) bool {
			if preview.Problems[i].ParticipantName != preview.Problems[j].ParticipantName {
				return preview.Problems[i].ParticipantName < preview.Problems[j].ParticipantName
			}
			if preview.Problems[i].SegmentID != preview.Problems[j].SegmentID {
				return preview.Problems[i].SegmentID < preview.Problems[j].SegmentID
			}
			return preview.Problems[i].Field < preview.Problems[j].Field
		})
		return preview, nil
	}
	_, diffs, err := c.PrepareConsentRevision(consents, s.now())
	if err != nil {
		return ConsentPreview{}, err
	}
	preview.Differences, preview.Valid = diffs, len(diffs) > 0
	return preview, nil
}

func (s *Service) ReviseConsents(caseID string, expected int64, command ReviseConsentsCommand, key string) (Detail, error) {
	return s.change("revise-consents", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		return c.ReviseConsents(command.Consents, command.Reason, command.BaseConsentRevision, s.now())
	})
}

func (s *Service) PreviewSegmentBatch(caseID string, inputs []domain.SegmentInput) (domain.SegmentPreview, error) {
	c, err := s.repo.Get(caseID)
	if err != nil {
		return domain.SegmentPreview{}, err
	}
	if len(inputs) == 0 {
		return domain.SegmentPreview{}, fmt.Errorf("%w: 批次不能为空", domain.ErrValidation)
	}
	return c.PreviewSegmentBatch(inputs), nil
}

func (s *Service) ImportSegmentBatch(caseID string, expected int64, command ImportSegmentsCommand, key string) (Detail, error) {
	return s.change("import-segment-batch", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		_, err := c.ImportSegmentBatch(command.Segments, s.now())
		return err
	})
}

func (s *Service) BatchRemediate(caseID string, expected int64, command BatchRemediateCommand, key string) (Detail, error) {
	return s.changeProjected("batch-remediate", caseID, expected, command, key, func(c *domain.ReleaseCase, detail *Detail) error {
		results, affected, err := c.BatchRemediate(command.FindingIDs, command.RemediationType, command.BeforeNote, command.AfterNote, s.now())
		if err != nil {
			return err
		}
		plan, err := policy.NewTargetedScanPlan(c, affected)
		if err != nil {
			return err
		}
		findings, planned, err := s.scanner.Execute(c, plan)
		if err != nil {
			return err
		}
		if err := c.ApplyScan(findings, false, planned, s.now()); err != nil {
			return err
		}
		outcomes := map[string]string{}
		for _, item := range results {
			outcomes[item.FindingID] = item.Outcome
		}
		for _, finding := range c.Findings {
			if _, selected := outcomes[finding.ID]; selected {
				if finding.Status == domain.FindingOpen {
					outcomes[finding.ID] = "still_matching"
				}
			}
		}
		detail.BatchRemediation = make([]domain.BatchRemediationItem, 0, len(outcomes))
		for id, outcome := range outcomes {
			detail.BatchRemediation = append(detail.BatchRemediation, domain.BatchRemediationItem{FindingID: id, Outcome: outcome})
		}
		sort.Slice(detail.BatchRemediation, func(i, j int) bool {
			return detail.BatchRemediation[i].FindingID < detail.BatchRemediation[j].FindingID
		})
		return nil
	})
}

func (s *Service) RemediateWithSupplementalConsent(caseID string, expected int64, command SupplementalConsentCommand, key string) (Detail, error) {
	return s.change("supplemental-consent", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		if err := c.AddSupplementalConsent(command.FindingID, command.ParticipantName, command.EvidenceDigest, command.AuthorizedDate, command.RuleCode, command.SegmentIDs, s.now()); err != nil {
			return err
		}
		segmentID, err := c.Remediate(command.FindingID, domain.RemediationSupplemental, command.BeforeNote, command.AfterNote, s.now())
		if err != nil {
			return err
		}
		plan, err := policy.NewTargetedScanPlan(c, map[string]bool{segmentID: true})
		if err != nil {
			return err
		}
		findings, planned, err := s.scanner.Execute(c, plan)
		if err != nil {
			return err
		}
		return c.ApplyScan(findings, false, planned, s.now())
	})
}

func (s *Service) SaveReviewProgress(caseID string, expected int64, command SaveReviewProgressCommand, key string) (Detail, error) {
	return s.change("save-review-progress", caseID, expected, command, key, func(c *domain.ReleaseCase) error {
		return c.SaveReviewProgress(command.VerifiedFindingIDs, command.Notes, s.now())
	})
}

type FindingQuery struct {
	RuleCode     string
	Participant  string
	Status       domain.FindingStatus
	SequenceFrom int
	SequenceTo   int
	PageSize     int
	Cursor       string
}

type FindingCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}
type FindingStatistics struct {
	ByRule               []FindingCount `json:"byRule"`
	ByParticipant        []FindingCount `json:"byParticipant"`
	BySegment            []FindingCount `json:"bySegment"`
	ByStatus             []FindingCount `json:"byStatus"`
	AffectedSegmentCount int            `json:"affectedSegmentCount"`
	OpenCount            int            `json:"openCount"`
	ScanRevision         int64          `json:"scanRevision"`
	ConsentRevision      int64          `json:"consentRevision"`
	ContentRevision      int64          `json:"contentRevision"`
	Current              bool           `json:"current"`
}
type FindingPage struct {
	Items      []domain.ConflictFinding `json:"items"`
	Statistics FindingStatistics        `json:"statistics"`
	NextCursor string                   `json:"nextCursor,omitempty"`
	Current    bool                     `json:"current"`
}

type findingCursor struct {
	CaseVersion int64  `json:"caseVersion"`
	Offset      int    `json:"offset"`
	Signature   string `json:"signature"`
}

func (s *Service) QueryFindings(caseID string, query FindingQuery) (FindingPage, error) {
	c, err := s.repo.Get(caseID)
	if err != nil {
		return FindingPage{}, err
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		return FindingPage{}, fmt.Errorf("%w: pageSize 必须介于 1 和 100", domain.ErrValidation)
	}
	if query.Status != "" && query.Status != domain.FindingOpen && query.Status != domain.FindingResolved {
		return FindingPage{}, fmt.Errorf("%w: 未知冲突状态 %s", domain.ErrValidation, query.Status)
	}
	if query.SequenceFrom < 0 || query.SequenceTo < 0 || (query.SequenceTo > 0 && query.SequenceFrom > query.SequenceTo) {
		return FindingPage{}, fmt.Errorf("%w: 片段范围无效", domain.ErrValidation)
	}
	rules, participants := map[string]bool{}, map[string]bool{}
	sequences := map[string]int{}
	for _, f := range c.Findings {
		rules[f.RuleCode], participants[f.ParticipantName] = true, true
	}
	for _, segment := range c.Segments {
		sequences[segment.ID] = segment.Sequence
	}
	if query.RuleCode != "" && !rules[query.RuleCode] {
		return FindingPage{}, fmt.Errorf("%w: 未知规则筛选值 %s", domain.ErrValidation, query.RuleCode)
	}
	if query.Participant != "" && !participants[query.Participant] {
		return FindingPage{}, fmt.Errorf("%w: 未知受访者筛选值 %s", domain.ErrValidation, query.Participant)
	}
	signature := strings.Join([]string{query.RuleCode, query.Participant, string(query.Status), strconv.Itoa(query.SequenceFrom), strconv.Itoa(query.SequenceTo), strconv.Itoa(query.PageSize)}, "|")
	offset := 0
	if query.Cursor != "" {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(query.Cursor)
		if decodeErr != nil {
			return FindingPage{}, fmt.Errorf("%w: 游标格式无效", domain.ErrValidation)
		}
		var cursor findingCursor
		if json.Unmarshal(raw, &cursor) != nil {
			return FindingPage{}, fmt.Errorf("%w: 游标格式无效", domain.ErrValidation)
		}
		if cursor.CaseVersion != c.Version || cursor.Signature != signature {
			return FindingPage{}, fmt.Errorf("%w: 分页游标已经过期", domain.ErrCursorStale)
		}
		offset = cursor.Offset
	}
	items := make([]domain.ConflictFinding, 0)
	for _, finding := range c.Findings {
		sequence := sequences[finding.SegmentID]
		if query.RuleCode != "" && finding.RuleCode != query.RuleCode {
			continue
		}
		if query.Participant != "" && finding.ParticipantName != query.Participant {
			continue
		}
		if query.Status != "" && finding.Status != query.Status {
			continue
		}
		if query.SequenceFrom > 0 && sequence < query.SequenceFrom {
			continue
		}
		if query.SequenceTo > 0 && sequence > query.SequenceTo {
			continue
		}
		items = append(items, finding)
	}
	sort.Slice(items, func(i, j int) bool {
		si, sj := sequences[items[i].SegmentID], sequences[items[j].SegmentID]
		if si != sj {
			return si < sj
		}
		if items[i].RuleCode != items[j].RuleCode {
			return items[i].RuleCode < items[j].RuleCode
		}
		return items[i].ID < items[j].ID
	})
	if offset < 0 || offset > len(items) {
		return FindingPage{}, fmt.Errorf("%w: 分页游标已经过期", domain.ErrCursorStale)
	}
	end := offset + query.PageSize
	if end > len(items) {
		end = len(items)
	}
	page := FindingPage{Items: append([]domain.ConflictFinding(nil), items[offset:end]...), Statistics: buildFindingStatistics(c), Current: c.ScanCurrent()}
	if end < len(items) {
		encoded, _ := json.Marshal(findingCursor{CaseVersion: c.Version, Offset: end, Signature: signature})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return page, nil
}

func buildFindingStatistics(c *domain.ReleaseCase) FindingStatistics {
	rules, people, segments, statuses := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	affected := map[string]bool{}
	open := 0
	scanRevision, scanConsent, scanContent := c.LastScanRevision, c.LastScanConsent, c.LastScanRevision
	for _, f := range c.Findings {
		rules[f.RuleCode]++
		people[f.ParticipantName]++
		segments[f.SegmentID]++
		statuses[string(f.Status)]++
		affected[f.SegmentID] = true
		if f.Status == domain.FindingOpen {
			open++
		}
		if c.LastScanRevision == 0 && f.ScanRevision > scanRevision {
			scanRevision = f.ScanRevision
		}
		if c.LastScanConsent == 0 && f.ConsentRevision > scanConsent {
			scanConsent = f.ConsentRevision
		}
		if c.LastScanRevision == 0 && f.ContentRevision > scanContent {
			scanContent = f.ContentRevision
		}
	}
	toList := func(values map[string]int) []FindingCount {
		out := make([]FindingCount, 0, len(values))
		for key, count := range values {
			out = append(out, FindingCount{key, count})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		return out
	}
	return FindingStatistics{ByRule: toList(rules), ByParticipant: toList(people), BySegment: toList(segments), ByStatus: toList(statuses), AffectedSegmentCount: len(affected), OpenCount: open, ScanRevision: scanRevision, ConsentRevision: scanConsent, ContentRevision: scanContent, Current: c.ScanCurrent()}
}

type CredentialPackage struct {
	CredentialID       string    `json:"credentialId"`
	CaseID             string    `json:"caseId"`
	CaseVersion        int64     `json:"caseVersion"`
	ConsentRevision    int64     `json:"consentRevision"`
	IncludedSegmentIDs []string  `json:"includedSegmentIds"`
	ReviewerName       string    `json:"reviewerName"`
	ApprovedAt         time.Time `json:"approvedAt"`
	EventSequence      int64     `json:"eventSequence"`
	CanonicalDigest    string    `json:"canonicalDigest"`
}

func (s *Service) ExportCredential(caseID string) ([]byte, string, error) {
	c, err := s.repo.Get(caseID)
	if err != nil {
		return nil, "", err
	}
	if c.Status != domain.StatusReleased || c.Credential == nil {
		return nil, "", domain.ErrCredentialMissing
	}
	cr := c.Credential
	pkg := CredentialPackage{cr.ID, c.ID, cr.CaseVersion, cr.ConsentRevision, append([]string(nil), cr.IncludedSegmentIDs...), cr.ReviewerName, cr.ApprovedAt, cr.EventSequence, cr.CanonicalDigest}
	b, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return nil, "", err
	}
	b = append(b, '\n')
	return b, "release-credential-" + c.ID + ".json", nil
}

type CredentialReport struct {
	Valid            bool                       `json:"valid"`
	Checks           []policy.VerificationCheck `json:"checks"`
	RecordCount      int64                      `json:"recordCount"`
	AnchoredSequence int64                      `json:"anchoredSequence"`
	ChainTailHash    string                     `json:"chainTailHash"`
	AnchoredHash     string                     `json:"anchoredHash"`
}

func (s *Service) VerifyDetailed(caseID string) (CredentialReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.repo.Get(caseID)
	if err != nil {
		return CredentialReport{}, err
	}
	// 每次校验都从磁盘重新读取日志并校验散列链，确保发布凭据签发后
	// JSON Lines 日志被原地篡改时，再次校验能够立即发现损坏并返回
	// journal.ErrCorruptJournal，而不是返回过期的有效报告。
	integrity, err := s.repo.VerifyIntegrity()
	if err != nil {
		return CredentialReport{}, err
	}
	base := policy.VerifyCredentialDetailed(c)
	report := CredentialReport{Valid: base.Valid, Checks: base.Checks, RecordCount: integrity.RecordCount, ChainTailHash: integrity.LastHash}
	if c.Credential == nil {
		return report, nil
	}
	report.AnchoredSequence = c.Credential.EventSequence
	report.AnchoredHash, err = s.repo.HashAtSequence(c.Credential.EventSequence)
	if err != nil {
		return CredentialReport{}, err
	}
	passed := c.Credential.EventSequence <= integrity.RecordCount
	report.Checks = append(report.Checks, policy.VerificationCheck{Code: "log_chain_anchor", Passed: passed, Message: "凭据事件序号位于连续日志链内"})
	if !passed {
		report.Valid = false
	}
	return report, nil
}
