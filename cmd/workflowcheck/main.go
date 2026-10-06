package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
	Evidence         []evidenceView `json:"evidence"`
	EvidenceFixation *fixationView  `json:"evidence_fixation,omitempty"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
}

type evidenceView struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Summary       string    `json:"summary"`
	ContentDigest string    `json:"content_digest"`
	SubmittedBy   string    `json:"submitted_by"`
	CreatedAt     time.Time `json:"created_at"`
	Target        struct {
		Kind             string `json:"kind"`
		SpecimenID       string `json:"specimen_id"`
		PlanID           string `json:"plan_id"`
		ProgressSequence int    `json:"progress_sequence"`
	} `json:"target"`
}

type fixationView struct {
	ID             string         `json:"id"`
	Scope          string         `json:"scope"`
	Records        []evidenceView `json:"records"`
	ManifestDigest string         `json:"manifest_digest"`
}

type releaseOutcome struct {
	Release          releaseView   `json:"release"`
	EvidenceFixation *fixationView `json:"evidence_fixation,omitempty"`
}

type evidenceList struct {
	Items []evidenceView `json:"items"`
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to check")
	flag.Parse()

	if err := run(*workflow); err != nil {
		fmt.Fprintln(os.Stderr, "workflow check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}

func run(workflow string) error {
	handler := httpapi.NewRouter(station.New(repository.NewMemory()))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	switch workflow {
	case "register-specimen":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		if specimen.Status != "registered" {
			return fmt.Errorf("expected registered status, got %s", specimen.Status)
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.ID != specimen.ID {
			return errors.New("registered specimen cannot be read back")
		}
		return nil

	case "approve-prep-plan":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		plan, err := createPlan(client, server.URL, specimen.ID)
		if err != nil {
			return err
		}
		var approved planView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/plans/"+plan.ID+"/approval",
			contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
			http.StatusOK,
			&approved,
		); err != nil {
			return err
		}
		if approved.Status != "approved" {
			return fmt.Errorf("expected approved plan, got %s", approved.Status)
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "preparing" {
			return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "log-preparation":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 35, "矩阵剥离"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var snapshot planSnapshot
		if err := requestJSON(client, http.MethodGet, server.URL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
			return err
		}
		if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 2 {
			return errors.New("plan did not complete with exactly two progress entries")
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "release-for-study":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var release releaseView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/specimens/"+specimen.ID+"/release",
			contracts.ReleaseRequest{
				Purpose:    "比较解剖研究",
				Recipient:  "地层研究组",
				ApprovedBy: "keeper-zhou",
			},
			http.StatusCreated,
			&release,
		); err != nil {
			return err
		}
		if release.SpecimenID != specimen.ID {
			return errors.New("release record references the wrong specimen")
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.Status != "available" {
			return fmt.Errorf("expected available specimen, got %s", fetched.Status)
		}
		return nil

	case "register-evidence":
		return checkRegisterEvidence(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func checkRegisterEvidence(client *http.Client, baseURL string) error {
	specimen, plan, err := completedPlanSetup(client, baseURL)
	if err != nil {
		return err
	}

	// 标本级证据（材料批次凭证）在制备开始前登记。
	materialDigest := digestOf("material-batch-A-07")
	var material evidenceView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:    "specimen",
			SpecimenID:    specimen.ID,
			Kind:          "material_certificate",
			Summary:       "可逆树脂批次 A-07 凭证",
			ContentDigest: materialDigest,
			SubmittedBy:   "keeper-zhou",
		},
		http.StatusCreated,
		&material,
	); err != nil {
		return err
	}

	// 未知对象：标本不存在返回 404。
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:    "specimen",
			SpecimenID:    "specimen-unknown",
			Kind:          "photo",
			Summary:       "不应成功",
			ContentDigest: digestOf("x"),
			SubmittedBy:   "keeper-zhou",
		},
		http.StatusNotFound,
		nil,
	); err != nil {
		return err
	}

	// 错误关联：方案不属于声明标本返回 400。
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:    "plan",
			SpecimenID:    specimen.ID,
			PlanID:        "plan-unknown",
			Kind:          "photo",
			Summary:       "不应成功",
			ContentDigest: digestOf("y"),
			SubmittedBy:   "keeper-zhou",
		},
		http.StatusNotFound,
		nil,
	); err != nil {
		return err
	}

	otherSpecimen, err := registerSpecimenCatalog(client, baseURL, "PPS-2026-002")
	if err != nil {
		return err
	}
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:    "plan",
			SpecimenID:    otherSpecimen.ID,
			PlanID:        plan.ID,
			Kind:          "photo",
			Summary:       "方案不属于该标本",
			ContentDigest: digestOf("z"),
			SubmittedBy:   "keeper-zhou",
		},
		http.StatusBadRequest,
		nil,
	); err != nil {
		return err
	}

	// 进度序号尚不存在返回 404（含非正序号，不能造成服务端错误）。
	for _, sequence := range []int{0, -1, 1} {
		if err := requestJSON(
			client,
			http.MethodPost,
			baseURL+"/evidence",
			contracts.RegisterEvidenceRequest{
				TargetKind:       "progress",
				SpecimenID:       specimen.ID,
				PlanID:           plan.ID,
				ProgressSequence: sequence,
				Kind:             "measurement",
				Summary:          "序号尚未产生",
				ContentDigest:    digestOf(fmt.Sprintf("p-%d", sequence)),
				SubmittedBy:      "preparator-chen",
			},
			http.StatusNotFound,
			nil,
		); err != nil {
			return err
		}
	}

	// 非法内容校验返回 400。
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:    "specimen",
			SpecimenID:    specimen.ID,
			Kind:          "photo",
			Summary:       "坏摘要",
			ContentDigest: "not-a-sha256",
			SubmittedBy:   "keeper-zhou",
		},
		http.StatusBadRequest,
		nil,
	); err != nil {
		return err
	}

	// 写入第一条进度，然后登记该序号的测量记录。
	if err := recordProgress(client, baseURL, plan.ID, 35, "矩阵剥离"); err != nil {
		return err
	}
	var measurement evidenceView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:       "progress",
			SpecimenID:       specimen.ID,
			PlanID:           plan.ID,
			ProgressSequence: 1,
			Kind:             "measurement",
			Summary:          "剥离面实测暴露比例 35%",
			ContentDigest:    digestOf("measure-seq-1"),
			SubmittedBy:      "preparator-chen",
		},
		http.StatusCreated,
		&measurement,
	); err != nil {
		return err
	}

	// 重复摘要（同坐标、同摘要、不同校验）返回 409。
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:       "progress",
			SpecimenID:       specimen.ID,
			PlanID:           plan.ID,
			ProgressSequence: 1,
			Kind:             "photo",
			Summary:          "剥离面实测暴露比例 35%",
			ContentDigest:    digestOf("measure-seq-1-dup-summary"),
			SubmittedBy:      "preparator-chen",
		},
		http.StatusConflict,
		nil,
	); err != nil {
		return err
	}

	// 并发提交完全相同的证据：恰有一条 201，其余 409。
	concurrentRequest := contracts.RegisterEvidenceRequest{
		TargetKind:    "plan",
		SpecimenID:    specimen.ID,
		PlanID:        plan.ID,
		Kind:          "photo",
		Summary:       "并发方案照片",
		ContentDigest: digestOf("concurrent-plan-photo"),
		SubmittedBy:   "preparator-chen",
	}
	const concurrency = 8
	statuses := make([]int, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			statuses[index] = postStatus(client, baseURL+"/evidence", concurrentRequest)
		}(i)
	}
	wg.Wait()
	var createdCount, conflictCount int
	for _, status := range statuses {
		switch status {
		case http.StatusCreated:
			createdCount++
		case http.StatusConflict:
			conflictCount++
		default:
			return fmt.Errorf("concurrent evidence submit returned unexpected status %d", status)
		}
	}
	if createdCount != 1 || conflictCount != concurrency-1 {
		return fmt.Errorf("concurrent evidence race got %d created and %d conflicts", createdCount, conflictCount)
	}

	// 完成前方案证据视图应包含方案级与进度级证据。
	var beforeSnapshot planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &beforeSnapshot); err != nil {
		return err
	}
	if len(beforeSnapshot.Evidence) != 2 {
		return fmt.Errorf("expected 2 plan-scoped evidences before completion, got %d", len(beforeSnapshot.Evidence))
	}
	if beforeSnapshot.EvidenceFixation != nil {
		return errors.New("plan must not carry an evidence fixation before completion")
	}

	// 完成方案（第二条进度到 100），同一提交固定证据关系。
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return err
	}
	var completed planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &completed); err != nil {
		return err
	}
	if completed.Plan.Status != "completed" {
		return fmt.Errorf("expected completed plan, got %s", completed.Plan.Status)
	}
	if completed.EvidenceFixation == nil {
		return errors.New("plan completion must fix the evidence relation")
	}
	if completed.EvidenceFixation.Scope != "plan_completion" {
		return fmt.Errorf("unexpected fixation scope %s", completed.EvidenceFixation.Scope)
	}
	if len(completed.EvidenceFixation.Records) != 2 {
		return fmt.Errorf("expected 2 fixed records at completion, got %d", len(completed.EvidenceFixation.Records))
	}
	completionDigest := completed.EvidenceFixation.ManifestDigest
	recomputed := digestManifest(completed.EvidenceFixation.Records)
	if completionDigest != recomputed {
		return fmt.Errorf("completion manifest digest mismatch: %s vs %s", completionDigest, recomputed)
	}

	// 完成后再登记方案级证据：不得静默改变已固定的历史清单。
	var late evidenceView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence",
		contracts.RegisterEvidenceRequest{
			TargetKind:    "plan",
			SpecimenID:    specimen.ID,
			PlanID:        plan.ID,
			Kind:          "material_certificate",
			Summary:       "完成后补充的批次凭证",
			ContentDigest: digestOf("late-plan-evidence"),
			SubmittedBy:   "keeper-zhou",
		},
		http.StatusCreated,
		&late,
	); err != nil {
		return err
	}
	var afterLate planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &afterLate); err != nil {
		return err
	}
	if len(afterLate.Evidence) != 3 {
		return fmt.Errorf("live evidence view should now contain 3 records, got %d", len(afterLate.Evidence))
	}
	if afterLate.EvidenceFixation == nil ||
		len(afterLate.EvidenceFixation.Records) != 2 ||
		afterLate.EvidenceFixation.ManifestDigest != completionDigest {
		return errors.New("completion fixation must stay immutable after later evidence registration")
	}

	// 证据可独立读取，且标本级证据列表返回最早登记的批次凭证。
	var fetched evidenceView
	if err := requestJSON(client, http.MethodGet, baseURL+"/evidence/"+material.ID, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.ContentDigest != materialDigest {
		return errors.New("evidence read back with a changed content digest")
	}
	var specimenEvidence evidenceList
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID+"/evidence", nil, http.StatusOK, &specimenEvidence); err != nil {
		return err
	}
	if len(specimenEvidence.Items) != 1 || specimenEvidence.Items[0].ID != material.ID {
		return errors.New("specimen evidence list did not return the batch certificate")
	}

	// 办理放行：固定范围覆盖标本、方案和全部进度证据，且与完成固定相互独立。
	var outcome releaseOutcome
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/release",
		contracts.ReleaseRequest{
			Purpose:    "比较解剖研究",
			Recipient:  "地层研究组",
			ApprovedBy: "keeper-zhou",
		},
		http.StatusCreated,
		nil,
	); err != nil {
		return err
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID+"/release", nil, http.StatusOK, &outcome); err != nil {
		return err
	}
	if outcome.EvidenceFixation == nil {
		return errors.New("release must fix the evidence relation")
	}
	if outcome.EvidenceFixation.Scope != "release" {
		return fmt.Errorf("unexpected release fixation scope %s", outcome.EvidenceFixation.Scope)
	}
	if len(outcome.EvidenceFixation.Records) != 4 {
		return fmt.Errorf("expected 4 fixed records at release, got %d", len(outcome.EvidenceFixation.Records))
	}
	if outcome.EvidenceFixation.ManifestDigest == completionDigest {
		return errors.New("release fixation must differ from completion fixation")
	}

	// 已固定的完成清单在放行后仍保持原值，历史不被静默改变。
	var finalPlan planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &finalPlan); err != nil {
		return err
	}
	if finalPlan.EvidenceFixation == nil ||
		finalPlan.EvidenceFixation.ManifestDigest != completionDigest {
		return errors.New("completion fixation changed after release")
	}
	return nil
}

func registerSpecimenCatalog(client *http.Client, baseURL, catalog string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: catalog,
			FieldLabel:    "北坡-08",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func digestOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// digestManifest 按与服务端完全一致的规范格式重算固定清单摘要，
// 用于证明返回的 manifest_digest 确实覆盖每条证据的全部字段。
func digestManifest(records []evidenceView) string {
	var builder bytes.Buffer
	for _, record := range records {
		fields := []string{
			record.ID,
			record.Target.Kind,
			record.Target.SpecimenID,
			record.Target.PlanID,
			strconv.Itoa(record.Target.ProgressSequence),
			record.Kind,
			record.Summary,
			record.ContentDigest,
			record.SubmittedBy,
			record.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		builder.WriteString(strings.Join(fields, "\t"))
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256(builder.Bytes())
	return hex.EncodeToString(sum[:])
}

func postStatus(client *http.Client, url string, body any) int {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return 0
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return response.StatusCode
}

func registerSpecimen(client *http.Client, baseURL string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: "PPS-2026-001",
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func createPlan(client *http.Client, baseURL, specimenID string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
		},
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func completedPlanSetup(client *http.Client, baseURL string) (specimenView, planView, error) {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		&approved,
	); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		contracts.ProgressRequest{
			Stage:      stage,
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: "preparator-chen",
		},
		http.StatusCreated,
		&ignored,
	)
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}
