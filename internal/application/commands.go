package application

import "oral-history-clearance/internal/domain"

type CreateCaseCommand struct {
	ArchiveCode       string `json:"archiveCode"`
	Title             string `json:"title"`
	EditorName        string `json:"editorName"`
	TargetPublishDate string `json:"targetPublishDate"`
}

type FreezeConsentsCommand struct {
	Consents []domain.ParticipantConsent `json:"consents"`
}

type AddSegmentCommand struct {
	StartMillis           int64    `json:"startMillis"`
	EndMillis             int64    `json:"endMillis"`
	Summary               string   `json:"summary"`
	MentionedParticipants []string `json:"mentionedParticipants"`
	TopicTags             []string `json:"topicTags"`
	LocationTag           string   `json:"locationTag"`
}

type RemediateCommand struct {
	FindingID       string                 `json:"findingId"`
	RemediationType domain.RemediationType `json:"remediationType"`
	BeforeNote      string                 `json:"beforeNote"`
	AfterNote       string                 `json:"afterNote"`
}

type SubmitReviewCommand struct {
	ReviewerName string `json:"reviewerName"`
}

type ReturnReviewCommand struct {
	FindingIDs []string `json:"findingIds"`
	Note       string   `json:"note"`
}

type ApproveCommand struct {
	VerifiedFindingIDs []string `json:"verifiedFindingIds"`
}

type ReviseProfileCommand struct {
	Title             string `json:"title"`
	EditorName        string `json:"editorName"`
	TargetPublishDate string `json:"targetPublishDate"`
}

type ReviseConsentsCommand struct {
	Consents            []domain.ParticipantConsent `json:"consents"`
	Reason              string                      `json:"reason"`
	BaseConsentRevision int64                       `json:"baseConsentRevision"`
}

type ConsentPreview struct {
	BaseConsentRevision int64                      `json:"baseConsentRevision"`
	Differences         []domain.ConsentDifference `json:"differences"`
	Problems            []ConsentProblem           `json:"problems,omitempty"`
	Valid               bool                       `json:"valid"`
}

type ConsentProblem struct {
	ParticipantName string `json:"participantName,omitempty"`
	Field           string `json:"field"`
	Code            string `json:"code"`
	Message         string `json:"message"`
	SegmentID       string `json:"segmentId,omitempty"`
}

type ImportSegmentsCommand struct {
	Segments []domain.SegmentInput `json:"segments"`
}

type BatchRemediateCommand struct {
	FindingIDs      []string               `json:"findingIds"`
	RemediationType domain.RemediationType `json:"remediationType"`
	BeforeNote      string                 `json:"beforeNote"`
	AfterNote       string                 `json:"afterNote"`
}

type SupplementalConsentCommand struct {
	FindingID       string   `json:"findingId"`
	ParticipantName string   `json:"participantName"`
	EvidenceDigest  string   `json:"evidenceDigest"`
	AuthorizedDate  string   `json:"authorizedDate"`
	RuleCode        string   `json:"ruleCode"`
	SegmentIDs      []string `json:"segmentIds"`
	BeforeNote      string   `json:"beforeNote"`
	AfterNote       string   `json:"afterNote"`
}

type SaveReviewProgressCommand struct {
	VerifiedFindingIDs []string          `json:"verifiedFindingIds"`
	Notes              map[string]string `json:"notes"`
}
