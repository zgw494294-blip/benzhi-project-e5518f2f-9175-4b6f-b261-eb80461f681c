package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
)

const maxRequestBytes = 1 << 20

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.Index)
	mux.HandleFunc("GET /assets/{name}", h.Asset)
	mux.HandleFunc("GET /api/cases", h.ListCases)
	mux.HandleFunc("POST /api/cases", h.CreateCase)
	mux.HandleFunc("GET /api/cases/{id}", h.GetCase)
	mux.HandleFunc("POST /api/cases/{id}/profile", h.ReviseProfile)
	mux.HandleFunc("PATCH /api/cases/{id}/profile", h.ReviseProfile)
	mux.HandleFunc("POST /api/cases/{id}/consents/freeze", h.FreezeConsents)
	mux.HandleFunc("POST /api/cases/{id}/consents/revisions/preview", h.PreviewConsentRevision)
	mux.HandleFunc("POST /api/cases/{id}/consents/revisions", h.ReviseConsents)
	mux.HandleFunc("POST /api/cases/{id}/segments", h.AddSegment)
	mux.HandleFunc("POST /api/cases/{id}/segments/batch/preview", h.PreviewSegmentBatch)
	mux.HandleFunc("POST /api/cases/{id}/segments/batch", h.ImportSegmentBatch)
	mux.HandleFunc("POST /api/cases/{id}/scan", h.ScanCase)
	mux.HandleFunc("GET /api/cases/{id}/findings", h.QueryFindings)
	mux.HandleFunc("POST /api/cases/{id}/findings/remediate", h.RemediateFinding)
	mux.HandleFunc("POST /api/cases/{id}/findings/batch-remediate", h.BatchRemediateFindings)
	mux.HandleFunc("POST /api/cases/{id}/review/submit", h.SubmitReview)
	mux.HandleFunc("POST /api/cases/{id}/review/progress", h.SaveReviewProgress)
	mux.HandleFunc("POST /api/cases/{id}/review/return", h.ReturnReview)
	mux.HandleFunc("POST /api/cases/{id}/review/approve", h.ApproveReview)
	mux.HandleFunc("GET /api/cases/{id}/credential/verify", h.VerifyCredential)
	mux.HandleFunc("GET /api/cases/{id}/credential/export", h.ExportCredential)
	return h.security(mux)
}

func (h *Handler) ReviseProfile(w http.ResponseWriter, r *http.Request) {
	var request profileRequest
	if !h.decode(w, r, &request) {
		return
	}
	detail, err := h.service.ReviseProfile(r.PathValue("id"), request.ExpectedVersion, application.ReviseProfileCommand{Title: request.Title, EditorName: request.EditorName, TargetPublishDate: request.TargetPublishDate}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) PreviewConsentRevision(w http.ResponseWriter, r *http.Request) {
	var request consentPreviewRequest
	if !h.decode(w, r, &request) {
		return
	}
	preview, err := h.service.PreviewConsentRevision(r.PathValue("id"), request.Consents)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) ReviseConsents(w http.ResponseWriter, r *http.Request) {
	var request consentRevisionRequest
	if !h.decode(w, r, &request) {
		return
	}
	command := application.ReviseConsentsCommand{Consents: request.Consents, Reason: request.Reason, BaseConsentRevision: request.BaseConsentRevision}
	detail, err := h.service.ReviseConsents(r.PathValue("id"), request.ExpectedVersion, command, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) PreviewSegmentBatch(w http.ResponseWriter, r *http.Request) {
	var request segmentBatchRequest
	if !h.decode(w, r, &request) {
		return
	}
	if len(request.Segments) == 0 || len(request.Segments) > 200 {
		h.problem(w, http.StatusUnprocessableEntity, "validation", "批量片段数量必须介于 1 和 200")
		return
	}
	preview, err := h.service.PreviewSegmentBatch(r.PathValue("id"), request.Segments)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) ImportSegmentBatch(w http.ResponseWriter, r *http.Request) {
	var request segmentBatchRequest
	if !h.decode(w, r, &request) {
		return
	}
	if len(request.Segments) == 0 || len(request.Segments) > 200 {
		h.problem(w, http.StatusUnprocessableEntity, "validation", "批量片段数量必须介于 1 和 200")
		return
	}
	detail, err := h.service.ImportSegmentBatch(r.PathValue("id"), request.ExpectedVersion, application.ImportSegmentsCommand{Segments: request.Segments}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) QueryFindings(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	pageSize := 50
	var err error
	if raw := values.Get("pageSize"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil {
			h.problem(w, http.StatusUnprocessableEntity, "validation", "pageSize 必须为整数")
			return
		}
	}
	from, to := 0, 0
	if raw := values.Get("sequenceFrom"); raw != "" {
		from, err = strconv.Atoi(raw)
		if err != nil {
			h.problem(w, http.StatusUnprocessableEntity, "validation", "sequenceFrom 必须为整数")
			return
		}
	}
	if raw := values.Get("sequenceTo"); raw != "" {
		to, err = strconv.Atoi(raw)
		if err != nil {
			h.problem(w, http.StatusUnprocessableEntity, "validation", "sequenceTo 必须为整数")
			return
		}
	}
	query := application.FindingQuery{RuleCode: values.Get("rule"), Participant: values.Get("participant"), Status: domain.FindingStatus(values.Get("status")), SequenceFrom: from, SequenceTo: to, PageSize: pageSize, Cursor: values.Get("cursor")}
	page, queryErr := h.service.QueryFindings(r.PathValue("id"), query)
	if queryErr != nil {
		h.writeError(w, queryErr)
		return
	}
	h.writeJSON(w, http.StatusOK, page)
}

func (h *Handler) BatchRemediateFindings(w http.ResponseWriter, r *http.Request) {
	var request batchRemediationRequest
	if !h.decode(w, r, &request) {
		return
	}
	if len(request.FindingIDs) == 0 || len(request.FindingIDs) > 100 {
		h.problem(w, http.StatusUnprocessableEntity, "validation", "findingId 数量必须介于 1 和 100")
		return
	}
	seen := map[string]bool{}
	for _, id := range request.FindingIDs {
		if seen[id] {
			h.problem(w, http.StatusUnprocessableEntity, "validation", "findingId 不得重复")
			return
		}
		seen[id] = true
	}
	command := application.BatchRemediateCommand{FindingIDs: request.FindingIDs, RemediationType: request.RemediationType, BeforeNote: request.BeforeNote, AfterNote: request.AfterNote}
	detail, err := h.service.BatchRemediate(r.PathValue("id"), request.ExpectedVersion, command, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) SaveReviewProgress(w http.ResponseWriter, r *http.Request) {
	var request reviewProgressRequest
	if !h.decode(w, r, &request) {
		return
	}
	detail, err := h.service.SaveReviewProgress(r.PathValue("id"), request.ExpectedVersion, application.SaveReviewProgressCommand{VerifiedFindingIDs: request.VerifiedFindingIDs, Notes: request.Notes}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) ListCases(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]any{"items": h.service.List()})
}

func (h *Handler) CreateCase(w http.ResponseWriter, r *http.Request) {
	var command application.CreateCaseCommand
	if !h.decode(w, r, &command) {
		return
	}
	detail, err := h.service.Create(command, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, detail)
}

func (h *Handler) GetCase(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.Get(r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) FreezeConsents(w http.ResponseWriter, r *http.Request) {
	var request freezeRequest
	if !h.decode(w, r, &request) {
		return
	}
	detail, err := h.service.FreezeConsents(r.PathValue("id"), request.ExpectedVersion, application.FreezeConsentsCommand{Consents: request.Consents}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) AddSegment(w http.ResponseWriter, r *http.Request) {
	var request segmentRequest
	if !h.decode(w, r, &request) {
		return
	}
	command := application.AddSegmentCommand{StartMillis: request.StartMillis, EndMillis: request.EndMillis, Summary: request.Summary, MentionedParticipants: request.MentionedParticipants, TopicTags: request.TopicTags, LocationTag: request.LocationTag}
	detail, err := h.service.AddSegment(r.PathValue("id"), request.ExpectedVersion, command, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) ScanCase(w http.ResponseWriter, r *http.Request) {
	var request mutationMeta
	if !h.decode(w, r, &request) {
		return
	}
	detail, err := h.service.Scan(r.PathValue("id"), request.ExpectedVersion, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) RemediateFinding(w http.ResponseWriter, r *http.Request) {
	var request remediationRequest
	if !h.decode(w, r, &request) {
		return
	}
	var detail application.Detail
	var err error
	if request.RemediationType == domain.RemediationSupplemental {
		command := application.SupplementalConsentCommand{FindingID: request.FindingID, ParticipantName: request.ParticipantName, EvidenceDigest: request.EvidenceDigest, AuthorizedDate: request.AuthorizedDate, RuleCode: request.RuleCode, SegmentIDs: request.SegmentIDs, BeforeNote: request.BeforeNote, AfterNote: request.AfterNote}
		detail, err = h.service.RemediateWithSupplementalConsent(r.PathValue("id"), request.ExpectedVersion, command, r.Header.Get("Idempotency-Key"))
	} else {
		command := application.RemediateCommand{FindingID: request.FindingID, RemediationType: request.RemediationType, BeforeNote: request.BeforeNote, AfterNote: request.AfterNote}
		detail, err = h.service.Remediate(r.PathValue("id"), request.ExpectedVersion, command, r.Header.Get("Idempotency-Key"))
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) SubmitReview(w http.ResponseWriter, r *http.Request) {
	var request reviewSubmitRequest
	if !h.decode(w, r, &request) {
		return
	}
	detail, err := h.service.SubmitReview(r.PathValue("id"), request.ExpectedVersion, application.SubmitReviewCommand{ReviewerName: request.ReviewerName}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) ReturnReview(w http.ResponseWriter, r *http.Request) {
	var request reviewReturnRequest
	if !h.decode(w, r, &request) {
		return
	}
	detail, err := h.service.ReturnReview(r.PathValue("id"), request.ExpectedVersion, application.ReturnReviewCommand{FindingIDs: request.FindingIDs, Note: request.Note}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) ApproveReview(w http.ResponseWriter, r *http.Request) {
	var request reviewApproveRequest
	if !h.decode(w, r, &request) {
		return
	}
	// 批准只读取服务器已暂存进度，不接受浏览器在批准请求中临时补齐清单。
	detail, err := h.service.Approve(r.PathValue("id"), request.ExpectedVersion, application.ApproveCommand{}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) VerifyCredential(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.VerifyDetailed(r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"valid": report.Valid, "message": "发布凭据分项校验完成", "checks": report.Checks, "recordCount": report.RecordCount, "anchoredSequence": report.AnchoredSequence, "anchoredHash": report.AnchoredHash, "chainTailHash": report.ChainTailHash, "verifiedAt": time.Now().UTC()})
}

func (h *Handler) ExportCredential(w http.ResponseWriter, r *http.Request) {
	payload, filename, err := h.service.ExportCredential(r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.problem(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type 必须为 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		h.problem(w, http.StatusBadRequest, "invalid_json", decodeMessage(err))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		h.problem(w, http.StatusBadRequest, "invalid_json", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func decodeMessage(err error) string {
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		return fmt.Sprintf("请求体不得超过 %d 字节", max.Limit)
	}
	return "JSON 请求体无效：" + err.Error()
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.problem(w, http.StatusNotFound, "not_found", "发布案或业务对象不存在")
	case errors.Is(err, domain.ErrVersionConflict):
		h.problem(w, http.StatusConflict, "version_conflict", "版本已变化，请刷新后重试")
	case errors.Is(err, journal.ErrIdempotencyConflict):
		h.problem(w, http.StatusConflict, "idempotency_conflict", "幂等键已用于不同请求")
	case errors.Is(err, domain.ErrScanStale):
		h.problem(w, http.StatusConflict, "scan_stale", "扫描结果已经过期，请重新扫描")
	case errors.Is(err, domain.ErrOpenFindings):
		h.problem(w, http.StatusConflict, "open_findings", "仍有未关闭冲突")
	case errors.Is(err, domain.ErrCredentialExists):
		h.problem(w, http.StatusConflict, "credential_exists", "发布凭据已经签发")
	case errors.Is(err, domain.ErrArchiveCodeConflict):
		var conflict *domain.ArchiveCodeConflictError
		if errors.As(err, &conflict) {
			h.writeJSON(w, http.StatusConflict, map[string]any{"problem": map[string]any{"code": "archive_code_conflict", "message": conflict.Error(), "archiveCode": conflict.ArchiveCode, "existingCaseId": conflict.ExistingCaseID}})
		} else {
			h.problem(w, http.StatusConflict, "archive_code_conflict", err.Error())
		}
	case errors.Is(err, domain.ErrCredentialMissing):
		h.problem(w, http.StatusNotFound, "credential_missing", "发布凭据不存在")
	case errors.Is(err, domain.ErrCursorStale):
		h.problem(w, http.StatusConflict, "cursor_stale", "分页游标已经过期，请刷新后重试")
	case errors.Is(err, journal.ErrCorruptJournal):
		h.problem(w, http.StatusInternalServerError, "journal_integrity", "事件日志完整性校验失败")
	case errors.Is(err, domain.ErrInvalidState):
		h.problem(w, http.StatusConflict, "invalid_state", err.Error())
	case errors.Is(err, domain.ErrValidation):
		var incomplete *domain.ReviewIncompleteError
		if errors.As(err, &incomplete) {
			h.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problem": map[string]any{"code": "review_incomplete", "message": incomplete.Error(), "unverifiedFindingIds": incomplete.FindingIDs}})
		} else {
			h.problem(w, http.StatusUnprocessableEntity, "validation", err.Error())
		}
	default:
		h.problem(w, http.StatusInternalServerError, "internal_error", "服务器无法完成请求")
	}
}

func (h *Handler) problem(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, map[string]any{"problem": map[string]any{"code": code, "message": message}})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				h.problem(w, http.StatusForbidden, "cross_origin", "拒绝跨源写入请求")
				return
			}
			key := r.Header.Get("Idempotency-Key")
			readOnlyPreview := strings.HasSuffix(r.URL.Path, "/preview")
			if !readOnlyPreview && (len(key) > 128 || strings.TrimSpace(key) == "") {
				h.problem(w, http.StatusBadRequest, "idempotency_key_required", "写入请求必须提供长度不超过 128 的 Idempotency-Key")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
