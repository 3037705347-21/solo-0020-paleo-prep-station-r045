package station

import (
	"context"
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// RegisterEvidence 把一条证据登记到标本、方案或某个进度序号上。
func (s *Service) RegisterEvidence(
	ctx context.Context,
	request contracts.RegisterEvidenceRequest,
) (domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.EvidenceRecord{}, err
	}

	target := domain.EvidenceTarget{
		Kind:             domain.EvidenceTargetKind(strings.TrimSpace(request.TargetKind)),
		SpecimenID:       strings.TrimSpace(request.SpecimenID),
		PlanID:           strings.TrimSpace(request.PlanID),
		ProgressSequence: request.ProgressSequence,
	}

	var created domain.EvidenceRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		if err := resolveEvidenceTarget(m, target); err != nil {
			return err
		}
		record, err := domain.NewEvidenceRecord(
			s.nextID("evidence"),
			target,
			domain.EvidenceKind(strings.TrimSpace(request.Kind)),
			request.Summary,
			request.ContentDigest,
			request.SubmittedBy,
			s.now(),
		)
		if err != nil {
			return err
		}
		// 同一关联坐标内，内容校验或摘要重复都给出确定的冲突结果，
		// 互斥事务保证并发提交时恰好一方成功、其余方拿到 409。
		for _, existing := range m.EvidenceForTarget(target) {
			if existing.ContentDigest == record.ContentDigest {
				return contracts.Conflict(
					"duplicate_evidence_digest",
					"同一关联坐标已存在相同内容校验的证据："+existing.ID,
				)
			}
			if existing.Summary == record.Summary {
				return contracts.Conflict(
					"duplicate_evidence_summary",
					"同一关联坐标已存在相同摘要的证据："+existing.ID,
				)
			}
		}
		m.PutEvidence(record)
		created = record
		return nil
	})
	return created, err
}

// resolveEvidenceTarget 在同一事务内把关联坐标解析为真实对象，
// 未知对象返回 404，标本/方案/进度序号错误关联返回 400。
func resolveEvidenceTarget(m *repository.Mutation, target domain.EvidenceTarget) error {
	switch target.Kind {
	case domain.EvidenceTargetSpecimen:
		if _, ok := m.GetSpecimen(target.SpecimenID); !ok {
			return notFoundSpecimen(target.SpecimenID)
		}
		return nil
	case domain.EvidenceTargetPlan, domain.EvidenceTargetProgress:
		// 继续在下面统一校验方案归属。
	default:
		return contracts.Invalid("invalid_evidence_target", "证据关联对象类型不被支持")
	}

	plan, ok := m.GetPlan(target.PlanID)
	if !ok {
		return notFoundPlan(target.PlanID)
	}
	if plan.SpecimenID != target.SpecimenID {
		return contracts.Invalid(
			"evidence_target_mismatch",
			"方案不属于所声明的标本，证据关联坐标错误",
		)
	}
	if target.Kind == domain.EvidenceTargetPlan {
		return nil
	}
	progress := m.ProgressForPlan(target.PlanID)
	if target.ProgressSequence <= 0 || target.ProgressSequence > len(progress) {
		return contracts.NotFound(
			"progress_sequence_not_found",
			"进度序号超出该方案已登记的进度范围",
		)
	}
	if progress[target.ProgressSequence-1].Sequence != target.ProgressSequence {
		return contracts.NotFound(
			"progress_sequence_not_found",
			"进度序号与已登记的进度不连续",
		)
	}
	return nil
}

func (s *Service) GetEvidence(ctx context.Context, evidenceID string) (domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.EvidenceRecord{}, err
	}
	var value domain.EvidenceRecord
	err := s.repo.Read(func(m *repository.Mutation) error {
		found, ok := m.GetEvidence(evidenceID)
		if !ok {
			return notFoundEvidence(evidenceID)
		}
		value = found
		return nil
	})
	return value, err
}

func (s *Service) ListEvidenceForSpecimen(
	ctx context.Context,
	specimenID string,
) ([]domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.EvidenceRecord
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		values = m.EvidenceForSpecimen(specimenID)
		return nil
	})
	return values, err
}

func (s *Service) ListEvidenceForPlan(
	ctx context.Context,
	planID string,
) ([]domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.EvidenceRecord
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetPlan(planID); !ok {
			return notFoundPlan(planID)
		}
		// 方案视图固定收集方案级与全部进度序号上的证据，顺序由仓储统一确定。
		values = m.EvidenceForPlanCompletion(planID)
		return nil
	})
	return values, err
}
