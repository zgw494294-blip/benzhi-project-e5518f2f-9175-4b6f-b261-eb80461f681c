package journal

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"oral-history-clearance/internal/domain"
)

func Open(path string) (*Repository, error) {
	if path == "" {
		return nil, fmt.Errorf("日志路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("创建日志目录: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开事件日志: %w", err)
	}
	r := &Repository{file: f, path: path, cases: map[string]*domain.ReleaseCase{}, idempotency: map[string]IdempotencyEntry{}, archiveCodes: map[string]string{}}
	if err := r.restore(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return r, nil
}

func (r *Repository) restore() error {
	if _, err := r.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(r.file)
	var prior string
	var sequence int64
	lineNo := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			if line[len(line)-1] != '\n' {
				return fmt.Errorf("%w: 第 %d 行为不完整尾部记录", ErrCorruptJournal, lineNo)
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				return fmt.Errorf("%w: 第 %d 行为空记录", ErrCorruptJournal, lineNo)
			}
			var rec record
			if decodeErr := json.Unmarshal(line, &rec); decodeErr != nil {
				return fmt.Errorf("%w: 第 %d 行 JSON 无效: %v", ErrCorruptJournal, lineNo, decodeErr)
			}
			if rec.SchemaVersion != schemaVersion {
				return fmt.Errorf("%w: 第 %d 行 schemaVersion=%d 不受支持", ErrCorruptJournal, lineNo, rec.SchemaVersion)
			}
			if rec.Sequence != sequence+1 || rec.PreviousHash != prior {
				return fmt.Errorf("%w: 第 %d 行序号或前序散列不连续", ErrCorruptJournal, lineNo)
			}
			expected, hashErr := hashRecord(rec)
			if hashErr != nil || rec.Hash != expected {
				return fmt.Errorf("%w: 第 %d 行散列不匹配", ErrCorruptJournal, lineNo)
			}
			if rec.Case == nil || rec.Case.ID != rec.CaseID {
				return fmt.Errorf("%w: 第 %d 行聚合快照缺失", ErrCorruptJournal, lineNo)
			}
			migrateSnapshot(rec.Case)
			if err := rec.Case.ValidateInvariants(); err != nil {
				return fmt.Errorf("%w: 第 %d 行聚合无效: %v", ErrCorruptJournal, lineNo, err)
			}
			if old := r.cases[rec.CaseID]; old != nil && rec.Case.Version <= old.Version {
				return fmt.Errorf("%w: 第 %d 行聚合版本未递增", ErrCorruptJournal, lineNo)
			}
			baseVersion := int64(0)
			if old := r.cases[rec.CaseID]; old != nil {
				baseVersion = old.Version
			}
			if err := validateEventBatch(rec.DomainEvents, baseVersion, rec.Case.Version); err != nil {
				return fmt.Errorf("%w: 第 %d 行事件批次无效: %v", ErrCorruptJournal, lineNo, err)
			}
			if rec.Idempotency != nil {
				if existing, ok := r.idempotency[rec.Idempotency.Key]; ok && existing.Fingerprint != rec.Idempotency.Fingerprint {
					return fmt.Errorf("%w: 第 %d 行幂等键重复", ErrCorruptJournal, lineNo)
				}
				r.idempotency[rec.Idempotency.Key] = *rec.Idempotency
			}
			normalizedArchive := domain.NormalizeArchiveCode(rec.Case.ArchiveCode)
			if owner, exists := r.archiveCodes[normalizedArchive]; exists && owner != rec.CaseID {
				return fmt.Errorf("%w: 第 %d 行馆藏编号 %s 与发布案 %s 重复", ErrCorruptJournal, lineNo, normalizedArchive, owner)
			}
			r.archiveCodes[normalizedArchive] = rec.CaseID
			r.cases[rec.CaseID] = rec.Case.Clone()
			sequence = rec.Sequence
			prior = rec.Hash
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取事件日志: %w", err)
		}
	}
	r.sequence = sequence
	r.lastHash = prior
	_, err := r.file.Seek(0, io.SeekEnd)
	return err
}

func migrateSnapshot(c *domain.ReleaseCase) {
	if c == nil {
		return
	}
	if c.ConsentRevision == 1 && len(c.ConsentHistory) == 0 && len(c.Consents) > 0 {
		frozenAt := c.UpdatedAt
		if !c.Consents[0].FrozenAt.IsZero() {
			frozenAt = c.Consents[0].FrozenAt
		}
		consents := append([]domain.ParticipantConsent(nil), c.Consents...)
		for i := range consents {
			consents[i].RestrictedTopics = append([]string(nil), c.Consents[i].RestrictedTopics...)
		}
		c.ConsentHistory = []domain.ConsentRevision{{Revision: 1, Reason: "历史授权边界恢复", FrozenAt: frozenAt, Consents: consents}}
	}
	if c.Review != nil && c.Review.Decision == "pending" && c.Review.Round == 0 {
		c.Review.Round, c.Review.ScanRevision, c.Review.ConsentRevision = 1, c.LastScanRevision, c.ConsentRevision
	}
	if c.Status == domain.StatusDraft && c.LastScanRevision == 0 && (len(c.Consents) > 0 || len(c.Segments) > 0) {
		c.NeedsFullScan = true
	}
}

func hashRecord(rec record) (string, error) {
	rec.Hash = ""
	b, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Repository) Get(id string) (*domain.ReleaseCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := r.cases[id]
	if c == nil {
		return nil, domain.ErrNotFound
	}
	return c.Clone(), nil
}

func (r *Repository) List() []*domain.ReleaseCase {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.ReleaseCase, 0, len(r.cases))
	for _, c := range r.cases {
		out = append(out, c.Clone())
	}
	return out
}

func (r *Repository) FindByArchiveCode(code string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.archiveCodes[domain.NormalizeArchiveCode(code)]
	return id, ok
}

func (r *Repository) LookupIdempotency(key, fingerprint string) (json.RawMessage, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.idempotency[key]
	if !ok {
		return nil, false, nil
	}
	if entry.Fingerprint != fingerprint {
		return nil, true, ErrIdempotencyConflict
	}
	return append(json.RawMessage(nil), entry.Response...), true, nil
}

func (r *Repository) Commit(c *domain.ReleaseCase, expectedVersion int64, key, fingerprint string, response json.RawMessage) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c == nil {
		return 0, fmt.Errorf("聚合不能为空")
	}
	if key == "" || fingerprint == "" || len(response) == 0 {
		return 0, fmt.Errorf("提交缺少幂等元数据")
	}
	if existing, ok := r.idempotency[key]; ok {
		if existing.Fingerprint != fingerprint {
			return 0, ErrIdempotencyConflict
		}
		return r.sequence, nil
	}
	current := r.cases[c.ID]
	normalizedArchive := domain.NormalizeArchiveCode(c.ArchiveCode)
	if current != nil && domain.NormalizeArchiveCode(current.ArchiveCode) != normalizedArchive {
		return 0, fmt.Errorf("%w: 馆藏编号创建后不可修改", domain.ErrValidation)
	}
	if owner, exists := r.archiveCodes[normalizedArchive]; exists && owner != c.ID {
		return 0, &domain.ArchiveCodeConflictError{ArchiveCode: normalizedArchive, ExistingCaseID: owner}
	}
	if current == nil {
		if expectedVersion != 0 {
			return 0, domain.ErrVersionConflict
		}
	} else if current.Version != expectedVersion {
		return 0, domain.ErrVersionConflict
	}
	if c.Version <= expectedVersion {
		return 0, fmt.Errorf("%w: 新版本必须递增", domain.ErrVersionConflict)
	}
	if err := c.ValidateInvariants(); err != nil {
		return 0, err
	}
	if current != nil && current.Credential != nil && c.Credential != nil && current.Credential.CanonicalDigest != c.Credential.CanonicalDigest {
		return 0, domain.ErrCredentialExists
	}
	events := make([]domain.DomainEvent, 0, c.Version-expectedVersion)
	for _, event := range c.Events {
		if event.Version > expectedVersion {
			events = append(events, event)
		}
	}
	if err := validateEventBatch(events, expectedVersion, c.Version); err != nil {
		return 0, err
	}
	// 在写锁内、记录写入前，把凭据的事件序号和规范化摘要锚定到本次提交真正
	// 分配的序号上。Approve 在读锁外读取的下一序号可能已被并发提交占用，因此
	// 凭据必须在此原子边界内取得自身记录的序号，确保 EventSequence 始终等于
	// 签发该凭据的日志记录序号。
	sequence := r.sequence + 1
	if c.Credential != nil && r.finalizeCred != nil {
		if err := r.finalizeCred(c, sequence); err != nil {
			return 0, err
		}
	}
	if err := c.ValidateInvariants(); err != nil {
		return 0, err
	}
	// 凭据序号或摘要可能刚刚在提交边界内被修正，幂等缓存必须保存与写入记录中
	// 聚合快照一致的响应，避免重放时把凭据锚定到错误的日志记录。
	finalResponse := response
	if c.Credential != nil && r.marshalResponse != nil {
		rebuilt, err := r.marshalResponse(c)
		if err != nil {
			return 0, err
		}
		finalResponse = rebuilt
	}
	if len(finalResponse) == 0 {
		return 0, fmt.Errorf("提交缺少幂等元数据")
	}
	rec := record{SchemaVersion: schemaVersion, Sequence: sequence, PreviousHash: r.lastHash, Kind: "aggregate.committed", CaseID: c.ID, Case: c.Clone(), DomainEvents: events, Idempotency: &IdempotencyEntry{Key: key, Fingerprint: fingerprint, Response: append(json.RawMessage(nil), finalResponse...)}, WrittenAt: time.Now().UTC()}
	hash, err := hashRecord(rec)
	if err != nil {
		return 0, err
	}
	rec.Hash = hash
	b, err := json.Marshal(rec)
	if err != nil {
		return 0, err
	}
	b = append(b, '\n')
	if _, err := r.file.Write(b); err != nil {
		return 0, fmt.Errorf("写入事件日志: %w", err)
	}
	if err := r.file.Sync(); err != nil {
		return 0, fmt.Errorf("同步事件日志: %w", err)
	}
	r.sequence = rec.Sequence
	r.lastHash = rec.Hash
	r.cases[c.ID] = c.Clone()
	r.archiveCodes[normalizedArchive] = c.ID
	r.idempotency[key] = *rec.Idempotency
	return rec.Sequence, nil
}

func (r *Repository) NextSequence() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sequence + 1
}

func (r *Repository) Sequence() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sequence
}

// VerifyIntegrity 从磁盘重新读取日志并验证链头、链尾、连续序号和每条记录散列。
// 它不依赖启动时建立的内存投影，可用于发布凭据的本地校验路径。
func (r *Repository) VerifyIntegrity() (IntegrityReport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.file == nil {
		return IntegrityReport{}, fmt.Errorf("事件日志已经关闭")
	}
	if err := r.file.Sync(); err != nil {
		return IntegrityReport{}, fmt.Errorf("校验前同步事件日志: %w", err)
	}
	b, err := os.ReadFile(r.path)
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("读取事件日志进行校验: %w", err)
	}
	if len(b) == 0 {
		if r.sequence != 0 {
			return IntegrityReport{}, fmt.Errorf("%w: 空日志与内存序号不一致", ErrCorruptJournal)
		}
		return IntegrityReport{Valid: true, CaseCount: len(r.cases), IdempotencyCount: len(r.idempotency)}, nil
	}
	if b[len(b)-1] != '\n' {
		return IntegrityReport{}, fmt.Errorf("%w: 日志尾记录不完整", ErrCorruptJournal)
	}
	lines := bytes.Split(b[:len(b)-1], []byte{'\n'})
	priorHash := ""
	var priorSequence int64
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			return IntegrityReport{}, fmt.Errorf("%w: 第 %d 行为空", ErrCorruptJournal, index+1)
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			return IntegrityReport{}, fmt.Errorf("%w: 第 %d 行 JSON 无效", ErrCorruptJournal, index+1)
		}
		if rec.SchemaVersion != schemaVersion || rec.Sequence != priorSequence+1 || rec.PreviousHash != priorHash {
			return IntegrityReport{}, fmt.Errorf("%w: 第 %d 行链路不连续", ErrCorruptJournal, index+1)
		}
		expected, err := hashRecord(rec)
		if err != nil || expected != rec.Hash {
			return IntegrityReport{}, fmt.Errorf("%w: 第 %d 行散列不匹配", ErrCorruptJournal, index+1)
		}
		priorSequence = rec.Sequence
		priorHash = rec.Hash
	}
	if priorSequence != r.sequence || priorHash != r.lastHash {
		return IntegrityReport{}, fmt.Errorf("%w: 磁盘链尾与内存投影不一致", ErrCorruptJournal)
	}
	return IntegrityReport{Valid: true, RecordCount: priorSequence, LastHash: priorHash, CaseCount: len(r.cases), IdempotencyCount: len(r.idempotency)}, nil
}

func (r *Repository) HashAtSequence(sequence int64) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if sequence < 1 || sequence > r.sequence {
		return "", fmt.Errorf("%w: 日志序号 %d 超出范围", ErrCorruptJournal, sequence)
	}
	if err := r.file.Sync(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(r.path)
	if err != nil {
		return "", err
	}
	lines := bytes.Split(bytes.TrimSuffix(b, []byte{'\n'}), []byte{'\n'})
	if int64(len(lines)) < sequence {
		return "", fmt.Errorf("%w: 日志序号 %d 缺失", ErrCorruptJournal, sequence)
	}
	var rec record
	if err := json.Unmarshal(lines[sequence-1], &rec); err != nil || rec.Sequence != sequence {
		return "", fmt.Errorf("%w: 日志锚点 %d 无效", ErrCorruptJournal, sequence)
	}
	return rec.Hash, nil
}

func validateEventBatch(events []domain.DomainEvent, expectedVersion, finalVersion int64) error {
	if len(events) == 0 {
		return fmt.Errorf("领域事件批次为空")
	}
	version := expectedVersion
	for _, event := range events {
		version++
		if event.Version != version || event.Type == "" || event.OccurredAt.IsZero() {
			return fmt.Errorf("期望事件版本 %d，实际为 %d", version, event.Version)
		}
	}
	if version != finalVersion {
		return fmt.Errorf("事件末版本 %d 与聚合版本 %d 不一致", version, finalVersion)
	}
	return nil
}

func (r *Repository) Path() string { return r.path }

func (r *Repository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
