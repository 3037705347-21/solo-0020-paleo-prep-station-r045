package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type PlanStatus string

const (
	PlanDraft     PlanStatus = "draft"
	PlanApproved  PlanStatus = "approved"
	PlanActive    PlanStatus = "active"
	PlanCompleted PlanStatus = "completed"
	PlanCancelled PlanStatus = "cancelled"
)

type PrepPlan struct {
	ID             string     `json:"id"`
	SpecimenID     string     `json:"specimen_id"`
	Objective      string     `json:"objective"`
	Method         string     `json:"method"`
	Adhesive       string     `json:"adhesive"`
	TargetExposure int        `json:"target_exposure"`
	Status         PlanStatus `json:"status"`
	ApprovedBy     string     `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	Version        int        `json:"version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PlanConditions struct {
	Objective      string `json:"objective"`
	Method         string `json:"method"`
	Adhesive       string `json:"adhesive"`
	TargetExposure int    `json:"target_exposure"`
}

type PlanRevision struct {
	ID                string         `json:"id"`
	PlanID            string         `json:"plan_id"`
	Number            int            `json:"number"`
	EffectiveSequence int            `json:"effective_sequence"`
	Conditions        PlanConditions `json:"conditions"`
	Reason            string         `json:"reason,omitempty"`
	ChangedBy         string         `json:"changed_by,omitempty"`
	RequestID         string         `json:"request_id,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

func (p PrepPlan) Conditions() PlanConditions {
	return PlanConditions{
		Objective:      p.Objective,
		Method:         p.Method,
		Adhesive:       p.Adhesive,
		TargetExposure: p.TargetExposure,
	}
}

func (p *PrepPlan) ApplyRevision(conditions PlanConditions, now time.Time) error {
	if p.Status != PlanApproved && p.Status != PlanActive {
		return contracts.Conflict("plan_not_revisable", "当前方案状态不能调整制备条件")
	}
	p.Objective = conditions.Objective
	p.Method = conditions.Method
	p.Adhesive = conditions.Adhesive
	p.TargetExposure = conditions.TargetExposure
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

func NewInitialPlanRevision(id string, plan PrepPlan, now time.Time) (PlanRevision, error) {
	if id == "" {
		return PlanRevision{}, contracts.Internal("missing_generated_id", "方案版本标识未生成", nil)
	}
	if plan.Status != PlanDraft {
		return PlanRevision{}, contracts.Conflict("plan_not_revisable", "只有草稿方案可以建立初始版本")
	}
	return PlanRevision{
		ID:                id,
		PlanID:            plan.ID,
		Number:            1,
		EffectiveSequence: 1,
		Conditions:        plan.Conditions(),
		CreatedAt:         now.UTC(),
	}, nil
}

func NewPlanRevision(
	id string,
	plan PrepPlan,
	previous []PlanRevision,
	progressCount int,
	request contracts.RevisePlanRequest,
	now time.Time,
) (PlanRevision, error) {
	if id == "" {
		return PlanRevision{}, contracts.Internal("missing_generated_id", "方案版本标识未生成", nil)
	}
	if plan.Status != PlanApproved && plan.Status != PlanActive {
		return PlanRevision{}, contracts.Conflict("plan_not_revisable", "当前方案状态不能调整制备条件")
	}
	reason, err := requiredText("调整原因", request.Reason, 300)
	if err != nil {
		return PlanRevision{}, err
	}
	changedBy, err := requiredText("调整人", request.ChangedBy, 80)
	if err != nil {
		return PlanRevision{}, err
	}
	requestID, err := requiredText("调整请求标识", request.RequestID, 120)
	if err != nil {
		return PlanRevision{}, err
	}
	objective, err := requiredText("修复目标", request.Objective, 200)
	if err != nil {
		return PlanRevision{}, err
	}
	method, err := requiredText("修复方法", request.Method, 160)
	if err != nil {
		return PlanRevision{}, err
	}
	adhesive, err := optionalText("黏结材料", request.Adhesive, 120)
	if err != nil {
		return PlanRevision{}, err
	}
	if err := validatePercent(request.TargetExposure); err != nil {
		return PlanRevision{}, err
	}
	if request.TargetExposure == 0 {
		return PlanRevision{}, contracts.Invalid("invalid_target_exposure", "目标暴露比例必须大于零")
	}
	nextNumber := 1
	if len(previous) > 0 {
		nextNumber = previous[len(previous)-1].Number + 1
	}
	return PlanRevision{
		ID:                id,
		PlanID:            plan.ID,
		Number:            nextNumber,
		EffectiveSequence: plan.Version + 1,
		Conditions: PlanConditions{
			Objective:      objective,
			Method:         method,
			Adhesive:       adhesive,
			TargetExposure: request.TargetExposure,
		},
		Reason:    reason,
		ChangedBy: changedBy,
		RequestID: requestID,
		CreatedAt: now.UTC(),
	}, nil
}

func NewPrepPlan(
	id string,
	specimen Specimen,
	objective string,
	method string,
	adhesive string,
	targetExposure int,
	now time.Time,
) (PrepPlan, error) {
	if id == "" {
		return PrepPlan{}, contracts.Internal("missing_generated_id", "方案标识未生成", nil)
	}
	if !specimen.CanCreatePlan() {
		return PrepPlan{}, contracts.Conflict("specimen_not_plannable", "当前标本状态不能创建修复方案")
	}
	goal, err := requiredText("修复目标", objective, 200)
	if err != nil {
		return PrepPlan{}, err
	}
	approach, err := requiredText("修复方法", method, 160)
	if err != nil {
		return PrepPlan{}, err
	}
	adhesiveValue, err := optionalText("黏结材料", adhesive, 120)
	if err != nil {
		return PrepPlan{}, err
	}
	if err := validatePercent(targetExposure); err != nil {
		return PrepPlan{}, contracts.Invalid("invalid_target_exposure", "目标暴露比例必须位于 0 到 100 之间")
	}
	if targetExposure == 0 {
		return PrepPlan{}, contracts.Invalid("invalid_target_exposure", "目标暴露比例必须大于零")
	}
	return PrepPlan{
		ID:             id,
		SpecimenID:     specimen.ID,
		Objective:      goal,
		Method:         approach,
		Adhesive:       adhesiveValue,
		TargetExposure: targetExposure,
		Status:         PlanDraft,
		Version:        1,
		CreatedAt:      now.UTC(),
		UpdatedAt:      now.UTC(),
	}, nil
}

func (p PrepPlan) IsOpen() bool {
	return p.Status == PlanDraft || p.Status == PlanApproved || p.Status == PlanActive
}

func (p PrepPlan) IsCompleted() bool {
	return p.Status == PlanCompleted
}

func (p *PrepPlan) Approve(approvedBy string, now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_approvable", "只有草稿方案可以审批")
	}
	approver, err := requiredText("审批人", approvedBy, 80)
	if err != nil {
		return err
	}
	stamp := now.UTC()
	p.Status = PlanApproved
	p.ApprovedBy = approver
	p.ApprovedAt = &stamp
	p.Version++
	p.UpdatedAt = stamp
	return nil
}

func (p *PrepPlan) Begin(now time.Time) error {
	if p.Status == PlanActive {
		return nil
	}
	if p.Status != PlanApproved {
		return contracts.Conflict("plan_not_active", "只有已审批方案可以开始制备")
	}
	p.Status = PlanActive
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PrepPlan) Complete(now time.Time) error {
	if p.Status == PlanCompleted {
		return nil
	}
	if p.Status != PlanActive {
		return contracts.Conflict("plan_not_completable", "只有进行中的方案可以完成")
	}
	p.Status = PlanCompleted
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PrepPlan) Cancel(now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_cancellable", "只有草稿方案可以取消")
	}
	p.Status = PlanCancelled
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}
