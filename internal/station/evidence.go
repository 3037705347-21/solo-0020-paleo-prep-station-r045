package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// ensureEvidenceOpen rejects registrations that would alter evidence relations
// already frozen by a plan-completion or release seal.
func ensureEvidenceOpen(m *repository.Mutation, specimenID string, planID string) error {
	if _, sealed := m.ReleaseEvidenceSeal(specimenID); sealed {
		return contracts.Conflict("evidence_relation_sealed", "放行完成后证据关系已固定，不能再登记证据")
	}
	if planID != "" {
		if _, sealed := m.PlanEvidenceSeal(planID); sealed {
			return contracts.Conflict("evidence_relation_sealed", "方案完成后证据关系已固定，不能再登记证据")
		}
	}
	return nil
}

func putEvidenceIfNew(m *repository.Mutation, record domain.EvidenceRecord) error {
	if _, exists := m.FindEvidenceByDigest(record.Digest); exists {
		return contracts.Conflict("duplicate_evidence_digest", "相同内容摘要的证据已登记，不能重复提交")
	}
	m.PutEvidence(record)
	return nil
}

func (s *Service) RegisterSpecimenEvidence(
	ctx context.Context,
	specimenID string,
	request contracts.RegisterEvidenceRequest,
) (domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.EvidenceRecord{}, err
	}

	var created domain.EvidenceRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}
		if err := ensureEvidenceOpen(m, specimen.ID, ""); err != nil {
			return err
		}
		record, err := domain.NewEvidenceRecord(
			s.nextID("evidence"),
			domain.EvidenceTarget{Kind: domain.TargetSpecimen, SpecimenID: specimen.ID},
			request.Kind,
			request.Digest,
			request.SubmittedBy,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := putEvidenceIfNew(m, record); err != nil {
			return err
		}
		created = record
		return nil
	})
	return created, err
}

func (s *Service) RegisterPlanEvidence(
	ctx context.Context,
	planID string,
	request contracts.RegisterEvidenceRequest,
) (domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.EvidenceRecord{}, err
	}

	var created domain.EvidenceRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		if err := ensureEvidenceOpen(m, plan.SpecimenID, plan.ID); err != nil {
			return err
		}
		if plan.Status == domain.PlanCancelled {
			return contracts.Conflict("plan_not_accepting_evidence", "已取消的方案不能登记证据")
		}
		record, err := domain.NewEvidenceRecord(
			s.nextID("evidence"),
			domain.EvidenceTarget{Kind: domain.TargetPlan, SpecimenID: plan.SpecimenID, PlanID: plan.ID},
			request.Kind,
			request.Digest,
			request.SubmittedBy,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := putEvidenceIfNew(m, record); err != nil {
			return err
		}
		created = record
		return nil
	})
	return created, err
}

func (s *Service) RegisterProgressEvidence(
	ctx context.Context,
	planID string,
	sequence int,
	request contracts.RegisterEvidenceRequest,
) (domain.EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.EvidenceRecord{}, err
	}

	var created domain.EvidenceRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		found := false
		for _, entry := range m.ProgressForPlan(planID) {
			if entry.Sequence == sequence {
				found = true
				break
			}
		}
		if !found {
			return notFoundProgress(planID, sequence)
		}
		if err := ensureEvidenceOpen(m, plan.SpecimenID, plan.ID); err != nil {
			return err
		}
		record, err := domain.NewEvidenceRecord(
			s.nextID("evidence"),
			domain.EvidenceTarget{
				Kind:        domain.TargetProgress,
				SpecimenID:  plan.SpecimenID,
				PlanID:      plan.ID,
				ProgressSeq: sequence,
			},
			request.Kind,
			request.Digest,
			request.SubmittedBy,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := putEvidenceIfNew(m, record); err != nil {
			return err
		}
		created = record
		return nil
	})
	return created, err
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
		if seal, sealed := m.ReleaseEvidenceSeal(specimenID); sealed {
			values = append([]domain.EvidenceRecord(nil), seal.Entries...)
		}
		return nil
	})
	return values, err
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

func (s *Service) VerifyEvidence(
	ctx context.Context,
	evidenceID string,
	request contracts.VerifyEvidenceRequest,
) (domain.EvidenceVerification, error) {
	if err := ctx.Err(); err != nil {
		return domain.EvidenceVerification{}, err
	}
	presented, err := domain.NormalizeEvidenceDigest(request.Digest)
	if err != nil {
		return domain.EvidenceVerification{}, err
	}
	var result domain.EvidenceVerification
	err = s.repo.Read(func(m *repository.Mutation) error {
		record, ok := m.GetEvidence(evidenceID)
		if !ok {
			return notFoundEvidence(evidenceID)
		}
		result = record.Verify(presented)
		return nil
	})
	return result, err
}
