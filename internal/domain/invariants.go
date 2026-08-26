package domain

import (
	"fmt"
	"strings"
	"time"
)

// ValidateInvariants 校验聚合完整性。命令方法负责建立这些不变量，持久化边界
// 再次执行校验，以便将磁盘损坏与业务拒绝区分开来。
func (c *ReleaseCase) ValidateInvariants() error {
	if c == nil {
		return invariant("发布案为空")
	}
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.ArchiveCode) == "" || strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.EditorName) == "" {
		return invariant("发布案必填字段缺失")
	}
	if _, err := time.Parse("2006-01-02", c.TargetPublishDate); err != nil {
		return invariant("目标公开日期无效")
	}
	if c.Version < 1 || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return invariant("版本或时间戳无效")
	}
	if !knownStatus(c.Status) {
		return invariant("发布案状态未知: %s", c.Status)
	}
	if c.ConsentRevision < 0 || c.ContentRevision < 0 || c.LastScanRevision < 0 || c.LastScanConsent < 0 {
		return invariant("修订号不得为负数")
	}
	if c.LastScanRevision > c.ContentRevision || c.LastScanConsent > c.ConsentRevision {
		return invariant("扫描修订号超出材料修订")
	}
	if err := c.validateConsents(); err != nil {
		return err
	}
	if len(c.ConsentHistory) != int(c.ConsentRevision) {
		return invariant("授权冻结历史与修订号不连续")
	}
	for i, revision := range c.ConsentHistory {
		if revision.Revision != int64(i+1) || revision.Reason == "" || revision.FrozenAt.IsZero() || len(revision.Consents) == 0 {
			return invariant("授权冻结历史无效")
		}
		seen := map[string]bool{}
		for _, consent := range revision.Consents {
			if consent.CaseID != c.ID || consent.Revision != revision.Revision || consent.ParticipantName == "" || consent.EvidenceDigest == "" || seen[consent.ParticipantName] {
				return invariant("授权冻结快照内容无效")
			}
			seen[consent.ParticipantName] = true
		}
	}
	segmentIDs, err := c.validateSegments()
	if err != nil {
		return err
	}
	if err := c.validateFindings(segmentIDs); err != nil {
		return err
	}
	if err := c.validateSupplementalConsents(segmentIDs); err != nil {
		return err
	}
	if err := c.validateEvents(); err != nil {
		return err
	}
	if err := c.validateState(); err != nil {
		return err
	}
	return c.validateCredential(segmentIDs)
}

func (c *ReleaseCase) validateSupplementalConsents(segmentIDs map[string]bool) error {
	seen := map[string]bool{}
	for _, evidence := range c.SupplementalConsents {
		if evidence.ID == "" || seen[evidence.ID] || evidence.FindingID == "" || evidence.ParticipantName == "" || evidence.EvidenceDigest == "" || evidence.AuthorizedDate == "" || evidence.RuleCode == "" || evidence.RecordedAt.IsZero() || len(evidence.SegmentIDs) == 0 {
			return invariant("补充授权证据无效")
		}
		if _, err := time.Parse("2006-01-02", evidence.AuthorizedDate); err != nil {
			return invariant("补充授权日期无效")
		}
		if !strictlySortedUnique(evidence.SegmentIDs) {
			return invariant("补充授权片段范围未规范化")
		}
		for _, id := range evidence.SegmentIDs {
			if !segmentIDs[id] {
				return invariant("补充授权引用未知片段")
			}
		}
		seen[evidence.ID] = true
	}
	return nil
}

func (c *ReleaseCase) validateConsents() error {
	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	for _, consent := range c.Consents {
		if consent.CaseID != c.ID || consent.ID == "" || seenIDs[consent.ID] {
			return invariant("授权记录 ID 或 caseId 无效")
		}
		if consent.ParticipantName == "" || seenNames[consent.ParticipantName] {
			return invariant("受访者姓名为空或重复")
		}
		if consent.EvidenceDigest == "" || consent.FrozenAt.IsZero() {
			return invariant("授权证据或冻结时间缺失")
		}
		if consent.Revision != c.ConsentRevision || consent.Revision < 1 {
			return invariant("授权记录修订号不一致")
		}
		if !knownPrecision(consent.LocationPrecision) {
			return invariant("地点精度未知: %s", consent.LocationPrecision)
		}
		if consent.EmbargoUntil != "" {
			if _, err := time.Parse("2006-01-02", consent.EmbargoUntil); err != nil {
				return invariant("授权禁用日期无效")
			}
		}
		if !strictlySortedUnique(consent.RestrictedTopics) {
			return invariant("限制话题必须规范化排序且不重复")
		}
		seenIDs[consent.ID] = true
		seenNames[consent.ParticipantName] = true
	}
	if len(c.Consents) == 0 && c.ConsentRevision != 0 {
		return invariant("无授权记录时授权修订号必须为零")
	}
	return nil
}

func (c *ReleaseCase) validateSegments() (map[string]bool, error) {
	ids := map[string]bool{}
	var priorEnd int64
	for i, segment := range c.Segments {
		if segment.CaseID != c.ID || segment.ID == "" || ids[segment.ID] {
			return nil, invariant("片段 ID 或 caseId 无效")
		}
		if segment.Sequence != i+1 {
			return nil, invariant("片段 sequence 不连续")
		}
		if segment.StartMillis < 0 || segment.EndMillis <= segment.StartMillis || segment.Summary == "" {
			return nil, invariant("片段时间或摘要无效")
		}
		if i > 0 && segment.StartMillis < priorEnd {
			return nil, invariant("片段时间范围重叠")
		}
		if segment.ContentRevision < 1 || segment.ContentRevision > c.ContentRevision {
			return nil, invariant("片段材料修订号无效")
		}
		if !strictlySortedUnique(segment.MentionedParticipants) || !strictlySortedUnique(segment.TopicTags) || !strictlySortedUnique(segment.SupplementalRules) {
			return nil, invariant("片段标签必须规范化排序且不重复")
		}
		ids[segment.ID] = true
		priorEnd = segment.EndMillis
	}
	if len(c.Segments) == 0 && c.ContentRevision != 0 {
		return nil, invariant("无片段时材料修订号必须为零")
	}
	return ids, nil
}

func (c *ReleaseCase) validateFindings(segmentIDs map[string]bool) error {
	ids := map[string]bool{}
	for _, finding := range c.Findings {
		if finding.ID == "" || ids[finding.ID] || finding.CaseID != c.ID || !segmentIDs[finding.SegmentID] {
			return invariant("冲突 ID、caseId 或 segmentId 无效")
		}
		if finding.RuleCode == "" || finding.Reason == "" || finding.ScanRevision < 1 || finding.ScanRevision > c.ContentRevision {
			return invariant("冲突规则依据或扫描修订无效")
		}
		if finding.Status != FindingOpen && finding.Status != FindingResolved {
			return invariant("冲突状态未知")
		}
		if finding.Status == FindingResolved {
			if finding.ResolvedAt == nil || finding.RemediationType == "" || finding.BeforeNote == "" || finding.AfterNote == "" {
				return invariant("已关闭冲突缺少整改审计信息")
			}
		} else if finding.ResolvedAt != nil {
			return invariant("未关闭冲突不应有 resolvedAt")
		}
		ids[finding.ID] = true
	}
	return nil
}

func (c *ReleaseCase) validateEvents() error {
	if len(c.Events) == 0 {
		return invariant("领域事件不能为空")
	}
	var prior int64
	for i, event := range c.Events {
		if event.Type == "" || event.OccurredAt.IsZero() {
			return invariant("领域事件类型或时间缺失")
		}
		if i == 0 && event.Version != 1 {
			return invariant("首个领域事件版本必须为 1")
		}
		if i > 0 && event.Version != prior+1 {
			return invariant("领域事件版本不连续")
		}
		prior = event.Version
	}
	if prior != c.Version {
		return invariant("末尾领域事件版本与聚合版本不一致")
	}
	return nil
}

func (c *ReleaseCase) validateState() error {
	open := c.OpenFindingCount()
	switch c.Status {
	case StatusDraft:
		if c.Credential != nil || (c.Review != nil && c.Review.Decision == "pending") {
			return invariant("草拟状态包含进行中的复核或凭据")
		}
		if len(c.Findings) > 0 && !c.NeedsFullScan {
			return invariant("带历史冲突的草拟状态必须等待全量扫描")
		}
	case StatusRemediation:
		if open == 0 || c.LastScanRevision == 0 {
			return invariant("待整改状态必须存在未关闭冲突和扫描记录")
		}
	case StatusReady:
		if open != 0 || !c.ScanCurrent() {
			return invariant("待复核状态必须无未关闭冲突且扫描有效")
		}
	case StatusReview:
		if c.Review == nil || c.Review.Decision != "pending" || c.Review.Round < 1 || c.Review.ScanRevision != c.LastScanRevision || c.Review.ConsentRevision != c.ConsentRevision || open != 0 || !c.ScanCurrent() {
			return invariant("复核中状态不完整")
		}
	case StatusReleased:
		if c.Review == nil || c.Review.Decision != "approved" || c.Credential == nil || open != 0 {
			return invariant("发布放行状态不完整")
		}
		resolved := map[string]bool{}
		for _, finding := range c.Findings {
			if finding.Status == FindingResolved {
				resolved[finding.ID] = true
			}
		}
		verified := map[string]bool{}
		for _, id := range c.Review.VerifiedFindingIDs {
			if verified[id] || !resolved[id] {
				return invariant("复核核验清单包含重复或未知冲突")
			}
			verified[id] = true
		}
		if len(verified) != len(resolved) {
			return invariant("复核核验清单未覆盖全部已关闭冲突")
		}
	}
	return nil
}

func (c *ReleaseCase) validateCredential(segmentIDs map[string]bool) error {
	if c.Credential == nil {
		return nil
	}
	credential := c.Credential
	if credential.ID == "" || credential.CaseID != c.ID || credential.CanonicalDigest == "" || credential.ReviewerName == "" || credential.ApprovedAt.IsZero() || credential.EventSequence < 1 {
		return invariant("发布凭据必填字段无效")
	}
	if credential.ConsentRevision != c.ConsentRevision || credential.CaseVersion != c.Version {
		return invariant("发布凭据冻结修订与聚合不一致")
	}
	if len(credential.IncludedSegmentIDs) == 0 {
		return invariant("发布凭据公开片段清单为空")
	}
	seen := map[string]bool{}
	for _, id := range credential.IncludedSegmentIDs {
		if seen[id] || !segmentIDs[id] {
			return invariant("发布凭据片段引用无效")
		}
		seen[id] = true
	}
	for _, segment := range c.Segments {
		if segment.Deleted == seen[segment.ID] {
			return invariant("发布凭据清单与删除标记不一致")
		}
	}
	included := c.IncludedSegments()
	for i, segment := range included {
		if credential.IncludedSegmentIDs[i] != segment.ID {
			return invariant("发布凭据片段顺序与冻结清单不一致")
		}
	}
	return nil
}

func knownStatus(status CaseStatus) bool {
	return status == StatusDraft || status == StatusRemediation || status == StatusReady || status == StatusReview || status == StatusReleased
}

func knownPrecision(value string) bool {
	return value == "none" || value == "province" || value == "city" || value == "exact"
}

func strictlySortedUnique(values []string) bool {
	for i, value := range values {
		if strings.TrimSpace(value) == "" || (i > 0 && values[i-1] >= value) {
			return false
		}
	}
	return true
}

func invariant(format string, args ...any) error {
	return fmt.Errorf("聚合不变量失败: "+format, args...)
}
