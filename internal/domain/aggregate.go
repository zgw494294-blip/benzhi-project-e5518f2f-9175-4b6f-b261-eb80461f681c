package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (c *ReleaseCase) FreezeConsents(inputs []ParticipantConsent, now time.Time) error {
	if c.Status != StatusDraft {
		return fmt.Errorf("%w: 仅草拟中发布案可冻结授权", ErrInvalidState)
	}
	if len(inputs) == 0 {
		return fmt.Errorf("%w: 至少登记一位受访者", ErrValidation)
	}
	seen := map[string]bool{}
	c.ConsentRevision++
	consents := make([]ParticipantConsent, 0, len(inputs))
	for i, in := range inputs {
		name := strings.TrimSpace(in.ParticipantName)
		if name == "" || strings.TrimSpace(in.EvidenceDigest) == "" {
			return fmt.Errorf("%w: 受访者姓名和授权证据摘要为必填", ErrValidation)
		}
		if seen[name] {
			return fmt.Errorf("%w: 受访者重复: %s", ErrValidation, name)
		}
		seen[name] = true
		precision := strings.TrimSpace(in.LocationPrecision)
		if precision != "exact" && precision != "city" && precision != "province" && precision != "none" {
			return fmt.Errorf("%w: 地点精度无效", ErrValidation)
		}
		if in.EmbargoUntil != "" {
			if _, err := time.Parse("2006-01-02", in.EmbargoUntil); err != nil {
				return fmt.Errorf("%w: 禁用日期格式错误", ErrValidation)
			}
		}
		in.ID = fmt.Sprintf("consent-%d-%d", c.ConsentRevision, i+1)
		in.CaseID = c.ID
		in.ParticipantName = name
		in.EvidenceDigest = strings.TrimSpace(in.EvidenceDigest)
		in.RestrictedTopics = normalizeTerms(in.RestrictedTopics)
		in.LocationPrecision = precision
		in.Revision = c.ConsentRevision
		in.FrozenAt = now.UTC()
		consents = append(consents, in)
	}
	sort.Slice(consents, func(i, j int) bool { return consents[i].ParticipantName < consents[j].ParticipantName })
	c.Consents = consents
	c.LastScanConsent = 0
	c.NeedsFullScan = true
	c.ConsentHistory = append(c.ConsentHistory, ConsentRevision{Revision: c.ConsentRevision, Reason: "初始授权边界冻结", FrozenAt: now.UTC(), Consents: cloneConsents(consents)})
	c.touch("consents.frozen", now, map[string]any{"revision": c.ConsentRevision, "count": len(consents)})
	return nil
}

func (c *ReleaseCase) AddSegment(in RecordingSegment, now time.Time) error {
	if c.Status != StatusDraft {
		return fmt.Errorf("%w: 仅草拟中可维护片段目录", ErrInvalidState)
	}
	if len(c.Consents) == 0 {
		return fmt.Errorf("%w: 请先冻结授权边界", ErrInvalidState)
	}
	if in.StartMillis < 0 || in.EndMillis <= in.StartMillis || strings.TrimSpace(in.Summary) == "" {
		return fmt.Errorf("%w: 片段时间和摘要无效", ErrValidation)
	}
	for _, existing := range c.Segments {
		if in.StartMillis < existing.EndMillis && in.EndMillis > existing.StartMillis {
			return fmt.Errorf("%w: 片段时间与 %s 重叠", ErrValidation, existing.ID)
		}
	}
	c.ContentRevision++
	in.ID = fmt.Sprintf("segment-%d", c.ContentRevision)
	in.CaseID = c.ID
	in.Summary = strings.TrimSpace(in.Summary)
	in.MentionedParticipants = normalizeTerms(in.MentionedParticipants)
	in.TopicTags = normalizeTerms(in.TopicTags)
	in.LocationTag = strings.TrimSpace(in.LocationTag)
	in.ContentRevision = c.ContentRevision
	c.Segments = append(c.Segments, in)
	sort.Slice(c.Segments, func(i, j int) bool { return c.Segments[i].StartMillis < c.Segments[j].StartMillis })
	for i := range c.Segments {
		c.Segments[i].Sequence = i + 1
	}
	c.LastScanRevision = 0
	c.NeedsFullScan = true
	c.touch("segment.added", now, map[string]any{"segmentId": in.ID})
	return nil
}

func (c *ReleaseCase) ApplyScan(seeds []FindingSeed, full bool, segmentIDs map[string]bool, now time.Time) error {
	if len(c.Consents) == 0 || len(c.Segments) == 0 {
		return fmt.Errorf("%w: 授权和片段目录不能为空", ErrInvalidState)
	}
	if c.Status == StatusReview || c.Status == StatusReleased {
		return fmt.Errorf("%w: 当前状态不可扫描", ErrInvalidState)
	}
	if full {
		c.Findings = nil
	} else {
		kept := c.Findings[:0]
		for _, f := range c.Findings {
			if !segmentIDs[f.SegmentID] || f.Status == FindingResolved {
				kept = append(kept, f)
			}
		}
		c.Findings = kept
	}
	for _, seed := range seeds {
		c.Findings = append(c.Findings, ConflictFinding{ID: seed.ID, CaseID: c.ID, SegmentID: seed.SegmentID, RuleCode: seed.RuleCode, Reason: seed.Reason, ScanRevision: c.ContentRevision, ConsentRevision: c.ConsentRevision, ContentRevision: c.ContentRevision, ParticipantName: seed.ParticipantName, Basis: seed.Basis, Status: FindingOpen})
	}
	sort.Slice(c.Findings, func(i, j int) bool {
		if c.Findings[i].SegmentID != c.Findings[j].SegmentID {
			return c.Findings[i].SegmentID < c.Findings[j].SegmentID
		}
		if c.Findings[i].RuleCode != c.Findings[j].RuleCode {
			return c.Findings[i].RuleCode < c.Findings[j].RuleCode
		}
		return c.Findings[i].ID < c.Findings[j].ID
	})
	c.LastScanRevision = c.ContentRevision
	c.LastScanConsent = c.ConsentRevision
	if full {
		c.NeedsFullScan = false
	}
	if c.OpenFindingCount() > 0 {
		c.Status = StatusRemediation
	} else {
		c.Status = StatusReady
	}
	c.touch("scan.completed", now, map[string]any{"full": full, "findings": len(seeds), "contentRevision": c.ContentRevision})
	return nil
}

func (c *ReleaseCase) Remediate(findingID string, remediation RemediationType, before, after string, now time.Time) (string, error) {
	if c.Status != StatusRemediation {
		return "", fmt.Errorf("%w: 当前不在整改阶段", ErrInvalidState)
	}
	if strings.TrimSpace(before) == "" || strings.TrimSpace(after) == "" {
		return "", fmt.Errorf("%w: 整改前后说明不能为空", ErrValidation)
	}
	idx := -1
	for i := range c.Findings {
		if c.Findings[i].ID == findingID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("%w: 冲突不存在", ErrNotFound)
	}
	if c.Findings[idx].Status != FindingOpen {
		return "", fmt.Errorf("%w: 冲突已经关闭", ErrInvalidState)
	}
	segmentID := c.Findings[idx].SegmentID
	seg := -1
	for i := range c.Segments {
		if c.Segments[i].ID == segmentID {
			seg = i
			break
		}
	}
	if seg < 0 {
		return "", fmt.Errorf("%w: 关联片段不存在", ErrNotFound)
	}
	ruleCode := c.Findings[idx].RuleCode
	if remediation == RemediationPseudonym && ruleCode != "IDENTITY" {
		return "", fmt.Errorf("%w: 化名仅适用于身份公开冲突", ErrValidation)
	}
	if remediation == RemediationGeneralize && ruleCode != "LOCATION" {
		return "", fmt.Errorf("%w: 地点泛化仅适用于地点精度冲突", ErrValidation)
	}
	switch remediation {
	case RemediationMute:
		c.Segments[seg].Muted = true
	case RemediationPseudonym:
		c.Segments[seg].Pseudonymized = true
	case RemediationGeneralize:
		c.Segments[seg].LocationGeneralized = true
	case RemediationDelete:
		c.Segments[seg].Deleted = true
	case RemediationSupplemental:
		hasStructuredEvidence := false
		for _, evidence := range c.SupplementalConsents {
			if evidence.FindingID == findingID {
				hasStructuredEvidence = true
				break
			}
		}
		if !hasStructuredEvidence {
			// 保留旧应用调用的兼容语义；公开 HTTP 入口要求先登记结构化证据。
			c.Segments[seg].SupplementalRules = normalizeTerms(append(c.Segments[seg].SupplementalRules, c.Findings[idx].RuleCode))
		}
	default:
		return "", fmt.Errorf("%w: 整改类型无效", ErrValidation)
	}
	c.ContentRevision++
	c.Segments[seg].ContentRevision = c.ContentRevision
	resolvedIDs := make([]string, 0, 1)
	for i := range c.Findings {
		finding := &c.Findings[i]
		if finding.SegmentID != segmentID || finding.Status != FindingOpen {
			continue
		}
		covered := finding.ID == findingID || remediation == RemediationMute || remediation == RemediationDelete || (remediation == RemediationPseudonym && finding.RuleCode == "IDENTITY") || (remediation == RemediationGeneralize && finding.RuleCode == "LOCATION")
		if !covered {
			continue
		}
		finding.Status = FindingResolved
		finding.RemediationType = remediation
		if finding.ID == findingID {
			finding.BeforeNote = strings.TrimSpace(before)
		} else {
			finding.BeforeNote = finding.Reason
		}
		finding.AfterNote = strings.TrimSpace(after)
		t := now.UTC()
		finding.ResolvedAt = &t
		resolvedIDs = append(resolvedIDs, finding.ID)
	}
	c.LastScanRevision = 0
	c.touch("finding.remediated", now, map[string]any{"findingId": findingID, "resolvedFindingIds": resolvedIDs, "segmentId": segmentID, "remediation": remediation})
	return segmentID, nil
}

func (c *ReleaseCase) SubmitReview(reviewer string, now time.Time) error {
	if c.Status != StatusReady {
		return fmt.Errorf("%w: 发布案尚未具备复核条件", ErrInvalidState)
	}
	if !c.ScanCurrent() {
		return ErrScanStale
	}
	if c.OpenFindingCount() != 0 {
		return ErrOpenFindings
	}
	if strings.TrimSpace(reviewer) == "" {
		return fmt.Errorf("%w: 复核员不能为空", ErrValidation)
	}
	c.Status = StatusReview
	round := int64(1)
	for _, prior := range c.ReviewHistory {
		if prior.Round >= round {
			round = prior.Round + 1
		}
	}
	if c.Review != nil && c.Review.Round >= round {
		round = c.Review.Round + 1
	}
	c.Review = &EthicsReview{ReviewerName: strings.TrimSpace(reviewer), SubmittedAt: now.UTC(), Decision: "pending", Round: round, ScanRevision: c.LastScanRevision, ConsentRevision: c.ConsentRevision, VerificationNotes: map[string]string{}}
	c.touch("review.submitted", now, map[string]any{"reviewer": reviewer})
	return nil
}

func (c *ReleaseCase) ReturnReview(findingIDs []string, note string, now time.Time) error {
	if c.Status != StatusReview || c.Review == nil {
		return fmt.Errorf("%w: 当前没有待处理复核", ErrInvalidState)
	}
	if len(findingIDs) == 0 || strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w: 退回时必须指定冲突和说明", ErrValidation)
	}
	wanted := map[string]bool{}
	for _, id := range findingIDs {
		wanted[id] = true
	}
	found := 0
	for i := range c.Findings {
		if wanted[c.Findings[i].ID] {
			found++
		}
	}
	if found != len(wanted) {
		return fmt.Errorf("%w: 指定冲突不存在", ErrValidation)
	}
	for i := range c.Findings {
		if wanted[c.Findings[i].ID] {
			c.Findings[i].Status = FindingOpen
			c.Findings[i].ResolvedAt = nil
		}
	}
	c.Review.Decision = "returned"
	c.Review.Note = strings.TrimSpace(note)
	c.Review.FindingIDs = append([]string(nil), findingIDs...)
	c.Review.DecidedAt = now.UTC()
	c.ReviewHistory = append(c.ReviewHistory, ReviewProgress{Round: c.Review.Round, ScanRevision: c.Review.ScanRevision, ConsentRevision: c.Review.ConsentRevision, VerifiedFindingIDs: append([]string(nil), c.Review.VerifiedFindingIDs...), Notes: cloneStringMap(c.Review.VerificationNotes), SavedAt: c.Review.LastProgressSavedAt, ClosedAt: now.UTC()})
	c.Status = StatusRemediation
	c.touch("review.returned", now, map[string]any{"findingIds": findingIDs, "note": note})
	return nil
}

func (c *ReleaseCase) Approve(credential ReleaseCredential, verifiedFindingIDs []string, now time.Time) error {
	if c.Status != StatusReview || c.Review == nil {
		return fmt.Errorf("%w: 当前没有待批准复核", ErrInvalidState)
	}
	if c.Credential != nil {
		return ErrCredentialExists
	}
	if !c.ScanCurrent() {
		return ErrScanStale
	}
	if c.OpenFindingCount() > 0 {
		return ErrOpenFindings
	}
	if credential.CanonicalDigest == "" || len(credential.IncludedSegmentIDs) == 0 {
		return fmt.Errorf("%w: 凭据摘要或公开片段不能为空", ErrValidation)
	}
	required := map[string]bool{}
	for _, finding := range c.Findings {
		if finding.Status == FindingResolved {
			required[finding.ID] = true
		}
	}
	if len(verifiedFindingIDs) == 0 {
		verifiedFindingIDs = c.Review.VerifiedFindingIDs
	}
	verified := map[string]bool{}
	for _, id := range verifiedFindingIDs {
		if verified[id] || !required[id] {
			return fmt.Errorf("%w: 逐项核验清单包含重复或未知冲突", ErrValidation)
		}
		verified[id] = true
	}
	if len(verified) != len(required) {
		missing := make([]string, 0)
		for id := range required {
			if !verified[id] {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		return &ReviewIncompleteError{FindingIDs: missing}
	}
	c.Review.VerifiedFindingIDs = normalizeTerms(verifiedFindingIDs)
	c.Review.Decision = "approved"
	c.Review.DecidedAt = now.UTC()
	c.Status = StatusReleased
	c.Credential = &credential
	c.touch("credential.issued", now, map[string]any{"credentialId": credential.ID, "digest": credential.CanonicalDigest})
	c.Credential.CaseVersion = c.Version
	return nil
}
