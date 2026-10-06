package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// EvidenceKind 是受控的证据类型，只允许照片、测量记录和材料批次凭证等有限取值。
type EvidenceKind string

const (
	EvidencePhoto               EvidenceKind = "photo"
	EvidenceMeasurement         EvidenceKind = "measurement"
	EvidenceMaterialCertificate EvidenceKind = "material_certificate"
	EvidenceNote                EvidenceKind = "note"
)

func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidencePhoto, EvidenceMeasurement, EvidenceMaterialCertificate, EvidenceNote:
		return true
	default:
		return false
	}
}

// EvidenceTargetKind 说明证据关联到哪一类对象。
type EvidenceTargetKind string

const (
	EvidenceTargetSpecimen EvidenceTargetKind = "specimen"
	EvidenceTargetPlan     EvidenceTargetKind = "plan"
	EvidenceTargetProgress EvidenceTargetKind = "progress"
)

func (k EvidenceTargetKind) Valid() bool {
	switch k {
	case EvidenceTargetSpecimen, EvidenceTargetPlan, EvidenceTargetProgress:
		return true
	default:
		return false
	}
}

// EvidenceTarget 是证据关联目标的稳定坐标。进度类型必须同时给出方案标识和进度序号。
type EvidenceTarget struct {
	Kind             EvidenceTargetKind `json:"kind"`
	SpecimenID       string             `json:"specimen_id"`
	PlanID           string             `json:"plan_id,omitempty"`
	ProgressSequence int                `json:"progress_sequence,omitempty"`
}

// EvidenceRecord 是只追加的证据登记，任何完成或放行动作只能读取它，不能改写或删除。
type EvidenceRecord struct {
	ID            string         `json:"id"`
	Target        EvidenceTarget `json:"target"`
	Kind          EvidenceKind   `json:"kind"`
	Summary       string         `json:"summary"`
	ContentDigest string         `json:"content_digest"`
	SubmittedBy   string         `json:"submitted_by"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ManifestLine 是固定清单中每条证据的规范表示，字段顺序和分隔符不得调整。
func (e EvidenceRecord) ManifestLine() string {
	return strings.Join([]string{
		e.ID,
		string(e.Target.Kind),
		e.Target.SpecimenID,
		e.Target.PlanID,
		strconv.Itoa(e.Target.ProgressSequence),
		string(e.Kind),
		e.Summary,
		e.ContentDigest,
		e.SubmittedBy,
		e.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, "\t")
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// NewEvidenceRecord 只做字段级不变量校验；目标对象是否存在、是否错误关联由编排层判定。
func NewEvidenceRecord(
	id string,
	target EvidenceTarget,
	kind EvidenceKind,
	summary string,
	contentDigest string,
	submittedBy string,
	now time.Time,
) (EvidenceRecord, error) {
	if id == "" {
		return EvidenceRecord{}, contracts.Internal("missing_generated_id", "证据标识未生成", nil)
	}
	if !target.Kind.Valid() {
		return EvidenceRecord{}, contracts.Invalid("invalid_evidence_target", "证据关联对象类型不被支持")
	}
	if target.SpecimenID == "" {
		return EvidenceRecord{}, contracts.Invalid("invalid_evidence_target", "证据必须关联到具体标本")
	}
	switch target.Kind {
	case EvidenceTargetSpecimen:
		if target.PlanID != "" || target.ProgressSequence != 0 {
			return EvidenceRecord{}, contracts.Invalid("invalid_evidence_target", "标本级证据不能携带方案或进度坐标")
		}
	case EvidenceTargetPlan:
		if target.PlanID == "" || target.ProgressSequence != 0 {
			return EvidenceRecord{}, contracts.Invalid("invalid_evidence_target", "方案级证据必须且只能携带方案标识")
		}
	case EvidenceTargetProgress:
		if target.PlanID == "" || target.ProgressSequence <= 0 {
			return EvidenceRecord{}, contracts.Invalid("invalid_evidence_target", "进度级证据必须同时携带方案标识和正整数进度序号")
		}
	}
	if !kind.Valid() {
		return EvidenceRecord{}, contracts.Invalid("invalid_evidence_kind", "证据类型不被支持")
	}
	summaryValue, err := requiredText("内容摘要", summary, 300)
	if err != nil {
		return EvidenceRecord{}, err
	}
	digest := strings.ToLower(strings.TrimSpace(contentDigest))
	if !digestPattern.MatchString(digest) {
		return EvidenceRecord{}, contracts.Invalid("invalid_content_digest", "内容校验必须是 64 位十六进制 SHA-256 摘要")
	}
	submitter, err := requiredText("提交人", submittedBy, 80)
	if err != nil {
		return EvidenceRecord{}, err
	}
	return EvidenceRecord{
		ID:            id,
		Target:        target,
		Kind:          kind,
		Summary:       summaryValue,
		ContentDigest: digest,
		SubmittedBy:   submitter,
		CreatedAt:     now.UTC(),
	}, nil
}

// FixationScope 标识证据关系固定发生在方案完成还是标本放行时。
type FixationScope string

const (
	FixationPlanCompletion FixationScope = "plan_completion"
	FixationRelease        FixationScope = "release"
)

// EvidenceFixation 是方案完成或办理放行时对当时证据关系的不可变快照。
type EvidenceFixation struct {
	ID             string           `json:"id"`
	Scope          FixationScope    `json:"scope"`
	SpecimenID     string           `json:"specimen_id"`
	PlanID         string           `json:"plan_id,omitempty"`
	ReleaseID      string           `json:"release_id,omitempty"`
	Records        []EvidenceRecord `json:"records"`
	ManifestDigest string           `json:"manifest_digest"`
	CreatedAt      time.Time        `json:"created_at"`
}

// NewEvidenceFixation 在事务内复制当时命中的证据并计算清单摘要，之后证据表的变化不会影响它。
func NewEvidenceFixation(
	id string,
	scope FixationScope,
	specimenID string,
	planID string,
	releaseID string,
	records []EvidenceRecord,
	now time.Time,
) (EvidenceFixation, error) {
	if id == "" {
		return EvidenceFixation{}, contracts.Internal("missing_generated_id", "证据固定标识未生成", nil)
	}
	if scope != FixationPlanCompletion && scope != FixationRelease {
		return EvidenceFixation{}, contracts.Internal("invalid_fixation_scope", "证据固定场景不被支持", nil)
	}
	if specimenID == "" {
		return EvidenceFixation{}, contracts.Internal("invalid_fixation_target", "证据固定必须指向标本", nil)
	}
	if scope == FixationPlanCompletion && planID == "" {
		return EvidenceFixation{}, contracts.Internal("invalid_fixation_target", "方案完成固定必须携带方案标识", nil)
	}
	if scope == FixationRelease {
		if releaseID == "" {
			return EvidenceFixation{}, contracts.Internal("invalid_fixation_target", "放行固定必须携带放行记录标识", nil)
		}
		planID = ""
	}
	fixed := append([]EvidenceRecord(nil), records...)
	sortEvidenceRecords(fixed)
	return EvidenceFixation{
		ID:             id,
		Scope:          scope,
		SpecimenID:     specimenID,
		PlanID:         planID,
		ReleaseID:      releaseID,
		Records:        fixed,
		ManifestDigest: EvidenceManifestDigest(fixed),
		CreatedAt:      now.UTC(),
	}, nil
}

// EvidenceManifestDigest 对固定清单的规范表示求 SHA-256，任何字段被替换都会改变结果。
func EvidenceManifestDigest(records []EvidenceRecord) string {
	ordered := append([]EvidenceRecord(nil), records...)
	sortEvidenceRecords(ordered)
	var builder strings.Builder
	for _, record := range ordered {
		builder.WriteString(record.ManifestLine())
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

// sortEvidenceRecords 让固定清单与读取投影在任何插入顺序下都得到确定结果。
func sortEvidenceRecords(records []EvidenceRecord) {
	sort.Slice(records, func(i, j int) bool {
		if !records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].CreatedAt.Before(records[j].CreatedAt)
		}
		return records[i].ID < records[j].ID
	})
}
