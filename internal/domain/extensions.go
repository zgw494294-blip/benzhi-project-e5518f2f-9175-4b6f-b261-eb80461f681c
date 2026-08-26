package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type ConsentDifference struct {
	Kind            string `json:"kind"`
	ParticipantName string `json:"participantName"`
	Field           string `json:"field,omitempty"`
	Before          string `json:"before,omitempty"`
	After           string `json:"after,omitempty"`
}

type SegmentInput struct {
	Row                   int      `json:"row"`
	Sequence              int      `json:"sequence,omitempty"`
	StartMillis           int64    `json:"startMillis"`
	EndMillis             int64    `json:"endMillis"`
	Summary               string   `json:"summary"`
	MentionedParticipants []string `json:"mentionedParticipants"`
	TopicTags             []string `json:"topicTags"`
	LocationTag           string   `json:"locationTag,omitempty"`
}

type SegmentProblem struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SegmentPreview struct {
	Rows     []SegmentInput   `json:"rows"`
	Problems []SegmentProblem `json:"problems"`
	Valid    bool             `json:"valid"`
}

type BatchRemediationItem struct {
	FindingID string `json:"findingId"`
	Outcome   string `json:"outcome"`
}

func (c *ReleaseCase) BatchRemediate(findingIDs []string, remediation RemediationType, before, after string, now time.Time) ([]BatchRemediationItem, map[string]bool, error) {
	if c.Status != StatusRemediation {
		return nil, nil, fmt.Errorf("%w: 当前不在整改阶段", ErrInvalidState)
	}
	ids := normalizeTerms(append(append([]string(nil), c.Review.VerifiedFindingIDs...), findingIDs...))
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("%w: 至少选择一个冲突", ErrValidation)
	}
	if len(ids) != len(findingIDs) {
		return nil, nil, fmt.Errorf("%w: findingId 不得重复", ErrValidation)
	}
	before, after = strings.TrimSpace(before), strings.TrimSpace(after)
	if before == "" || after == "" {
		return nil, nil, fmt.Errorf("%w: 整改前后说明不能为空", ErrValidation)
	}
	selected := map[string]*ConflictFinding{}
	for _, id := range ids {
		for i := range c.Findings {
			if c.Findings[i].ID == id {
				selected[id] = &c.Findings[i]
				break
			}
		}
		finding := selected[id]
		if finding == nil {
			return nil, nil, fmt.Errorf("%w: 冲突 %s 不存在", ErrNotFound, id)
		}
		if finding.Status != FindingOpen {
			return nil, nil, fmt.Errorf("%w: 冲突 %s 已经关闭", ErrInvalidState, id)
		}
		if remediation == RemediationPseudonym && finding.RuleCode != "IDENTITY" {
			return nil, nil, fmt.Errorf("%w: 化名仅兼容 IDENTITY 冲突", ErrValidation)
		}
		if remediation == RemediationGeneralize && finding.RuleCode != "LOCATION" {
			return nil, nil, fmt.Errorf("%w: 地点泛化仅兼容 LOCATION 冲突", ErrValidation)
		}
		if remediation == RemediationSupplemental {
			return nil, nil, fmt.Errorf("%w: 补充授权需逐项登记结构化证据", ErrValidation)
		}
	}
	if remediation != RemediationMute && remediation != RemediationDelete && remediation != RemediationPseudonym && remediation != RemediationGeneralize {
		return nil, nil, fmt.Errorf("%w: 批量整改类型无效", ErrValidation)
	}
	affected := map[string]bool{}
	for _, finding := range selected {
		affected[finding.SegmentID] = true
	}
	c.ContentRevision++
	for i := range c.Segments {
		if !affected[c.Segments[i].ID] {
			continue
		}
		switch remediation {
		case RemediationMute:
			c.Segments[i].Muted = true
		case RemediationDelete:
			c.Segments[i].Deleted = true
		case RemediationPseudonym:
			c.Segments[i].Pseudonymized = true
		case RemediationGeneralize:
			c.Segments[i].LocationGeneralized = true
		}
		c.Segments[i].ContentRevision = c.ContentRevision
	}
	results := make([]BatchRemediationItem, 0, len(ids))
	for i := range c.Findings {
		finding := &c.Findings[i]
		if !affected[finding.SegmentID] || finding.Status != FindingOpen {
			continue
		}
		covered := selected[finding.ID] != nil || remediation == RemediationMute || remediation == RemediationDelete || (remediation == RemediationPseudonym && finding.RuleCode == "IDENTITY") || (remediation == RemediationGeneralize && finding.RuleCode == "LOCATION")
		if !covered {
			continue
		}
		finding.Status, finding.RemediationType, finding.AfterNote = FindingResolved, remediation, after
		if selected[finding.ID] != nil {
			finding.BeforeNote = before
		} else {
			finding.BeforeNote = finding.Reason
		}
		t := now.UTC()
		finding.ResolvedAt = &t
		results = append(results, BatchRemediationItem{FindingID: finding.ID, Outcome: "closed"})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].FindingID < results[j].FindingID })
	c.LastScanRevision = 0
	c.touch("findings.batch_remediated", now, map[string]any{"findingIds": ids, "remediation": remediation, "segmentCount": len(affected)})
	return results, affected, nil
}

func editableStatus(status CaseStatus) bool {
	return status == StatusDraft || status == StatusRemediation || status == StatusReady
}

func (c *ReleaseCase) ReviseProfile(title, editor, target string, now time.Time) error {
	if !editableStatus(c.Status) {
		return fmt.Errorf("%w: 复核中或已放行发布案不可修改基础资料", ErrInvalidState)
	}
	title = strings.TrimSpace(title)
	editor = strings.TrimSpace(editor)
	if title == "" || editor == "" {
		return fmt.Errorf("%w: 标题和整理员不能为空", ErrValidation)
	}
	if _, err := time.Parse("2006-01-02", target); err != nil {
		return fmt.Errorf("%w: 目标公开日期格式应为 YYYY-MM-DD", ErrValidation)
	}
	changes := map[string]any{}
	if title != c.Title {
		changes["title"] = map[string]string{"before": c.Title, "after": title}
	}
	if editor != c.EditorName {
		changes["editorName"] = map[string]string{"before": c.EditorName, "after": editor}
	}
	dateChanged := target != c.TargetPublishDate
	if dateChanged {
		changes["targetPublishDate"] = map[string]string{"before": c.TargetPublishDate, "after": target}
	}
	if len(changes) == 0 {
		return fmt.Errorf("%w: 基础资料没有实际变化", ErrValidation)
	}
	c.Title, c.EditorName, c.TargetPublishDate = title, editor, target
	if dateChanged {
		c.LastScanRevision, c.LastScanConsent = 0, 0
		c.NeedsFullScan = true
		if c.Status == StatusRemediation || c.Status == StatusReady {
			c.Status = StatusDraft
		}
	}
	c.touch("case.profile_revised", now, changes)
	return nil
}

func normalizeConsentInputs(inputs []ParticipantConsent, revision int64, caseID string, now time.Time) ([]ParticipantConsent, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w: 至少登记一位受访者", ErrValidation)
	}
	seen := map[string]bool{}
	out := make([]ParticipantConsent, 0, len(inputs))
	for i, in := range inputs {
		name := strings.TrimSpace(in.ParticipantName)
		if name == "" || strings.TrimSpace(in.EvidenceDigest) == "" {
			return nil, fmt.Errorf("%w: 受访者姓名和授权证据摘要为必填", ErrValidation)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: 受访者重复: %s", ErrValidation, name)
		}
		seen[name] = true
		in.LocationPrecision = strings.TrimSpace(in.LocationPrecision)
		if !knownPrecision(in.LocationPrecision) {
			return nil, fmt.Errorf("%w: 地点精度无效: %s", ErrValidation, in.LocationPrecision)
		}
		if in.EmbargoUntil != "" {
			if _, err := time.Parse("2006-01-02", in.EmbargoUntil); err != nil {
				return nil, fmt.Errorf("%w: 禁用日期格式错误", ErrValidation)
			}
		}
		in.ID = fmt.Sprintf("consent-%d-%d", revision, i+1)
		in.CaseID, in.ParticipantName, in.EvidenceDigest = caseID, name, strings.TrimSpace(in.EvidenceDigest)
		in.RestrictedTopics = normalizeTerms(in.RestrictedTopics)
		in.Revision, in.FrozenAt = revision, now.UTC()
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ParticipantName < out[j].ParticipantName })
	for i := range out {
		out[i].ID = fmt.Sprintf("consent-%d-%d", revision, i+1)
	}
	return out, nil
}

func PreviewConsentDifferences(current, proposed []ParticipantConsent) []ConsentDifference {
	left, right := map[string]ParticipantConsent{}, map[string]ParticipantConsent{}
	for _, item := range current {
		left[item.ParticipantName] = item
	}
	for _, item := range proposed {
		right[item.ParticipantName] = item
	}
	names := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for name := range left {
		names = append(names, name)
		seen[name] = true
	}
	for name := range right {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []ConsentDifference
	for _, name := range names {
		a, aok := left[name]
		b, bok := right[name]
		if !aok {
			out = append(out, ConsentDifference{Kind: "participant_added", ParticipantName: name})
			continue
		}
		if !bok {
			out = append(out, ConsentDifference{Kind: "participant_removed", ParticipantName: name})
			continue
		}
		fields := []struct{ name, before, after string }{
			{"identityDisclosure", fmt.Sprint(a.IdentityDisclosure), fmt.Sprint(b.IdentityDisclosure)},
			{"restrictedTopics", strings.Join(normalizeTerms(a.RestrictedTopics), "、"), strings.Join(normalizeTerms(b.RestrictedTopics), "、")},
			{"locationPrecision", a.LocationPrecision, b.LocationPrecision}, {"embargoUntil", a.EmbargoUntil, b.EmbargoUntil}, {"evidenceDigest", strings.TrimSpace(a.EvidenceDigest), strings.TrimSpace(b.EvidenceDigest)},
		}
		for _, field := range fields {
			if field.before != field.after {
				out = append(out, ConsentDifference{Kind: "field_changed", ParticipantName: name, Field: field.name, Before: field.before, After: field.after})
			}
		}
	}
	return out
}

func (c *ReleaseCase) PrepareConsentRevision(inputs []ParticipantConsent, now time.Time) ([]ParticipantConsent, []ConsentDifference, error) {
	if !editableStatus(c.Status) {
		return nil, nil, fmt.Errorf("%w: 当前状态不可修订授权边界", ErrInvalidState)
	}
	normalized, err := normalizeConsentInputs(inputs, c.ConsentRevision+1, c.ID, now)
	if err != nil {
		return nil, nil, err
	}
	diffs := PreviewConsentDifferences(c.Consents, normalized)
	return normalized, diffs, nil
}

func (c *ReleaseCase) ReviseConsents(inputs []ParticipantConsent, reason string, baseRevision int64, now time.Time) error {
	if baseRevision != c.ConsentRevision {
		return fmt.Errorf("%w: 授权修订预览基准已过期", ErrVersionConflict)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: 授权修订原因不能为空", ErrValidation)
	}
	normalized, diffs, err := c.PrepareConsentRevision(inputs, now)
	if err != nil {
		return err
	}
	if len(diffs) == 0 {
		return fmt.Errorf("%w: 授权清单没有实际差异", ErrValidation)
	}
	known := map[string]bool{}
	for _, item := range normalized {
		known[item.ParticipantName] = true
	}
	var missing []string
	for _, segment := range c.Segments {
		if segment.Deleted {
			continue
		}
		for _, name := range segment.MentionedParticipants {
			if !known[name] {
				missing = append(missing, fmt.Sprintf("片段 %s 引用已移除受访者 %s", segment.ID, name))
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: %s", ErrValidation, strings.Join(missing, "；"))
	}
	c.ConsentRevision++
	c.Consents = normalized
	c.ConsentHistory = append(c.ConsentHistory, ConsentRevision{Revision: c.ConsentRevision, Reason: reason, FrozenAt: now.UTC(), Consents: cloneConsents(normalized)})
	c.LastScanRevision, c.LastScanConsent, c.NeedsFullScan = 0, 0, true
	c.Status = StatusDraft
	c.touch("consents.revised", now, map[string]any{"revision": c.ConsentRevision, "reason": reason, "differenceCount": len(diffs)})
	return nil
}

func cloneConsents(in []ParticipantConsent) []ParticipantConsent {
	out := append([]ParticipantConsent(nil), in...)
	for i := range out {
		out[i].RestrictedTopics = append([]string(nil), in[i].RestrictedTopics...)
	}
	return out
}

func (c *ReleaseCase) PreviewSegmentBatch(inputs []SegmentInput) SegmentPreview {
	preview := SegmentPreview{}
	known := map[string]bool{}
	for _, consent := range c.Consents {
		known[consent.ParticipantName] = true
	}
	for index, raw := range inputs {
		row := raw.Row
		if row <= 0 {
			row = index + 1
		}
		raw.Row, raw.Summary, raw.LocationTag = row, strings.TrimSpace(raw.Summary), strings.TrimSpace(raw.LocationTag)
		raw.MentionedParticipants, raw.TopicTags = normalizeTerms(raw.MentionedParticipants), normalizeTerms(raw.TopicTags)
		preview.Rows = append(preview.Rows, raw)
		add := func(field, code, message string) {
			preview.Problems = append(preview.Problems, SegmentProblem{Row: row, Field: field, Code: code, Message: message})
		}
		if raw.StartMillis < 0 || raw.EndMillis <= raw.StartMillis {
			add("timeRange", "invalid_time_range", "结束时间必须大于非负开始时间")
		}
		if raw.Summary == "" {
			add("summary", "summary_required", "文字摘要不能为空")
		}
		for _, name := range raw.MentionedParticipants {
			if !known[name] {
				add("mentionedParticipants", "unknown_participant", "未登记受访者："+name)
			}
		}
		if raw.LocationTag != "" {
			prefix := strings.ToLower(strings.SplitN(raw.LocationTag, ":", 2)[0])
			if !knownPrecision(prefix) {
				add("locationTag", "unsupported_location_prefix", "地点标签前缀仅支持 exact、city、province 或 none")
			}
		}
	}
	sort.SliceStable(preview.Rows, func(i, j int) bool {
		if preview.Rows[i].StartMillis != preview.Rows[j].StartMillis {
			return preview.Rows[i].StartMillis < preview.Rows[j].StartMillis
		}
		return preview.Rows[i].Row < preview.Rows[j].Row
	})
	for i := range preview.Rows {
		sequence := 1
		for _, segment := range c.Segments {
			if segment.StartMillis < preview.Rows[i].StartMillis {
				sequence++
			}
		}
		for j := range preview.Rows {
			if preview.Rows[j].StartMillis < preview.Rows[i].StartMillis {
				sequence++
			}
		}
		preview.Rows[i].Sequence = sequence
	}
	for i, row := range preview.Rows {
		for _, existing := range c.Segments {
			if row.StartMillis < existing.EndMillis && row.EndMillis > existing.StartMillis {
				preview.Problems = append(preview.Problems, SegmentProblem{Row: row.Row, Field: "timeRange", Code: "overlap_existing", Message: "与已有片段 " + existing.ID + " 重叠"})
			}
		}
		for j := 0; j < i; j++ {
			prior := preview.Rows[j]
			if row.StartMillis < prior.EndMillis && row.EndMillis > prior.StartMillis {
				preview.Problems = append(preview.Problems, SegmentProblem{Row: row.Row, Field: "timeRange", Code: "overlap_batch", Message: fmt.Sprintf("与输入行 %d 重叠", prior.Row)})
			}
		}
	}
	sort.SliceStable(preview.Problems, func(i, j int) bool {
		if preview.Problems[i].Row != preview.Problems[j].Row {
			return preview.Problems[i].Row < preview.Problems[j].Row
		}
		if preview.Problems[i].Field != preview.Problems[j].Field {
			return preview.Problems[i].Field < preview.Problems[j].Field
		}
		return preview.Problems[i].Code < preview.Problems[j].Code
	})
	preview.Valid = len(preview.Rows) > 0 && len(preview.Problems) == 0
	return preview
}

func (c *ReleaseCase) ImportSegmentBatch(inputs []SegmentInput, now time.Time) (SegmentPreview, error) {
	if !editableStatus(c.Status) {
		return SegmentPreview{}, fmt.Errorf("%w: 当前状态不可导入片段", ErrInvalidState)
	}
	if len(c.Consents) == 0 {
		return SegmentPreview{}, fmt.Errorf("%w: 请先冻结授权边界", ErrInvalidState)
	}
	preview := c.PreviewSegmentBatch(inputs)
	if !preview.Valid {
		return preview, fmt.Errorf("%w: 批量片段存在 %d 项问题", ErrValidation, len(preview.Problems))
	}
	c.ContentRevision++
	for index, row := range preview.Rows {
		c.Segments = append(c.Segments, RecordingSegment{ID: fmt.Sprintf("segment-%d-%d", c.ContentRevision, index+1), CaseID: c.ID, StartMillis: row.StartMillis, EndMillis: row.EndMillis, Summary: row.Summary, MentionedParticipants: row.MentionedParticipants, TopicTags: row.TopicTags, LocationTag: row.LocationTag, ContentRevision: c.ContentRevision})
	}
	sort.Slice(c.Segments, func(i, j int) bool { return c.Segments[i].StartMillis < c.Segments[j].StartMillis })
	for i := range c.Segments {
		c.Segments[i].Sequence = i + 1
	}
	c.LastScanRevision, c.LastScanConsent, c.NeedsFullScan = 0, 0, true
	if c.Status == StatusRemediation || c.Status == StatusReady {
		c.Status = StatusDraft
	}
	c.touch("segments.batch_imported", now, map[string]any{"count": len(preview.Rows), "contentRevision": c.ContentRevision})
	return preview, nil
}

func (c *ReleaseCase) SaveReviewProgress(findingIDs []string, notes map[string]string, now time.Time) error {
	if c.Status != StatusReview || c.Review == nil {
		return fmt.Errorf("%w: 当前没有进行中的伦理复核", ErrInvalidState)
	}
	if c.Review.ScanRevision != c.LastScanRevision || c.Review.ConsentRevision != c.ConsentRevision {
		return ErrScanStale
	}
	required := map[string]bool{}
	for _, finding := range c.Findings {
		if finding.Status == FindingResolved {
			required[finding.ID] = true
		}
	}
	ids := normalizeTerms(findingIDs)
	for _, id := range ids {
		if !required[id] {
			return fmt.Errorf("%w: 核验项 %s 不是当前已关闭冲突", ErrValidation, id)
		}
	}
	cleanNotes := cloneStringMap(c.Review.VerificationNotes)
	if cleanNotes == nil {
		cleanNotes = map[string]string{}
	}
	for id, note := range notes {
		if !required[id] {
			return fmt.Errorf("%w: 核验备注引用未知冲突 %s", ErrValidation, id)
		}
		cleanNotes[id] = strings.TrimSpace(note)
	}
	c.Review.VerifiedFindingIDs, c.Review.VerificationNotes, c.Review.LastProgressSavedAt = ids, cleanNotes, now.UTC()
	c.touch("review.progress_saved", now, map[string]any{"round": c.Review.Round, "verifiedCount": len(ids)})
	return nil
}

func (c *ReleaseCase) AddSupplementalConsent(findingID, participant, evidence, authorizedDate, rule string, segmentIDs []string, now time.Time) error {
	participant, evidence, rule = strings.TrimSpace(participant), strings.TrimSpace(evidence), strings.TrimSpace(rule)
	if participant == "" || evidence == "" || authorizedDate == "" || rule == "" || len(segmentIDs) == 0 {
		return fmt.Errorf("%w: 补充授权的受访者、证据、日期、规则和片段均为必填", ErrValidation)
	}
	date, err := time.Parse("2006-01-02", authorizedDate)
	if err != nil {
		return fmt.Errorf("%w: 授权日期格式应为 YYYY-MM-DD", ErrValidation)
	}
	today, _ := time.Parse("2006-01-02", now.Format("2006-01-02"))
	if date.After(today) {
		return fmt.Errorf("%w: 授权日期不能晚于今天", ErrValidation)
	}
	knownParticipant := false
	for _, consent := range c.Consents {
		if consent.ParticipantName == participant {
			knownParticipant = true
		}
	}
	if !knownParticipant {
		return fmt.Errorf("%w: 未知受访者 %s", ErrValidation, participant)
	}
	var finding *ConflictFinding
	for i := range c.Findings {
		if c.Findings[i].ID == findingID {
			finding = &c.Findings[i]
			break
		}
	}
	if finding == nil {
		return fmt.Errorf("%w: 冲突不存在", ErrNotFound)
	}
	if finding.ParticipantName != participant || finding.RuleCode != rule {
		return fmt.Errorf("%w: 补充授权必须与原冲突的受访者和规则精确匹配", ErrValidation)
	}
	segments := normalizeTerms(segmentIDs)
	covered := false
	for _, id := range segments {
		if id == finding.SegmentID {
			covered = true
		}
		exists := false
		for _, seg := range c.Segments {
			if seg.ID == id {
				exists = true
			}
		}
		if !exists {
			return fmt.Errorf("%w: 适用片段 %s 不存在", ErrValidation, id)
		}
	}
	if !covered {
		return fmt.Errorf("%w: 适用片段未覆盖原冲突片段", ErrValidation)
	}
	id := fmt.Sprintf("supplemental-%d", len(c.SupplementalConsents)+1)
	c.SupplementalConsents = append(c.SupplementalConsents, SupplementalConsentEvidence{ID: id, FindingID: findingID, ParticipantName: participant, EvidenceDigest: evidence, AuthorizedDate: authorizedDate, RuleCode: rule, SegmentIDs: segments, RecordedAt: now.UTC()})
	return nil
}
