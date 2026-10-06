package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type EvidenceKind string

const (
	EvidencePhotoBefore   EvidenceKind = "photo_before"
	EvidencePhotoAfter    EvidenceKind = "photo_after"
	EvidenceMeasurement   EvidenceKind = "measurement"
	EvidenceMaterialBatch EvidenceKind = "material_batch"
)

type EvidenceTargetKind string

const (
	TargetSpecimen EvidenceTargetKind = "specimen"
	TargetPlan     EvidenceTargetKind = "plan"
	TargetProgress EvidenceTargetKind = "progress"
)

// EvidenceTarget describes the object an evidence record is attached to.
// The service layer resolves it from existing specimens, plans and progress
// entries before constructing a record.
type EvidenceTarget struct {
	Kind        EvidenceTargetKind
	SpecimenID  string
	PlanID      string
	ProgressSeq int
}

type EvidenceRecord struct {
	ID          string             `json:"id"`
	TargetKind  EvidenceTargetKind `json:"target_kind"`
	SpecimenID  string             `json:"specimen_id"`
	PlanID      string             `json:"plan_id,omitempty"`
	ProgressSeq int                `json:"progress_sequence,omitempty"`
	Kind        EvidenceKind       `json:"kind"`
	Digest      string             `json:"digest"`
	SubmittedBy string             `json:"submitted_by"`
	CreatedAt   time.Time          `json:"created_at"`
}

var evidenceDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// NormalizeEvidenceDigest validates the content digest used to detect file
// replacement. Digests are lowercase hex SHA-256 values.
func NormalizeEvidenceDigest(value string) (string, error) {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	if !evidenceDigestPattern.MatchString(cleaned) {
		return "", contracts.Invalid("invalid_evidence_digest", "内容摘要必须是 64 位十六进制字符（SHA-256）")
	}
	return cleaned, nil
}

func parseEvidenceKind(value string) (EvidenceKind, error) {
	switch kind := EvidenceKind(strings.TrimSpace(value)); kind {
	case EvidencePhotoBefore, EvidencePhotoAfter, EvidenceMeasurement, EvidenceMaterialBatch:
		return kind, nil
	}
	return "", contracts.Invalid(
		"invalid_evidence_kind",
		"证据类型必须是 photo_before、photo_after、measurement 或 material_batch",
	)
}

func validateEvidenceTarget(target EvidenceTarget) error {
	if target.SpecimenID == "" {
		return contracts.Internal("invalid_evidence_target", "证据缺少关联标本", nil)
	}
	switch target.Kind {
	case TargetSpecimen:
		if target.PlanID != "" || target.ProgressSeq != 0 {
			return contracts.Internal("invalid_evidence_target", "标本级证据不能关联方案或进度", nil)
		}
	case TargetPlan:
		if target.PlanID == "" || target.ProgressSeq != 0 {
			return contracts.Internal("invalid_evidence_target", "方案级证据关联不完整", nil)
		}
	case TargetProgress:
		if target.PlanID == "" || target.ProgressSeq <= 0 {
			return contracts.Internal("invalid_evidence_target", "进度级证据关联不完整", nil)
		}
	default:
		return contracts.Internal("invalid_evidence_target", "证据关联对象类型未知", nil)
	}
	return nil
}

func NewEvidenceRecord(
	id string,
	target EvidenceTarget,
	kind string,
	digest string,
	submittedBy string,
	now time.Time,
) (EvidenceRecord, error) {
	if id == "" {
		return EvidenceRecord{}, contracts.Internal("missing_generated_id", "证据标识未生成", nil)
	}
	if err := validateEvidenceTarget(target); err != nil {
		return EvidenceRecord{}, err
	}
	parsedKind, err := parseEvidenceKind(kind)
	if err != nil {
		return EvidenceRecord{}, err
	}
	normalized, err := NormalizeEvidenceDigest(digest)
	if err != nil {
		return EvidenceRecord{}, err
	}
	submitter, err := requiredText("提交人", submittedBy, 80)
	if err != nil {
		return EvidenceRecord{}, err
	}
	return EvidenceRecord{
		ID:          id,
		TargetKind:  target.Kind,
		SpecimenID:  target.SpecimenID,
		PlanID:      target.PlanID,
		ProgressSeq: target.ProgressSeq,
		Kind:        parsedKind,
		Digest:      normalized,
		SubmittedBy: submitter,
		CreatedAt:   now.UTC(),
	}, nil
}

const (
	VerificationMatch    = "match"
	VerificationMismatch = "mismatch"
)

// EvidenceVerification reports whether a presented digest still matches the
// digest registered for the evidence, exposing silent file replacement.
type EvidenceVerification struct {
	EvidenceID       string `json:"evidence_id"`
	RegisteredDigest string `json:"registered_digest"`
	PresentedDigest  string `json:"presented_digest"`
	Result           string `json:"result"`
}

func (e EvidenceRecord) Verify(presentedDigest string) EvidenceVerification {
	result := VerificationMismatch
	if e.Digest == presentedDigest {
		result = VerificationMatch
	}
	return EvidenceVerification{
		EvidenceID:       e.ID,
		RegisteredDigest: e.Digest,
		PresentedDigest:  presentedDigest,
		Result:           result,
	}
}

type EvidenceSealScope string

const (
	SealPlanCompletion EvidenceSealScope = "plan_completion"
	SealRelease        EvidenceSealScope = "release"
)

// EvidenceSeal freezes the evidence relations read when a plan completes or a
// release is granted. Seals are append-only: later registrations, deletions or
// replacements cannot rewrite the frozen manifest.
type EvidenceSeal struct {
	ID             string            `json:"id"`
	Scope          EvidenceSealScope `json:"scope"`
	SpecimenID     string            `json:"specimen_id"`
	PlanID         string            `json:"plan_id,omitempty"`
	Entries        []EvidenceRecord  `json:"entries"`
	ManifestDigest string            `json:"manifest_digest"`
	CreatedAt      time.Time         `json:"created_at"`
}

func NewEvidenceSeal(
	id string,
	scope EvidenceSealScope,
	specimenID string,
	planID string,
	entries []EvidenceRecord,
	now time.Time,
) (EvidenceSeal, error) {
	if id == "" {
		return EvidenceSeal{}, contracts.Internal("missing_generated_id", "证据封存标识未生成", nil)
	}
	if specimenID == "" {
		return EvidenceSeal{}, contracts.Internal("invalid_evidence_seal", "证据封存缺少关联标本", nil)
	}
	switch scope {
	case SealPlanCompletion:
		if planID == "" {
			return EvidenceSeal{}, contracts.Internal("invalid_evidence_seal", "方案完成封存缺少关联方案", nil)
		}
	case SealRelease:
		if planID != "" {
			return EvidenceSeal{}, contracts.Internal("invalid_evidence_seal", "放行封存不能关联单个方案", nil)
		}
	default:
		return EvidenceSeal{}, contracts.Internal("invalid_evidence_seal", "证据封存范围未知", nil)
	}
	ordered := append([]EvidenceRecord(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	return EvidenceSeal{
		ID:             id,
		Scope:          scope,
		SpecimenID:     specimenID,
		PlanID:         planID,
		Entries:        ordered,
		ManifestDigest: evidenceManifestDigest(ordered),
		CreatedAt:      now.UTC(),
	}, nil
}

func evidenceManifestDigest(entries []EvidenceRecord) string {
	var builder strings.Builder
	for _, entry := range entries {
		builder.WriteString(entry.ID)
		builder.WriteByte(':')
		builder.WriteString(entry.Digest)
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}
