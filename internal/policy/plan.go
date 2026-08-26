package policy

import (
	"fmt"
	"sort"

	"oral-history-clearance/internal/domain"
)

// ScanPlan 将“扫描哪些片段”与规则执行分离。计划记录生成时看到的两类修订号，
// 防止应用层把旧计划套用到后来变化的授权或材料上。
type ScanPlan struct {
	Full            bool     `json:"full"`
	SegmentIDs      []string `json:"segmentIds"`
	ContentRevision int64    `json:"contentRevision"`
	ConsentRevision int64    `json:"consentRevision"`
	Reason          string   `json:"reason"`
}

func NewFullScanPlan(c *domain.ReleaseCase) (ScanPlan, error) {
	if c == nil {
		return ScanPlan{}, fmt.Errorf("%w: 发布案不能为空", domain.ErrValidation)
	}
	if len(c.Consents) == 0 {
		return ScanPlan{}, fmt.Errorf("%w: 授权边界尚未冻结", domain.ErrInvalidState)
	}
	if len(c.Segments) == 0 {
		return ScanPlan{}, fmt.Errorf("%w: 片段目录为空", domain.ErrInvalidState)
	}
	if c.Status == domain.StatusReview || c.Status == domain.StatusReleased {
		return ScanPlan{}, fmt.Errorf("%w: 复核中或已放行发布案不可重新扫描", domain.ErrInvalidState)
	}
	ids := make([]string, 0, len(c.Segments))
	for _, segment := range c.Segments {
		if !segment.Deleted {
			ids = append(ids, segment.ID)
		}
	}
	if len(ids) == 0 {
		return ScanPlan{}, fmt.Errorf("%w: 没有可扫描的未删除片段", domain.ErrInvalidState)
	}
	sort.Strings(ids)
	return ScanPlan{Full: true, SegmentIDs: ids, ContentRevision: c.ContentRevision, ConsentRevision: c.ConsentRevision, Reason: "全量比对冻结授权边界与片段目录"}, nil
}

func NewTargetedScanPlan(c *domain.ReleaseCase, affected map[string]bool) (ScanPlan, error) {
	if c == nil {
		return ScanPlan{}, fmt.Errorf("%w: 发布案不能为空", domain.ErrValidation)
	}
	if c.NeedsFullScan {
		return ScanPlan{}, fmt.Errorf("%w: 授权、公开日期或目录已变化，必须执行全量扫描", domain.ErrScanStale)
	}
	if len(affected) == 0 {
		return ScanPlan{}, fmt.Errorf("%w: 定向重扫范围为空", domain.ErrValidation)
	}
	known := map[string]bool{}
	for _, segment := range c.Segments {
		known[segment.ID] = true
	}
	ids := make([]string, 0, len(affected))
	for id, selected := range affected {
		if !selected {
			continue
		}
		if !known[id] {
			return ScanPlan{}, fmt.Errorf("%w: 定向重扫片段 %s 不存在", domain.ErrNotFound, id)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ScanPlan{}, fmt.Errorf("%w: 定向重扫范围没有选中片段", domain.ErrValidation)
	}
	sort.Strings(ids)
	return ScanPlan{Full: false, SegmentIDs: ids, ContentRevision: c.ContentRevision, ConsentRevision: c.ConsentRevision, Reason: "整改后仅重扫受影响片段"}, nil
}

func (s *Scanner) Execute(c *domain.ReleaseCase, plan ScanPlan) ([]domain.FindingSeed, map[string]bool, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("%w: 发布案不能为空", domain.ErrValidation)
	}
	if plan.ContentRevision != c.ContentRevision || plan.ConsentRevision != c.ConsentRevision {
		return nil, nil, domain.ErrScanStale
	}
	ids := make(map[string]bool, len(plan.SegmentIDs))
	known := map[string]bool{}
	for _, segment := range c.Segments {
		known[segment.ID] = true
	}
	for _, id := range plan.SegmentIDs {
		if ids[id] {
			return nil, nil, fmt.Errorf("%w: 扫描计划包含重复片段", domain.ErrValidation)
		}
		if !known[id] {
			return nil, nil, fmt.Errorf("%w: 扫描计划引用未知片段", domain.ErrNotFound)
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("%w: 扫描计划为空", domain.ErrValidation)
	}
	return s.ScanSegments(c, ids), ids, nil
}

type VerificationCheck struct {
	Code    string `json:"code"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type CredentialVerification struct {
	Valid  bool                `json:"valid"`
	Checks []VerificationCheck `json:"checks"`
}

func VerifyCredentialDetailed(c *domain.ReleaseCase) CredentialVerification {
	result := CredentialVerification{Valid: true}
	add := func(code string, passed bool, message string) {
		result.Checks = append(result.Checks, VerificationCheck{Code: code, Passed: passed, Message: message})
		if !passed {
			result.Valid = false
		}
	}
	if c == nil || c.Credential == nil {
		add("credential_present", false, "发布凭据不存在")
		return result
	}
	credential := c.Credential
	add("credential_present", true, "发布凭据存在")
	add("released_state", c.Status == domain.StatusReleased, "发布案处于冻结放行状态")
	add("consent_revision", credential.ConsentRevision == c.ConsentRevision, "凭据授权修订与冻结快照一致")
	add("case_version", credential.CaseVersion == c.Version, "凭据案卷版本与不可变聚合一致")
	add("event_sequence", credential.EventSequence > 0, "凭据包含有效日志事件序号")
	included := c.IncludedSegments()
	listMatches := len(included) == len(credential.IncludedSegmentIDs)
	if listMatches {
		for i := range included {
			if included[i].ID != credential.IncludedSegmentIDs[i] {
				listMatches = false
				break
			}
		}
	}
	add("segment_manifest", listMatches, "公开片段清单与冻结材料一致")
	expected := Digest(c, credential.ReviewerName, credential.EventSequence)
	add("canonical_digest", expected == credential.CanonicalDigest, "规范化授权与片段摘要可复算")
	return result
}
