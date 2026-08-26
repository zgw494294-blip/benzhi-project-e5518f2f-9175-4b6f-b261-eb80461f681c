package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"oral-history-clearance/internal/domain"
)

type canonicalConsent struct {
	Participant string   `json:"participant"`
	Identity    bool     `json:"identity"`
	Topics      []string `json:"topics"`
	Location    string   `json:"location"`
	Embargo     string   `json:"embargo"`
	Evidence    string   `json:"evidence"`
}

func CanonicalBytes(c *domain.ReleaseCase, reviewer string, eventSequence int64) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "schema=oral-history-clearance/v1\ncase=%s\narchive=%s\ntitle=%s\ntarget=%s\nconsentRevision=%d\neventSequence=%d\nreviewer=%s\n", c.ID, escape(c.ArchiveCode), escape(c.Title), c.TargetPublishDate, c.ConsentRevision, eventSequence, escape(reviewer))
	consents := append([]domain.ParticipantConsent(nil), c.Consents...)
	sort.Slice(consents, func(i, j int) bool { return consents[i].ParticipantName < consents[j].ParticipantName })
	for _, consent := range consents {
		topics := append([]string(nil), consent.RestrictedTopics...)
		sort.Strings(topics)
		fmt.Fprintf(&b, "consent=%s|%t|%s|%s|%s|%s\n", escape(consent.ParticipantName), consent.IdentityDisclosure, escape(strings.Join(topics, ",")), consent.LocationPrecision, consent.EmbargoUntil, escape(consent.EvidenceDigest))
	}
	for _, seg := range c.IncludedSegments() {
		people := append([]string(nil), seg.MentionedParticipants...)
		sort.Strings(people)
		topics := append([]string(nil), seg.TopicTags...)
		sort.Strings(topics)
		fmt.Fprintf(&b, "segment=%s|%d|%d|%d|%s|%s|%s|%s|%t|%t|%t\n", seg.ID, seg.Sequence, seg.StartMillis, seg.EndMillis, escape(seg.Summary), escape(strings.Join(people, ",")), escape(strings.Join(topics, ",")), escape(seg.LocationTag), seg.Muted, seg.Pseudonymized, seg.LocationGeneralized)
	}
	return []byte(b.String())
}

func Digest(c *domain.ReleaseCase, reviewer string, eventSequence int64) string {
	sum := sha256.Sum256(CanonicalBytes(c, reviewer, eventSequence))
	return hex.EncodeToString(sum[:])
}

func VerifyCredential(c *domain.ReleaseCase) (bool, string) {
	result := VerifyCredentialDetailed(c)
	if !result.Valid {
		for _, check := range result.Checks {
			if !check.Passed {
				return false, check.Message
			}
		}
		return false, "凭据校验失败"
	}
	return true, "凭据摘要、事件序号、授权快照和冻结范围校验通过"
}

func escape(value string) string {
	r := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "|", "\\|")
	return r.Replace(strings.TrimSpace(value))
}
