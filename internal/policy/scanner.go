package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"oral-history-clearance/internal/domain"
)

type Scanner struct{}

func NewScanner() *Scanner { return &Scanner{} }

func (s *Scanner) ScanAll(c *domain.ReleaseCase) []domain.FindingSeed {
	ids := make(map[string]bool, len(c.Segments))
	for _, seg := range c.Segments {
		ids[seg.ID] = true
	}
	return s.ScanSegments(c, ids)
}

func (s *Scanner) ScanSegments(c *domain.ReleaseCase, ids map[string]bool) []domain.FindingSeed {
	consents := append([]domain.ParticipantConsent(nil), c.Consents...)
	sort.Slice(consents, func(i, j int) bool { return consents[i].ParticipantName < consents[j].ParticipantName })
	segments := append([]domain.RecordingSegment(nil), c.Segments...)
	sort.Slice(segments, func(i, j int) bool { return segments[i].Sequence < segments[j].Sequence })
	var out []domain.FindingSeed
	for _, seg := range segments {
		if !ids[seg.ID] || seg.Deleted {
			continue
		}
		for _, consent := range consents {
			if !contains(seg.MentionedParticipants, consent.ParticipantName) {
				continue
			}
			if !consent.IdentityDisclosure && !seg.Pseudonymized && !seg.Muted && !wasSupplemented(c, seg.ID, consent.ParticipantName, "IDENTITY") {
				out = append(out, seed(seg, consent, "IDENTITY", "受访者未授权公开身份", "identityDisclosure", "false", "mentionedParticipants", consent.ParticipantName))
			}
			if !seg.Muted {
				for _, topic := range intersection(seg.TopicTags, consent.RestrictedTopics) {
					if !wasSupplemented(c, seg.ID, consent.ParticipantName, "TOPIC:"+topic) {
						out = append(out, seed(seg, consent, "TOPIC:"+topic, "片段命中限制话题“"+topic+"”", "restrictedTopics", topic, "topicTags", topic))
					}
				}
			}
			if seg.LocationTag != "" && locationTooPrecise(seg.LocationTag, consent.LocationPrecision) && !seg.LocationGeneralized && !seg.Muted && !wasSupplemented(c, seg.ID, consent.ParticipantName, "LOCATION") {
				out = append(out, seed(seg, consent, "LOCATION", "地点标签精度超过授权边界 "+consent.LocationPrecision, "locationPrecision", consent.LocationPrecision, "locationTag", seg.LocationTag))
			}
			if consent.EmbargoUntil != "" && !seg.Muted && !wasSupplemented(c, seg.ID, consent.ParticipantName, "EMBARGO") {
				target, e1 := time.Parse("2006-01-02", c.TargetPublishDate)
				embargo, e2 := time.Parse("2006-01-02", consent.EmbargoUntil)
				if e1 == nil && e2 == nil && target.Before(embargo) {
					out = append(out, seed(seg, consent, "EMBARGO", "目标公开日期早于禁用截止日期 "+consent.EmbargoUntil, "embargoUntil", consent.EmbargoUntil, "targetPublishDate", c.TargetPublishDate))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SegmentID != out[j].SegmentID {
			return out[i].SegmentID < out[j].SegmentID
		}
		if out[i].RuleCode != out[j].RuleCode {
			return out[i].RuleCode < out[j].RuleCode
		}
		return out[i].Reason < out[j].Reason
	})
	for i := range out {
		sum := sha256.Sum256([]byte(out[i].SegmentID + "|" + out[i].RuleCode + "|" + out[i].Reason))
		out[i].ID = fmt.Sprintf("finding-%s-r%d-%s", out[i].SegmentID, c.ContentRevision, hex.EncodeToString(sum[:4]))
	}
	return out
}

func seed(segment domain.RecordingSegment, consent domain.ParticipantConsent, code, reason, consentField, consentValue, segmentField, segmentValue string) domain.FindingSeed {
	return domain.FindingSeed{SegmentID: segment.ID, ParticipantName: consent.ParticipantName, RuleCode: code, Reason: consent.ParticipantName + "：" + reason, Basis: domain.FindingBasis{ConsentField: consentField, ConsentValue: consentValue, SegmentField: segmentField, SegmentValue: segmentValue, ComparedValue: consentValue + " ↔ " + segmentValue, RuleCode: code}}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersection(left, right []string) []string {
	set := map[string]bool{}
	for _, value := range right {
		set[value] = true
	}
	var out []string
	for _, value := range left {
		if set[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func locationTooPrecise(tag, allowed string) bool {
	rank := map[string]int{"none": 0, "province": 1, "city": 2, "exact": 3}
	actual := 3
	lower := strings.ToLower(tag)
	for key, value := range rank {
		if strings.HasPrefix(lower, key+":") || lower == key {
			actual = value
			break
		}
	}
	return actual > rank[allowed]
}

func wasSupplemented(c *domain.ReleaseCase, segmentID, participant, rule string) bool {
	for _, evidence := range c.SupplementalConsents {
		if evidence.ParticipantName != participant || evidence.RuleCode != rule {
			continue
		}
		for _, id := range evidence.SegmentIDs {
			if id == segmentID {
				return true
			}
		}
	}
	for _, seg := range c.Segments {
		if seg.ID == segmentID && contains(seg.SupplementalRules, rule) {
			return true
		}
	}
	for _, finding := range c.Findings {
		if finding.SegmentID == segmentID && finding.RemediationType == domain.RemediationSupplemental && finding.Status == domain.FindingResolved {
			if finding.RuleCode == rule || (rule == "LOCATION" && finding.RuleCode == "LOCATION") || (rule == "IDENTITY" && finding.RuleCode == "IDENTITY") || (rule == "EMBARGO" && finding.RuleCode == "EMBARGO") {
				return true
			}
		}
	}
	return false
}

func AffectedSegments(c *domain.ReleaseCase, findingID string) map[string]bool {
	out := map[string]bool{}
	for _, finding := range c.Findings {
		if finding.ID == findingID {
			out[finding.SegmentID] = true
		}
	}
	return out
}

func PlanRevisionCurrent(c *domain.ReleaseCase) error {
	if !c.ScanCurrent() {
		return domain.ErrScanStale
	}
	return nil
}
