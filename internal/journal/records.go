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
	mu              sync.RWMutex
	file            *os.File
	path            string
	sequence        int64
	lastHash        string
	cases           map[string]*domain.ReleaseCase
	idempotency     map[string]IdempotencyEntry
	archiveCodes    map[string]string
	finalizeCred    CredentialFinalizer
	marshalResponse ResponseMarshaler
}

// CredentialFinalizer 在 Commit 写锁内、记录写入前调用，用于把凭据的事件序号
// 和规范化摘要锚定到本次提交真正分配的序号上。调用方应更新 c.Credential 的
// EventSequence、CanonicalDigest 以及派生自摘要的标识符。由于并发提交可能在
// Approve 读取下一序号后、Commit 写入前占用该序号，凭据必须在原子提交边界内
// 取得自身记录的序号，确保 Credential.EventSequence 始终等于签发凭据的日志记录序号。
type CredentialFinalizer func(c *domain.ReleaseCase, sequence int64) error

// ResponseMarshaler 在 Commit 写锁内、凭据最终确定后调用，用于从最终聚合重新
// 序列化幂等响应。这样即使凭据序号在提交边界内被修正，幂等缓存中保存的响应
// 也会与写入日志记录中的聚合快照保持一致。
type ResponseMarshaler func(c *domain.ReleaseCase) (json.RawMessage, error)

// SetCredentialFinalizer 注册在原子提交边界内最终确定凭据的回调。服务层启动时
// 注入实现，使凭据的事件序号与规范化摘要锚定到 Commit 实际分配的序号上。
func (r *Repository) SetCredentialFinalizer(f CredentialFinalizer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalizeCred = f
}

// SetResponseMarshaler 注册在原子提交边界内重新序列化幂等响应的回调。它配合
// CredentialFinalizer 使用，确保幂等缓存中保存的响应与最终聚合快照一致。
func (r *Repository) SetResponseMarshaler(f ResponseMarshaler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marshalResponse = f
}

type IntegrityReport struct {
	Valid            bool   `json:"valid"`
	RecordCount      int64  `json:"recordCount"`
	LastHash         string `json:"lastHash"`
	CaseCount        int    `json:"caseCount"`
	IdempotencyCount int    `json:"idempotencyCount"`
}
