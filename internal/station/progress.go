package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

func (s *Service) LogProgress(
	ctx context.Context,
	planID string,
	request contracts.ProgressRequest,
) (domain.ProgressEntry, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProgressEntry{}, err
	}

	var created domain.ProgressEntry
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		specimen, ok := m.GetSpecimen(plan.SpecimenID)
		if !ok {
			return notFoundSpecimen(plan.SpecimenID)
		}
		previous := m.ProgressForPlan(planID)
		revision, ok := m.RevisionForSequence(planID, len(previous)+1)
		if !ok {
			return contracts.Internal("plan_revision_missing", "方案缺少可用的条件版本", nil)
		}
		entry, err := domain.NewProgressEntry(
			s.nextID("progress"),
			plan,
			previous,
			revision,
			request.Stage,
			request.Percent,
			request.Note,
			request.RecordedBy,
			s.now(),
		)
		if err != nil {
			return err
		}
		if plan.Status == domain.PlanApproved {
			if err := plan.Begin(s.now()); err != nil {
				return err
			}
		}
		if request.Percent == 100 {
			if err := plan.Complete(s.now()); err != nil {
				return err
			}
			if err := specimen.MarkStabilized(s.now()); err != nil {
				return err
			}
			seal, err := domain.NewEvidenceSeal(
				s.nextID("seal"),
				domain.SealPlanCompletion,
				specimen.ID,
				plan.ID,
				m.EvidenceForPlan(planID),
				s.now(),
			)
			if err != nil {
				return err
			}
			m.PutPlanEvidenceSeal(seal)
		}
		m.AppendProgress(entry)
		m.PutPlan(plan)
		m.PutSpecimen(specimen)
		created = entry
		return nil
	})
	return created, err
}
