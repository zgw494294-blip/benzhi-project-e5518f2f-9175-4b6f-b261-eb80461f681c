"use strict";

const state = { cases: [], detail: null, consentDrafts: [], consentRevisionMode: false, segmentBatch: null, findingPage: null };
const $ = (selector) => document.querySelector(selector);
const splitTerms = (value) => value.split(/[，,]/).map(v => v.trim()).filter(Boolean);
const key = (action) => `${action}-${Date.now()}-${crypto.randomUUID()}`;
const statusNames = { draft: "草拟中", remediation: "待整改", ready_for_review: "待提交复核", in_review: "伦理复核中", released: "已发布放行" };

async function api(path, options = {}) {
  const init = { ...options, headers: { Accept: "application/json", ...(options.headers || {}) } };
  if (init.body) init.headers["Content-Type"] = "application/json";
  const response = await fetch(path, init);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.problem?.message || `请求失败（${response.status}）`);
  return body;
}

function notify(message, error = false) {
  const box = $("#notice");
  box.textContent = message;
  box.classList.toggle("error", error);
  box.classList.remove("hidden");
  window.clearTimeout(notify.timer);
  notify.timer = window.setTimeout(() => box.classList.add("hidden"), 4500);
}

async function loadCases(selectID) {
  const result = await api("/api/cases");
  state.cases = result.items || [];
  renderCaseList();
  if (selectID) await openCase(selectID);
  else if (!state.detail && state.cases.length) await openCase(state.cases[0].case.id);
}

function renderCaseList() {
  const list = $("#case-list");
  list.innerHTML = state.cases.length ? state.cases.map(item => {
    const c = item.case;
    return `<button class="case-item ${state.detail?.case.id === c.id ? "active" : ""}" data-case="${escapeHTML(c.id)}"><strong>${escapeHTML(c.title)}</strong><small>${escapeHTML(c.archiveCode)} · ${statusNames[c.status]}</small></button>`;
  }).join("") : `<div class="empty">尚无发布案</div>`;
  list.querySelectorAll("[data-case]").forEach(button => button.addEventListener("click", () => openCase(button.dataset.case)));
}

async function openCase(id) {
  state.detail = await api(`/api/cases/${encodeURIComponent(id)}`);
  state.consentDrafts = [];
  state.consentRevisionMode = false;
  state.segmentBatch = null;
  $("#create-panel").classList.add("hidden");
  $("#case-panel").classList.remove("hidden");
  renderCaseList();
  renderDetail();
}

function renderDetail() {
  const d = state.detail;
  const c = d.case;
  $("#case-head").innerHTML = `<div><h2>${escapeHTML(c.title)}</h2><p>${escapeHTML(c.archiveCode)} · 整理员 ${escapeHTML(c.editorName)} · 计划 ${escapeHTML(c.targetPublishDate)}</p></div><div><span class="status">${statusNames[c.status]}</span> <span class="version">v${c.version}</span></div>`;
  const profile = $("#profile-form");
  profile.elements.title.value = c.title;
  profile.elements.editorName.value = c.editorName;
  profile.elements.targetPublishDate.value = c.targetPublishDate;
  $("#profile-editor").classList.toggle("hidden", !["draft", "remediation", "ready_for_review"].includes(c.status));
  const stages = ["建立发布案", "冻结授权", "片段目录", "冲突整改", "伦理复核", "放行凭据"];
  const stage = stageIndex(c);
  $("#progress").innerHTML = stages.map((name, index) => `<li class="${index < stage ? "done" : index === stage ? "current" : ""}">${name}</li>`).join("");
  $("#todo-strip").textContent = d.todos?.length ? d.todos.map(t => t.message).join(" · ") : "本轮流程已完成";
  renderConsents(c);
  renderSegments(c);
  renderFindings(c);
  renderReview(c);
  renderCredential(c);
}

function stageIndex(c) {
  if (c.status === "released") return 5;
  if (c.status === "in_review") return 4;
  if (["remediation", "ready_for_review"].includes(c.status)) return 3;
  if ((c.segments || []).length) return 2;
  if ((c.consents || []).length) return 1;
  return 0;
}

function renderConsents(c) {
  $("#consent-revision").textContent = c.consentRevision ? `冻结修订 ${c.consentRevision}` : "尚未冻结";
  const frozen = c.consentHistory?.at(-1);
  $("#consent-existing").innerHTML = (frozen ? `<p class="meta">本修订原因：${escapeHTML(frozen.reason)} · 冻结于 ${escapeHTML(frozen.frozenAt)}</p>` : "") + (c.consents?.length ? c.consents.map(x => `<div class="record"><div class="record-head"><strong>${escapeHTML(x.participantName)}</strong><span class="tag">${x.identityDisclosure ? "身份可公开" : "身份受限"}</span></div><p>地点精度：${escapeHTML(x.locationPrecision)}；禁用至：${escapeHTML(x.embargoUntil || "无")}</p><p>限制话题：${escapeHTML((x.restrictedTopics || []).join("、") || "无")}</p><p class="meta">证据：${escapeHTML(x.evidenceDigest)}</p></div>`).join("") : `<div class="empty">先添加受访者授权边界，再一次性冻结。</div>`);
  const stateEditable = ["draft", "remediation", "ready_for_review"].includes(c.status);
  const editable = stateEditable && (!c.consents?.length || state.consentRevisionMode);
  $("#consent-form").classList.toggle("hidden", !editable);
  $("#freeze-consents").classList.toggle("hidden", Boolean(c.consents?.length));
  $("#start-consent-revision").classList.toggle("hidden", !stateEditable || !c.consents?.length || state.consentRevisionMode);
  $("#consent-revision-actions").classList.toggle("hidden", !state.consentRevisionMode);
  renderConsentDrafts();
}

function renderConsentDrafts() {
  $("#consent-draft").innerHTML = state.consentDrafts.map((x, i) => `<div class="draft-item"><span>${escapeHTML(x.participantName)} · ${x.identityDisclosure ? "身份可公开" : "身份受限"} · ${escapeHTML(x.locationPrecision)}</span><button class="quiet" data-remove-consent="${i}">移除</button></div>`).join("");
  document.querySelectorAll("[data-remove-consent]").forEach(button => button.addEventListener("click", () => { state.consentDrafts.splice(Number(button.dataset.removeConsent), 1); renderConsentDrafts(); }));
}

function renderSegments(c) {
  $("#segment-list").innerHTML = c.segments?.length ? c.segments.map(s => `<div class="record"><div class="record-head"><strong>#${s.sequence} ${escapeHTML(s.summary)}</strong><span class="meta">${s.startMillis}–${s.endMillis} ms</span></div><div class="chips">${[...(s.mentionedParticipants || []), ...(s.topicTags || []), s.locationTag].filter(Boolean).map(v => `<span class="chip">${escapeHTML(v)}</span>`).join("")}</div>${s.deleted ? "<p>已从公开清单删除</p>" : ""}</div>`).join("") : `<div class="empty">尚未登记录音片段。</div>`;
  const editable = ["draft", "remediation", "ready_for_review"].includes(c.status) && c.consents?.length;
  $("#segment-form").classList.toggle("hidden", !editable);
  $("#run-scan").classList.toggle("hidden", !(editable && c.segments?.length));
  $("#segment-batch-panel").classList.toggle("hidden", !editable);
}

function renderFindings(c) {
  const findings = c.findings || [];
  const current = c.lastScanRevision === c.contentRevision && c.lastScanConsentRevision === c.consentRevision && c.lastScanRevision > 0 && !c.needsFullScan;
  $("#scan-state").textContent = current ? `当前结论 · 扫描修订 ${c.lastScanRevision}` : (findings.length ? "历史结论 · 已过期" : "尚未扫描或已过期");
  $("#historical-warning").classList.toggle("hidden", current || !findings.length);
  $("#historical-warning").textContent = "当前显示的是历史扫描结论，关闭数量不能作为提交伦理复核的依据。";
  renderFindingItems(findings, c);
  loadFindingQuery().catch(error => notify(error.message, true));
}

function renderFindingItems(findings, c) {
  $("#finding-list").innerHTML = findings.length ? findings.map(f => `<article class="finding ${f.status}"><div class="record-head"><strong>${f.status === "open" ? `<input type="checkbox" name="batchFinding" value="${escapeAttr(f.id)}" aria-label="选择冲突"> ` : ""}${escapeHTML(f.ruleCode)}</strong><span class="tag">${f.status === "open" ? "待整改" : "已关闭"}</span></div><p>${escapeHTML(f.reason)}</p><p class="meta">${escapeHTML(f.participantName || "")} · ${escapeHTML(f.segmentId)} · ${escapeHTML(f.id)}</p><details><summary>展开命中依据</summary><p>授权字段 ${escapeHTML(f.basis?.consentField || "-")} = ${escapeHTML(f.basis?.consentValue || "-")}</p><p>片段字段 ${escapeHTML(f.basis?.segmentField || "-")} = ${escapeHTML(f.basis?.segmentValue || "-")}</p><p class="meta">规则 ${escapeHTML(f.ruleCode)} · consentRevision ${f.consentRevision || c.consentRevision} · contentRevision ${f.contentRevision || f.scanRevision} · scanRevision ${f.scanRevision}</p></details>${f.status === "resolved" ? `<p>整改：${escapeHTML(f.remediationType)} · ${escapeHTML(f.afterNote || "")}</p>` : remediationForm(f)}</article>`).join("") : `<div class="empty">${c.lastScanRevision ? "扫描未发现授权冲突。" : "运行扫描后显示逐项命中依据。"}</div>`;
  document.querySelectorAll(".remediation-form").forEach(form => form.addEventListener("submit", remediate));
  document.querySelectorAll("[name=batchFinding]").forEach(box => box.addEventListener("change", updateBatchRemediationOptions));
}

function updateBatchRemediationOptions() {
  const selected = new Set([...document.querySelectorAll("[name=batchFinding]:checked")].map(x => x.value));
  $("#batch-remediation").classList.toggle("hidden", selected.size === 0);
  const rules = (state.findingPage?.items || state.detail.case.findings || []).filter(f => selected.has(f.id)).map(f => f.ruleCode);
  const compatibility = { mute: true, delete_segment: true, pseudonym: rules.length > 0 && rules.every(rule => rule === "IDENTITY"), generalize_location: rules.length > 0 && rules.every(rule => rule === "LOCATION") };
  const select = $("#batch-remediation-type");
  [...select.options].forEach(option => { option.disabled = !compatibility[option.value]; option.hidden = option.disabled; });
  if (select.selectedOptions[0]?.disabled) select.value = "mute";
}

function remediationForm(finding) {
  const today = new Date().toISOString().slice(0, 10);
  return `<form class="remediation-form" data-finding="${escapeHTML(finding.id)}"><label>整改方式<select name="remediationType"><option value="mute">静音</option><option value="pseudonym">化名</option><option value="generalize_location">地点泛化</option><option value="delete_segment">删除片段</option><option value="supplemental_consent">补充授权</option></select></label><label>整改前说明<input name="beforeNote" required value="${escapeAttr(finding.reason)}"></label><label>整改后说明<input name="afterNote" required placeholder="描述变更或补充证据"></label><details><summary>补充授权证据字段（选择补充授权时必填）</summary><label>受访者<input name="participantName" value="${escapeAttr(finding.participantName || "")}"></label><label>证据摘要<input name="evidenceDigest"></label><label>授权日期<input name="authorizedDate" type="date" max="${today}" value="${today}"></label><input name="ruleCode" type="hidden" value="${escapeAttr(finding.ruleCode)}"><input name="segmentIds" type="hidden" value="${escapeAttr(finding.segmentId)}"></details><button class="primary">提交并定向重扫</button></form>`;
}

function renderReview(c) {
  const review = c.review;
  const progress = state.detail.reviewProgress;
  const history = (c.reviewHistory || []).map(item => `<p class="meta">历史第 ${item.round} 轮：核验 ${item.verifiedFindingIds?.length || 0} 项，结束于 ${escapeHTML(item.closedAt || "-")}</p>`).join("");
  $("#review-state").innerHTML = (review ? `<div class="record"><strong>${escapeHTML(review.reviewerName)} · 第 ${review.round || 1} 轮</strong><p>决定：${escapeHTML(review.decision)} ${review.note ? "· " + escapeHTML(review.note) : ""}</p><p>已核验 ${progress?.verifiedFindingIds?.length || 0} 项，待核验 ${progress?.unverifiedFindingIds?.length || 0} 项</p><p class="meta">提交于 ${escapeHTML(review.submittedAt)}${progress?.lastSavedAt ? ` · 最后保存 ${escapeHTML(progress.lastSavedAt)}` : ""}</p></div>` : `<div class="empty">全部冲突关闭且扫描未过期后可提交。</div>`) + history;
  $("#review-submit-form").classList.toggle("hidden", c.status !== "ready_for_review");
  $("#review-actions").classList.toggle("hidden", c.status !== "in_review");
  const saved = new Set(progress?.verifiedFindingIds || []);
  $("#return-findings").innerHTML = (c.findings || []).filter(f => f.status === "resolved").map(f => `<label class="check"><input type="checkbox" name="reviewFinding" value="${escapeAttr(f.id)}" ${saved.has(f.id) ? "checked" : ""}>已逐项核验：${escapeHTML(f.ruleCode)} · ${escapeHTML(f.segmentId)}</label>`).join("") || `<p class="empty">本案没有需要逐项核验的历史冲突；可直接批准。</p>`;
}

function renderCredential(c) {
  const credential = c.credential;
  $("#credential-view").innerHTML = credential ? `<div class="record"><div class="record-head"><strong>${escapeHTML(credential.id)}</strong><span class="tag">事件 #${credential.eventSequence}</span></div><p>复核员：${escapeHTML(credential.reviewerName)} · 授权修订：${credential.consentRevision} · 案卷版本：${credential.caseVersion}</p><p>公开片段：${escapeHTML(credential.includedSegmentIds.join("、"))}</p><div class="digest">SHA-256 ${escapeHTML(credential.canonicalDigest)}</div></div>` : `批准后将在此展示冻结范围、事件序号和规范化摘要。`;
  $("#verify-credential").classList.toggle("hidden", !credential);
  $("#download-credential").classList.toggle("hidden", !credential);
  $("#download-credential").href = credential ? `/api/cases/${encodeURIComponent(c.id)}/credential/export` : "";
  $("#verify-result").innerHTML = "";
}

async function mutate(path, body, action) {
  try {
    const detail = await api(path, { method: "POST", headers: { "Idempotency-Key": key(action) }, body: JSON.stringify(body) });
    state.detail = detail;
    renderDetail();
    await loadCases();
    notify("变更已写入可追溯日志");
  } catch (error) { notify(error.message, true); }
}

async function loadFindingQuery() {
  if (!state.detail) return;
  const form = $("#finding-filter");
  const data = new FormData(form);
  const query = new URLSearchParams();
  for (const [name, value] of data) if (String(value).trim()) query.set(name, value);
  const page = await api(`/api/cases/${encodeURIComponent(state.detail.case.id)}/findings?${query}`);
  state.findingPage = page;
  const stats = page.statistics;
  $("#finding-statistics").innerHTML = `<div class="record"><strong>${stats.openCount} 项未关闭 · ${stats.affectedSegmentCount} 个受影响片段</strong><p>规则：${escapeHTML((stats.byRule || []).map(x => `${x.key} ${x.count}`).join(" · ") || "无命中")}</p><p class="meta">扫描修订 ${stats.scanRevision} · 授权修订 ${stats.consentRevision} · 材料修订 ${stats.contentRevision}</p></div>`;
  renderFindingItems(page.items || [], state.detail.case);
}

function parseSegmentBatch() {
  return $("#segment-batch-input").value.split(/\r?\n/).filter(line => line.trim()).map((line, index) => {
    const [start, end, summary = "", people = "", topics = "", locationTag = ""] = line.split("\t");
    return { row: index + 1, startMillis: Number(start), endMillis: Number(end), summary, mentionedParticipants: splitTerms(people), topicTags: splitTerms(topics), locationTag };
  });
}

$("#new-case").addEventListener("click", () => {
  state.detail = null;
  state.consentDrafts = [];
  $("#case-panel").classList.add("hidden");
  $("#create-panel").classList.remove("hidden");
  renderCaseList();
});

$("#create-form").addEventListener("submit", async event => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.currentTarget));
  try {
    const detail = await api("/api/cases", { method: "POST", headers: { "Idempotency-Key": key("create") }, body: JSON.stringify(data) });
    state.detail = detail;
    event.currentTarget.reset();
    await loadCases(detail.case.id);
    notify("发布案已创建");
  } catch (error) { notify(error.message, true); }
});

$("#profile-form").addEventListener("submit", event => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.currentTarget));
  mutate(`/api/cases/${state.detail.case.id}/profile`, { expectedVersion: state.detail.case.version, ...data }, "profile");
});

$("#consent-form").addEventListener("submit", event => {
  event.preventDefault();
  const form = event.currentTarget;
  const data = Object.fromEntries(new FormData(form));
  state.consentDrafts.push({ participantName: data.participantName, identityDisclosure: form.elements.identityDisclosure.checked, restrictedTopics: splitTerms(data.restrictedTopics || ""), locationPrecision: data.locationPrecision, embargoUntil: data.embargoUntil || "", evidenceDigest: data.evidenceDigest });
  form.reset();
  renderConsentDrafts();
});

$("#freeze-consents").addEventListener("click", () => {
  if (!state.consentDrafts.length) return notify("请先加入至少一位受访者", true);
  mutate(`/api/cases/${state.detail.case.id}/consents/freeze`, { expectedVersion: state.detail.case.version, consents: state.consentDrafts }, "freeze");
});

$("#start-consent-revision").addEventListener("click", () => {
  state.consentRevisionMode = true;
  state.consentDrafts = (state.detail.case.consents || []).map(x => ({ participantName: x.participantName, identityDisclosure: x.identityDisclosure, restrictedTopics: [...(x.restrictedTopics || [])], locationPrecision: x.locationPrecision, embargoUntil: x.embargoUntil || "", evidenceDigest: x.evidenceDigest }));
  renderConsents(state.detail.case);
});

$("#preview-consent-revision").addEventListener("click", async () => {
  try {
    const preview = await api(`/api/cases/${state.detail.case.id}/consents/revisions/preview`, { method: "POST", body: JSON.stringify({ consents: state.consentDrafts }) });
    $("#consent-diff").innerHTML = preview.problems?.length ? preview.problems.map(x => `<div class="record error"><strong>${escapeHTML(x.participantName || "清单")} · ${escapeHTML(x.code)}</strong><p>${escapeHTML(x.message)}${x.segmentId ? ` · ${escapeHTML(x.segmentId)}` : ""}</p></div>`).join("") : (preview.differences?.length ? preview.differences.map(x => `<div class="record"><strong>${escapeHTML(x.participantName)} · ${escapeHTML(x.kind)}</strong><p>${escapeHTML(x.field || "清单")}：${escapeHTML(x.before || "-")} → ${escapeHTML(x.after || "-")}</p></div>`).join("") : `<div class="empty">清单没有实际差异。</div>`);
  } catch (error) { notify(error.message, true); }
});

$("#confirm-consent-revision").addEventListener("click", () => {
  const reason = $("#consent-reason").value.trim();
  if (!reason) return notify("请填写授权修订原因", true);
  mutate(`/api/cases/${state.detail.case.id}/consents/revisions`, { expectedVersion: state.detail.case.version, baseConsentRevision: state.detail.case.consentRevision, reason, consents: state.consentDrafts }, "consent-revision");
  state.consentRevisionMode = false;
});

$("#segment-form").addEventListener("submit", event => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.currentTarget));
  mutate(`/api/cases/${state.detail.case.id}/segments`, { expectedVersion: state.detail.case.version, startMillis: Number(data.startMillis), endMillis: Number(data.endMillis), summary: data.summary, mentionedParticipants: splitTerms(data.mentionedParticipants || ""), topicTags: splitTerms(data.topicTags || ""), locationTag: data.locationTag || "" }, "segment");
  event.currentTarget.reset();
});

$("#run-scan").addEventListener("click", () => mutate(`/api/cases/${state.detail.case.id}/scan`, { expectedVersion: state.detail.case.version }, "scan"));

$("#preview-segment-batch").addEventListener("click", async () => {
  try {
    const segments = parseSegmentBatch();
    const preview = await api(`/api/cases/${state.detail.case.id}/segments/batch/preview`, { method: "POST", body: JSON.stringify({ segments }) });
    state.segmentBatch = preview.valid ? segments : null;
    $("#confirm-segment-batch").disabled = !preview.valid;
    const problems = new Map(); (preview.problems || []).forEach(p => { const list = problems.get(p.row) || []; list.push(p.message); problems.set(p.row, list); });
    $("#segment-batch-preview").innerHTML = (preview.rows || []).map(row => `<div class="record"><strong>#${row.sequence} · 输入行 ${row.row} · ${row.startMillis}–${row.endMillis} ms</strong><p>${escapeHTML(row.summary)}</p>${problems.has(row.row) ? `<p class="error">${escapeHTML(problems.get(row.row).join("；"))}</p>` : `<p class="meta">校验通过</p>`}</div>`).join("");
  } catch (error) { notify(error.message, true); }
});

$("#confirm-segment-batch").addEventListener("click", () => {
  if (!state.segmentBatch) return;
  mutate(`/api/cases/${state.detail.case.id}/segments/batch`, { expectedVersion: state.detail.case.version, segments: state.segmentBatch }, "segment-batch");
  state.segmentBatch = null;
});

function remediate(event) {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.currentTarget));
  if (data.segmentIds) data.segmentIds = [data.segmentIds];
  mutate(`/api/cases/${state.detail.case.id}/findings/remediate`, { expectedVersion: state.detail.case.version, findingId: event.currentTarget.dataset.finding, ...data }, "remediate");
}

$("#finding-filter").addEventListener("submit", event => { event.preventDefault(); loadFindingQuery().catch(error => notify(error.message, true)); });

$("#apply-batch-remediation").addEventListener("click", () => {
  const findingIds = [...document.querySelectorAll("[name=batchFinding]:checked")].map(x => x.value);
  const afterNote = $("#batch-remediation-note").value.trim();
  if (!findingIds.length || !afterNote) return notify("请选择冲突并填写统一说明", true);
  mutate(`/api/cases/${state.detail.case.id}/findings/batch-remediate`, { expectedVersion: state.detail.case.version, findingIds, remediationType: $("#batch-remediation-type").value, beforeNote: "批量整改前逐项依据见原冲突记录", afterNote }, "batch-remediate");
});

$("#review-submit-form").addEventListener("submit", event => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.currentTarget));
  mutate(`/api/cases/${state.detail.case.id}/review/submit`, { expectedVersion: state.detail.case.version, reviewerName: data.reviewerName }, "review-submit");
});

$("#return-review").addEventListener("click", () => {
  const findingIds = [...document.querySelectorAll("[name=reviewFinding]:checked")].map(x => x.value);
  const note = $("#return-note").value.trim();
  if (!findingIds.length || !note) return notify("请选择冲突并填写退回说明", true);
  mutate(`/api/cases/${state.detail.case.id}/review/return`, { expectedVersion: state.detail.case.version, findingIds, note }, "review-return");
});

$("#save-review-progress").addEventListener("click", () => {
  const verifiedFindingIds = [...document.querySelectorAll("[name=reviewFinding]:checked")].map(x => x.value);
  mutate(`/api/cases/${state.detail.case.id}/review/progress`, { expectedVersion: state.detail.case.version, verifiedFindingIds, notes: {} }, "review-progress");
});

$("#approve-review").addEventListener("click", () => {
  if (state.detail.reviewProgress?.unverifiedFindingIds?.length) return notify("请先核验全部冲突并保存进度", true);
  mutate(`/api/cases/${state.detail.case.id}/review/approve`, { expectedVersion: state.detail.case.version, verifiedFindingIds: [] }, "approve");
});

$("#verify-credential").addEventListener("click", async () => {
  try {
    const result = await api(`/api/cases/${state.detail.case.id}/credential/verify`);
    $("#verify-result").innerHTML = `<div class="verify-ok">${result.valid ? "✓" : "×"} ${escapeHTML(result.message)}</div>${(result.checks || []).map(check => `<p>${check.passed ? "✓" : "×"} ${escapeHTML(check.code)}：${escapeHTML(check.message)}</p>`).join("")}<p class="meta">日志记录 ${result.recordCount} · 凭据锚点 #${result.anchoredSequence}</p>`;
  } catch (error) { notify(error.message, true); }
});

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
}
function escapeAttr(value) { return escapeHTML(value).replace(/`/g, "&#96;"); }

loadCases().catch(error => notify(error.message, true));
