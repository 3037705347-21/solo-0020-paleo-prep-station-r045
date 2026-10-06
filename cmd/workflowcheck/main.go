package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

type evidenceView struct {
	ID               string `json:"id"`
	TargetKind       string `json:"target_kind"`
	Kind             string `json:"kind"`
	Digest           string `json:"digest"`
	ProgressSequence int    `json:"progress_sequence"`
}

type sealView struct {
	ID             string         `json:"id"`
	Scope          string         `json:"scope"`
	ManifestDigest string         `json:"manifest_digest"`
	Entries        []evidenceView `json:"entries"`
}

type verificationView struct {
	EvidenceID string `json:"evidence_id"`
	Result     string `json:"result"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
	Evidence     []evidenceView `json:"evidence"`
	EvidenceSeal *sealView      `json:"evidence_seal"`
}

type releaseView struct {
	ID           string    `json:"id"`
	SpecimenID   string    `json:"specimen_id"`
	EvidenceSeal *sealView `json:"evidence_seal"`
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
		return checkEvidenceTrail(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
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

func registerEvidence(
	client *http.Client,
	url string,
	kind string,
	digest string,
	wantStatus int,
	target *evidenceView,
) error {
	var decoded any
	if target != nil {
		decoded = target
	}
	return requestJSON(
		client,
		http.MethodPost,
		url,
		contracts.RegisterEvidenceRequest{
			Kind:        kind,
			Digest:      digest,
			SubmittedBy: "preparator-chen",
		},
		wantStatus,
		decoded,
	)
}

func checkEvidenceTrail(client *http.Client, baseURL string) error {
	specimen, plan, err := completedPlanSetup(client, baseURL)
	if err != nil {
		return err
	}
	if err := recordProgress(client, baseURL, plan.ID, 40, "基质剥离"); err != nil {
		return err
	}

	digestBefore := strings.Repeat("a", 64)
	digestBatch := strings.Repeat("b", 64)
	digestMeasure := strings.Repeat("c", 64)

	var before evidenceView
	if err := registerEvidence(
		client,
		baseURL+"/specimens/"+specimen.ID+"/evidence",
		"photo_before",
		digestBefore,
		http.StatusCreated,
		&before,
	); err != nil {
		return err
	}
	if before.TargetKind != "specimen" || before.Digest != digestBefore {
		return errors.New("specimen evidence does not keep target and digest")
	}
	var batch evidenceView
	if err := registerEvidence(
		client,
		baseURL+"/plans/"+plan.ID+"/evidence",
		"material_batch",
		digestBatch,
		http.StatusCreated,
		&batch,
	); err != nil {
		return err
	}
	var measure evidenceView
	if err := registerEvidence(
		client,
		baseURL+"/plans/"+plan.ID+"/progress/1/evidence",
		"measurement",
		digestMeasure,
		http.StatusCreated,
		&measure,
	); err != nil {
		return err
	}
	if measure.TargetKind != "progress" || measure.ProgressSequence != 1 {
		return errors.New("progress evidence is not linked to sequence 1")
	}

	// Duplicate digest, unknown objects and wrong associations are deterministic.
	if err := registerEvidence(
		client,
		baseURL+"/specimens/"+specimen.ID+"/evidence",
		"photo_after",
		digestBefore,
		http.StatusConflict,
		nil,
	); err != nil {
		return err
	}
	if err := registerEvidence(
		client,
		baseURL+"/plans/plan-missing/evidence",
		"measurement",
		strings.Repeat("d", 64),
		http.StatusNotFound,
		nil,
	); err != nil {
		return err
	}
	if err := registerEvidence(
		client,
		baseURL+"/plans/"+plan.ID+"/progress/9/evidence",
		"measurement",
		strings.Repeat("d", 64),
		http.StatusNotFound,
		nil,
	); err != nil {
		return err
	}
	if err := registerEvidence(
		client,
		baseURL+"/plans/"+plan.ID+"/evidence",
		"xray_scan",
		strings.Repeat("d", 64),
		http.StatusBadRequest,
		nil,
	); err != nil {
		return err
	}

	// Completing the plan freezes plan-level and progress-level evidence.
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return err
	}
	var snapshot planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
		return err
	}
	if snapshot.EvidenceSeal == nil || snapshot.EvidenceSeal.Scope != "plan_completion" {
		return errors.New("completed plan has no plan_completion evidence seal")
	}
	if len(snapshot.EvidenceSeal.Entries) != 2 || len(snapshot.Evidence) != 2 {
		return errors.New("plan seal must freeze exactly the plan and progress evidence")
	}
	if snapshot.EvidenceSeal.ManifestDigest == "" {
		return errors.New("plan seal is missing a manifest digest")
	}
	if err := registerEvidence(
		client,
		baseURL+"/plans/"+plan.ID+"/evidence",
		"photo_after",
		strings.Repeat("e", 64),
		http.StatusConflict,
		nil,
	); err != nil {
		return err
	}
	if err := registerEvidence(
		client,
		baseURL+"/plans/"+plan.ID+"/progress/1/evidence",
		"photo_after",
		strings.Repeat("e", 64),
		http.StatusConflict,
		nil,
	); err != nil {
		return err
	}

	// Digest verification detects replaced files.
	var match verificationView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence/"+before.ID+"/verification",
		contracts.VerifyEvidenceRequest{Digest: digestBefore},
		http.StatusOK,
		&match,
	); err != nil {
		return err
	}
	if match.Result != "match" {
		return fmt.Errorf("expected matching digest, got %s", match.Result)
	}
	var mismatch verificationView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/evidence/"+before.ID+"/verification",
		contracts.VerifyEvidenceRequest{Digest: strings.Repeat("f", 64)},
		http.StatusOK,
		&mismatch,
	); err != nil {
		return err
	}
	if mismatch.Result != "mismatch" {
		return fmt.Errorf("expected mismatched digest, got %s", mismatch.Result)
	}

	// Release freezes every evidence relation of the specimen.
	var release releaseView
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
		&release,
	); err != nil {
		return err
	}
	if release.EvidenceSeal == nil || release.EvidenceSeal.Scope != "release" {
		return errors.New("release record has no release evidence seal")
	}
	if len(release.EvidenceSeal.Entries) != 3 {
		return errors.New("release seal must freeze specimen, plan and progress evidence")
	}
	var fetchedRelease releaseView
	if err := requestJSON(
		client,
		http.MethodGet,
		baseURL+"/specimens/"+specimen.ID+"/release",
		nil,
		http.StatusOK,
		&fetchedRelease,
	); err != nil {
		return err
	}
	if fetchedRelease.EvidenceSeal == nil ||
		fetchedRelease.EvidenceSeal.ManifestDigest != release.EvidenceSeal.ManifestDigest {
		return errors.New("release evidence relations are not fixed after the fact")
	}
	if err := registerEvidence(
		client,
		baseURL+"/specimens/"+specimen.ID+"/evidence",
		"photo_after",
		strings.Repeat("0", 64),
		http.StatusConflict,
		nil,
	); err != nil {
		return err
	}
	return nil
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
