package journal

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"oral-history-clearance/internal/domain"
)

const schemaVersion = 1

var (
	ErrCorruptJournal      = errors.New("journal integrity failure")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
)

type IdempotencyEntry struct {
	Key         string          `json:"key"`
	Fingerprint string          `json:"fingerprint"`
	Response    json.RawMessage `json:"response"`
}

type record struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Sequence      int64                `json:"sequence"`
	PreviousHash  string               `json:"previousHash"`
	Hash          string               `json:"hash"`
	Kind          string               `json:"kind"`
	CaseID        string               `json:"caseId"`
	Case          *domain.ReleaseCase  `json:"case"`
	DomainEvents  []domain.DomainEvent `json:"domainEvents"`
	Idempotency   *IdempotencyEntry    `json:"idempotency,omitempty"`
	WrittenAt     time.Time            `json:"writtenAt"`
}

type Repository struct {
	mu           sync.RWMutex
	file         *os.File
	path         string
	sequence     int64
	lastHash     string
	cases        map[string]*domain.ReleaseCase
	idempotency  map[string]IdempotencyEntry
	archiveCodes map[string]string
}

type IntegrityReport struct {
	Valid            bool   `json:"valid"`
	RecordCount      int64  `json:"recordCount"`
	LastHash         string `json:"lastHash"`
	CaseCount        int    `json:"caseCount"`
	IdempotencyCount int    `json:"idempotencyCount"`
}
