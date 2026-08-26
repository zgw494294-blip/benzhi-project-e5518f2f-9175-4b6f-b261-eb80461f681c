package web

import "oral-history-clearance/internal/domain"

type mutationMeta struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

type freezeRequest struct {
	ExpectedVersion int64                       `json:"expectedVersion"`
	Consents        []domain.ParticipantConsent `json:"consents"`
}

type segmentRequest struct {
	ExpectedVersion       int64    `json:"expectedVersion"`
	StartMillis           int64    `json:"startMillis"`
	EndMillis             int64    `json:"endMillis"`
	Summary               string   `json:"summary"`
	MentionedParticipants []string `json:"mentionedParticipants"`
	TopicTags             []string `json:"topicTags"`
	LocationTag           string   `json:"locationTag"`
}

type remediationRequest struct {
	ExpectedVersion int64                  `json:"expectedVersion"`
	FindingID       string                 `json:"findingId"`
	RemediationType domain.RemediationType `json:"remediationType"`
	BeforeNote      string                 `json:"beforeNote"`
	AfterNote       string                 `json:"afterNote"`
	ParticipantName string                 `json:"participantName,omitempty"`
	EvidenceDigest  string                 `json:"evidenceDigest,omitempty"`
	AuthorizedDate  string                 `json:"authorizedDate,omitempty"`
	RuleCode        string                 `json:"ruleCode,omitempty"`
	SegmentIDs      []string               `json:"segmentIds,omitempty"`
}

type reviewSubmitRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	ReviewerName    string `json:"reviewerName"`
}

type reviewReturnRequest struct {
	ExpectedVersion int64    `json:"expectedVersion"`
	FindingIDs      []string `json:"findingIds"`
	Note            string   `json:"note"`
}

type reviewApproveRequest struct {
	ExpectedVersion    int64    `json:"expectedVersion"`
	VerifiedFindingIDs []string `json:"verifiedFindingIds"`
}

type profileRequest struct {
	ExpectedVersion   int64  `json:"expectedVersion"`
	Title             string `json:"title"`
	EditorName        string `json:"editorName"`
	TargetPublishDate string `json:"targetPublishDate"`
}

type consentPreviewRequest struct {
	Consents []domain.ParticipantConsent `json:"consents"`
}

type consentRevisionRequest struct {
	ExpectedVersion     int64                       `json:"expectedVersion"`
	BaseConsentRevision int64                       `json:"baseConsentRevision"`
	Reason              string                      `json:"reason"`
	Consents            []domain.ParticipantConsent `json:"consents"`
}

type segmentBatchRequest struct {
	ExpectedVersion int64                 `json:"expectedVersion,omitempty"`
	Segments        []domain.SegmentInput `json:"segments"`
}

type batchRemediationRequest struct {
	ExpectedVersion int64                  `json:"expectedVersion"`
	FindingIDs      []string               `json:"findingIds"`
	RemediationType domain.RemediationType `json:"remediationType"`
	BeforeNote      string                 `json:"beforeNote"`
	AfterNote       string                 `json:"afterNote"`
}

type reviewProgressRequest struct {
	ExpectedVersion    int64             `json:"expectedVersion"`
	VerifiedFindingIDs []string          `json:"verifiedFindingIds"`
	Notes              map[string]string `json:"notes"`
}
