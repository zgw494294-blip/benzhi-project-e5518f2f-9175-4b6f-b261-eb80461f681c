package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type ReleaseCase struct {
	ID                   string                        `json:"id"`
	ArchiveCode          string                        `json:"archiveCode"`
	Title                string                        `json:"title"`
	EditorName           string                        `json:"editorName"`
	TargetPublishDate    string                        `json:"targetPublishDate"`
	Status               CaseStatus                    `json:"status"`
	Version              int64                         `json:"version"`
	ConsentRevision      int64                         `json:"consentRevision"`
	ContentRevision      int64                         `json:"contentRevision"`
	LastScanRevision     int64                         `json:"lastScanRevision"`
	LastScanConsent      int64                         `json:"lastScanConsentRevision"`
	NeedsFullScan        bool                          `json:"needsFullScan"`
	CreatedAt            time.Time                     `json:"createdAt"`
	UpdatedAt            time.Time                     `json:"updatedAt"`
	Consents             []ParticipantConsent          `json:"consents"`
	Segments             []RecordingSegment            `json:"segments"`
	Findings             []ConflictFinding             `json:"findings"`
	Review               *EthicsReview                 `json:"review,omitempty"`
	Credential           *ReleaseCredential            `json:"credential,omitempty"`
	ConsentHistory       []ConsentRevision             `json:"consentHistory,omitempty"`
	SupplementalConsents []SupplementalConsentEvidence `json:"supplementalConsents,omitempty"`
	ReviewHistory        []ReviewProgress              `json:"reviewHistory,omitempty"`
	Events               []DomainEvent                 `json:"events,omitempty"`
}

type ParticipantConsent struct {
	ID                 string    `json:"id"`
	CaseID             string    `json:"caseId"`
	ParticipantName    string    `json:"participantName"`
	IdentityDisclosure bool      `json:"identityDisclosure"`
	RestrictedTopics   []string  `json:"restrictedTopics"`
	LocationPrecision  string    `json:"locationPrecision"`
	EmbargoUntil       string    `json:"embargoUntil,omitempty"`
	EvidenceDigest     string    `json:"evidenceDigest"`
	Revision           int64     `json:"revision"`
	FrozenAt           time.Time `json:"frozenAt"`
}

type ConsentRevision struct {
	Revision int64                `json:"revision"`
	Reason   string               `json:"reason"`
	FrozenAt time.Time            `json:"frozenAt"`
	Consents []ParticipantConsent `json:"consents"`
}

type RecordingSegment struct {
	ID                    string   `json:"id"`
	CaseID                string   `json:"caseId"`
	Sequence              int      `json:"sequence"`
	StartMillis           int64    `json:"startMillis"`
	EndMillis             int64    `json:"endMillis"`
	Summary               string   `json:"summary"`
	MentionedParticipants []string `json:"mentionedParticipants"`
	TopicTags             []string `json:"topicTags"`
	LocationTag           string   `json:"locationTag,omitempty"`
	ContentRevision       int64    `json:"contentRevision"`
	Deleted               bool     `json:"deleted"`
	Muted                 bool     `json:"muted"`
	Pseudonymized         bool     `json:"pseudonymized"`
	LocationGeneralized   bool     `json:"locationGeneralized"`
	SupplementalRules     []string `json:"supplementalRules,omitempty"`
}

type ConflictFinding struct {
	ID              string          `json:"id"`
	CaseID          string          `json:"caseId"`
	SegmentID       string          `json:"segmentId"`
	RuleCode        string          `json:"ruleCode"`
	Reason          string          `json:"reason"`
	ScanRevision    int64           `json:"scanRevision"`
	ConsentRevision int64           `json:"consentRevision"`
	ContentRevision int64           `json:"contentRevision"`
	ParticipantName string          `json:"participantName"`
	Basis           FindingBasis    `json:"basis"`
	Status          FindingStatus   `json:"status"`
	RemediationType RemediationType `json:"remediationType,omitempty"`
	BeforeNote      string          `json:"beforeNote,omitempty"`
	AfterNote       string          `json:"afterNote,omitempty"`
	ResolvedAt      *time.Time      `json:"resolvedAt,omitempty"`
}

type FindingBasis struct {
	ConsentField  string `json:"consentField"`
	ConsentValue  string `json:"consentValue"`
	SegmentField  string `json:"segmentField"`
	SegmentValue  string `json:"segmentValue"`
	ComparedValue string `json:"comparedValue"`
	RuleCode      string `json:"ruleCode"`
}

type SupplementalConsentEvidence struct {
	ID              string    `json:"id"`
	FindingID       string    `json:"findingId"`
	ParticipantName string    `json:"participantName"`
	EvidenceDigest  string    `json:"evidenceDigest"`
	AuthorizedDate  string    `json:"authorizedDate"`
	RuleCode        string    `json:"ruleCode"`
	SegmentIDs      []string  `json:"segmentIds"`
	RecordedAt      time.Time `json:"recordedAt"`
}

type EthicsReview struct {
	ReviewerName        string            `json:"reviewerName"`
	SubmittedAt         time.Time         `json:"submittedAt"`
	Decision            string            `json:"decision"`
	Note                string            `json:"note,omitempty"`
	FindingIDs          []string          `json:"findingIds,omitempty"`
	VerifiedFindingIDs  []string          `json:"verifiedFindingIds,omitempty"`
	Round               int64             `json:"round"`
	ScanRevision        int64             `json:"scanRevision"`
	ConsentRevision     int64             `json:"consentRevision"`
	VerificationNotes   map[string]string `json:"verificationNotes,omitempty"`
	LastProgressSavedAt time.Time         `json:"lastProgressSavedAt,omitempty"`
	DecidedAt           time.Time         `json:"decidedAt,omitempty"`
}

type ReviewProgress struct {
	Round              int64             `json:"round"`
	ScanRevision       int64             `json:"scanRevision"`
	ConsentRevision    int64             `json:"consentRevision"`
	VerifiedFindingIDs []string          `json:"verifiedFindingIds"`
	Notes              map[string]string `json:"notes,omitempty"`
	SavedAt            time.Time         `json:"savedAt"`
	ClosedAt           time.Time         `json:"closedAt,omitempty"`
}

type ReleaseCredential struct {
	ID                 string    `json:"id"`
	CaseID             string    `json:"caseId"`
	CaseVersion        int64     `json:"caseVersion"`
	ConsentRevision    int64     `json:"consentRevision"`
	IncludedSegmentIDs []string  `json:"includedSegmentIds"`
	ReviewerName       string    `json:"reviewerName"`
	ApprovedAt         time.Time `json:"approvedAt"`
	EventSequence      int64     `json:"eventSequence"`
	CanonicalDigest    string    `json:"canonicalDigest"`
}

type DomainEvent struct {
	Type       string         `json:"type"`
	Version    int64          `json:"version"`
	OccurredAt time.Time      `json:"occurredAt"`
	Data       map[string]any `json:"data,omitempty"`
}

type FindingSeed struct {
	ID              string
	SegmentID       string
	RuleCode        string
	Reason          string
	ParticipantName string
	Basis           FindingBasis
}

func NormalizeArchiveCode(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }

func NewReleaseCase(id, archiveCode, title, editor, target string, now time.Time) (*ReleaseCase, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(archiveCode) == "" || strings.TrimSpace(title) == "" || strings.TrimSpace(editor) == "" {
		return nil, fmt.Errorf("%w: 发布案编号、馆藏编号、标题和整理员均为必填", ErrValidation)
	}
	if _, err := time.Parse("2006-01-02", target); err != nil {
		return nil, fmt.Errorf("%w: 目标公开日期格式应为 YYYY-MM-DD", ErrValidation)
	}
	c := &ReleaseCase{ID: id, ArchiveCode: NormalizeArchiveCode(archiveCode), Title: strings.TrimSpace(title), EditorName: strings.TrimSpace(editor), TargetPublishDate: target, Status: StatusDraft, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	c.touch("case.created", now, map[string]any{"archiveCode": c.ArchiveCode})
	return c, nil
}

func (c *ReleaseCase) Clone() *ReleaseCase {
	if c == nil {
		return nil
	}
	n := *c
	n.Consents = append([]ParticipantConsent(nil), c.Consents...)
	for i := range n.Consents {
		n.Consents[i].RestrictedTopics = append([]string(nil), c.Consents[i].RestrictedTopics...)
	}
	n.Segments = append([]RecordingSegment(nil), c.Segments...)
	for i := range n.Segments {
		n.Segments[i].MentionedParticipants = append([]string(nil), c.Segments[i].MentionedParticipants...)
		n.Segments[i].TopicTags = append([]string(nil), c.Segments[i].TopicTags...)
		n.Segments[i].SupplementalRules = append([]string(nil), c.Segments[i].SupplementalRules...)
	}
	n.Findings = append([]ConflictFinding(nil), c.Findings...)
	n.ConsentHistory = cloneConsentHistory(c.ConsentHistory)
	n.SupplementalConsents = append([]SupplementalConsentEvidence(nil), c.SupplementalConsents...)
	for i := range n.SupplementalConsents {
		n.SupplementalConsents[i].SegmentIDs = append([]string(nil), c.SupplementalConsents[i].SegmentIDs...)
	}
	n.ReviewHistory = append([]ReviewProgress(nil), c.ReviewHistory...)
	for i := range n.ReviewHistory {
		n.ReviewHistory[i].VerifiedFindingIDs = append([]string(nil), c.ReviewHistory[i].VerifiedFindingIDs...)
		n.ReviewHistory[i].Notes = cloneStringMap(c.ReviewHistory[i].Notes)
	}
	if c.Review != nil {
		r := *c.Review
		r.FindingIDs = append([]string(nil), c.Review.FindingIDs...)
		r.VerifiedFindingIDs = append([]string(nil), c.Review.VerifiedFindingIDs...)
		r.VerificationNotes = cloneStringMap(c.Review.VerificationNotes)
		n.Review = &r
	}
	if c.Credential != nil {
		cr := *c.Credential
		cr.IncludedSegmentIDs = append([]string(nil), c.Credential.IncludedSegmentIDs...)
		n.Credential = &cr
	}
	n.Events = append([]DomainEvent(nil), c.Events...)
	return &n
}

func cloneConsentHistory(in []ConsentRevision) []ConsentRevision {
	out := append([]ConsentRevision(nil), in...)
	for i := range out {
		out[i].Consents = append([]ParticipantConsent(nil), in[i].Consents...)
		for j := range out[i].Consents {
			out[i].Consents[j].RestrictedTopics = append([]string(nil), in[i].Consents[j].RestrictedTopics...)
		}
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (c *ReleaseCase) touch(kind string, now time.Time, data map[string]any) {
	c.Version++
	c.UpdatedAt = now.UTC()
	c.Events = append(c.Events, DomainEvent{Type: kind, Version: c.Version, OccurredAt: now.UTC(), Data: data})
}

func normalizeTerms(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		v := strings.TrimSpace(value)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func (c *ReleaseCase) OpenFindingCount() int {
	n := 0
	for _, f := range c.Findings {
		if f.Status == FindingOpen {
			n++
		}
	}
	return n
}

func (c *ReleaseCase) ScanCurrent() bool {
	return !c.NeedsFullScan && c.LastScanRevision == c.ContentRevision && c.LastScanConsent == c.ConsentRevision && c.LastScanRevision > 0
}

func (c *ReleaseCase) IncludedSegments() []RecordingSegment {
	out := make([]RecordingSegment, 0, len(c.Segments))
	for _, s := range c.Segments {
		if !s.Deleted {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}
