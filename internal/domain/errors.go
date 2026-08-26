package domain

import (
	"errors"
	"fmt"
	"strings"
)

type CaseStatus string

const (
	StatusDraft       CaseStatus = "draft"
	StatusRemediation CaseStatus = "remediation"
	StatusReady       CaseStatus = "ready_for_review"
	StatusReview      CaseStatus = "in_review"
	StatusReleased    CaseStatus = "released"
)

type FindingStatus string

const (
	FindingOpen     FindingStatus = "open"
	FindingResolved FindingStatus = "resolved"
)

type RemediationType string

const (
	RemediationMute         RemediationType = "mute"
	RemediationPseudonym    RemediationType = "pseudonym"
	RemediationGeneralize   RemediationType = "generalize_location"
	RemediationDelete       RemediationType = "delete_segment"
	RemediationSupplemental RemediationType = "supplemental_consent"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrVersionConflict     = errors.New("version conflict")
	ErrInvalidState        = errors.New("invalid state")
	ErrValidation          = errors.New("validation failed")
	ErrScanStale           = errors.New("scan is stale")
	ErrOpenFindings        = errors.New("open findings remain")
	ErrCredentialExists    = errors.New("credential already exists")
	ErrArchiveCodeConflict = errors.New("archive code conflict")
	ErrCredentialMissing   = errors.New("credential missing")
	ErrCursorStale         = errors.New("cursor stale")
)

type ArchiveCodeConflictError struct {
	ArchiveCode    string
	ExistingCaseID string
}

func (e *ArchiveCodeConflictError) Error() string {
	return fmt.Sprintf("%v: 馆藏编号 %s 已由发布案 %s 使用", ErrArchiveCodeConflict, e.ArchiveCode, e.ExistingCaseID)
}

func (e *ArchiveCodeConflictError) Unwrap() error { return ErrArchiveCodeConflict }

type ReviewIncompleteError struct{ FindingIDs []string }

func (e *ReviewIncompleteError) Error() string {
	return fmt.Sprintf("%v: 尚有未核验冲突 %s", ErrValidation, strings.Join(e.FindingIDs, "、"))
}

func (e *ReviewIncompleteError) Unwrap() error { return ErrValidation }
