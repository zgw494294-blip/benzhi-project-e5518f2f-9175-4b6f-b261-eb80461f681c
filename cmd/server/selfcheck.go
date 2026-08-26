package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"oral-history-clearance/internal/application"
	"oral-history-clearance/internal/domain"
	"oral-history-clearance/internal/journal"
)

func runSelfcheck(address string) error {
	tempDir, err := os.MkdirTemp("", "oral-history-clearance-selfcheck-")
	if err != nil {
		return fmt.Errorf("创建自检目录: %w", err)
	}
	defer os.RemoveAll(tempDir)
	repo, err := journal.Open(filepath.Join(tempDir, "events.jsonl"))
	if err != nil {
		return err
	}
	defer repo.Close()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("自检监听 %s: %w", address, err)
	}
	server := newHTTPServer(buildHandler(repo))
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	baseURL := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}
	if err := executeSelfcheck(client, baseURL); err != nil {
		_ = server.Close()
		<-serveDone
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("关闭自检服务: %w", err)
	}
	if err := <-serveDone; err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("自检服务异常: %w", err)
	}
	fmt.Println("selfcheck: 完整发布放行流程与凭据校验通过")
	return nil
}

func executeSelfcheck(client *http.Client, baseURL string) error {
	response, err := client.Get(baseURL + "/")
	if err != nil {
		return fmt.Errorf("读取工作台: %w", err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(page, []byte("口述史发布放行工作台")) {
		return fmt.Errorf("工作台页面检查失败: status=%d err=%v", response.StatusCode, err)
	}
	var detail application.Detail
	if err := selfcheckPost(client, baseURL+"/api/cases", "selfcheck-create", map[string]any{"archiveCode": "SELF-OH-001", "title": "自检口述史", "editorName": "自检整理员", "targetPublishDate": "2030-06-01"}, &detail); err != nil {
		return err
	}
	caseID := detail.Case.ID
	consents := []domain.ParticipantConsent{{ParticipantName: "受访者甲", IdentityDisclosure: false, RestrictedTopics: []string{"家庭迁移"}, LocationPrecision: "city", EvidenceDigest: "自检授权证据摘要"}}
	if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/consents/freeze", "selfcheck-consents", map[string]any{"expectedVersion": detail.Case.Version, "consents": consents}, &detail); err != nil {
		return err
	}
	segment := map[string]any{"expectedVersion": detail.Case.Version, "startMillis": 0, "endMillis": 12000, "summary": "受访者甲讲述家庭迁移及旧居", "mentionedParticipants": []string{"受访者甲"}, "topicTags": []string{"家庭迁移"}, "locationTag": "exact:旧居门牌"}
	if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/segments", "selfcheck-segment", segment, &detail); err != nil {
		return err
	}
	if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/scan", "selfcheck-scan", map[string]any{"expectedVersion": detail.Case.Version}, &detail); err != nil {
		return err
	}
	if detail.Case.OpenFindingCount() == 0 {
		return fmt.Errorf("自检扫描没有产生预期冲突")
	}
	step := 0
	for detail.Case.OpenFindingCount() > 0 {
		var finding domain.ConflictFinding
		for _, item := range detail.Case.Findings {
			if item.Status == domain.FindingOpen {
				finding = item
				break
			}
		}
		step++
		body := map[string]any{"expectedVersion": detail.Case.Version, "findingId": finding.ID, "remediationType": "supplemental_consent", "beforeNote": finding.Reason, "afterNote": "自检补充授权证据已冻结", "participantName": finding.ParticipantName, "evidenceDigest": "自检补充授权记录", "authorizedDate": time.Now().UTC().Format("2006-01-02"), "ruleCode": finding.RuleCode, "segmentIds": []string{finding.SegmentID}}
		if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/findings/remediate", fmt.Sprintf("selfcheck-remediate-%d", step), body, &detail); err != nil {
			return err
		}
		if step > 10 {
			return fmt.Errorf("自检整改未能收敛")
		}
	}
	if detail.Case.Status != domain.StatusReady {
		return fmt.Errorf("自检整改后状态错误: %s", detail.Case.Status)
	}
	if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/review/submit", "selfcheck-review", map[string]any{"expectedVersion": detail.Case.Version, "reviewerName": "自检伦理复核员"}, &detail); err != nil {
		return err
	}
	verified := make([]string, 0)
	for _, finding := range detail.Case.Findings {
		if finding.Status == domain.FindingResolved {
			verified = append(verified, finding.ID)
		}
	}
	if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/review/progress", "selfcheck-progress", map[string]any{"expectedVersion": detail.Case.Version, "verifiedFindingIds": verified, "notes": map[string]string{}}, &detail); err != nil {
		return err
	}
	if err := selfcheckPost(client, baseURL+"/api/cases/"+caseID+"/review/approve", "selfcheck-approve", map[string]any{"expectedVersion": detail.Case.Version, "verifiedFindingIds": []string{}}, &detail); err != nil {
		return err
	}
	if detail.Case.Status != domain.StatusReleased || detail.Case.Credential == nil {
		return fmt.Errorf("自检未签发凭据")
	}
	verifyResponse, err := client.Get(baseURL + "/api/cases/" + caseID + "/credential/verify")
	if err != nil {
		return err
	}
	defer verifyResponse.Body.Close()
	var verification struct {
		Valid   bool   `json:"valid"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(verifyResponse.Body).Decode(&verification); err != nil {
		return fmt.Errorf("解析校验响应: %w", err)
	}
	if verifyResponse.StatusCode != http.StatusOK || !verification.Valid {
		return fmt.Errorf("凭据校验失败: %s", verification.Message)
	}
	return nil
}

func selfcheckPost(client *http.Client, url, idempotencyKey string, body any, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("自检请求 %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("自检请求 %s 返回 %d: %s", url, response.StatusCode, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("解析自检响应 %s: %w", url, err)
	}
	return nil
}
