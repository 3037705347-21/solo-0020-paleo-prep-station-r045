package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

func (s *Service) CreatePlan(
	ctx context.Context,
	specimenID string,
	request contracts.CreatePlanRequest,
) (domain.PrepPlan, error) {
	if err := ctx.Err(); err != nil {
		return domain.PrepPlan{}, err
	}

	var created domain.PrepPlan
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}
		if m.HasOpenPlan(specimenID) {
			return contracts.Conflict("open_plan_exists", "该标本已存在未结束的修复方案")
		}
		plan, err := domain.NewPrepPlan(
			s.nextID("plan"),
			specimen,
			request.Objective,
			request.Method,
			request.Adhesive,
			request.TargetExposure,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := specimen.MarkPlanned(s.now()); err != nil {
			return err
		}
		revision, err := domain.NewInitialPlanRevision(s.nextID("revision"), plan, s.now())
		if err != nil {
			return err
		}
		m.PutPlan(plan)
		m.AppendPlanRevision(revision)
		m.PutSpecimen(specimen)
		created = plan
		return nil
	})
	return created, err
}

func (s *Service) ApprovePlan(
	ctx context.Context,
	planID string,
	request contracts.ApprovalRequest,
) (domain.PrepPlan, error) {
	if err := ctx.Err(); err != nil {
		return domain.PrepPlan{}, err
	}

	var approved domain.PrepPlan
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		specimen, ok := m.GetSpecimen(plan.SpecimenID)
		if !ok {
			return notFoundSpecimen(plan.SpecimenID)
		}
		if err := plan.Approve(request.ApprovedBy, s.now()); err != nil {
			return err
		}
		if err := specimen.MarkPreparing(s.now()); err != nil {
			return err
		}
		m.PutPlan(plan)
		m.PutSpecimen(specimen)
		approved = plan
		return nil
	})
	return approved, err
}

func (s *Service) GetPlan(ctx context.Context, planID string) (PlanSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PlanSnapshot{}, err
	}
	var snapshot PlanSnapshot
	err := s.repo.Read(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		revisions := m.PlanRevisions(planID)
		progress := m.ProgressForPlan(planID)
		if len(revisions) > 0 {
			latest := revisions[len(revisions)-1]
			for index := range progress {
				progress[index].RevisionNumber = latest.Number
				progress[index].Conditions = latest.Conditions
			}
		}
		snapshot = PlanSnapshot{
			Plan:      plan,
			Revisions: revisions,
			Progress:  progress,
		}
		return nil
	})
	return snapshot, err
}

func (s *Service) RevisePlan(
	ctx context.Context,
	planID string,
	request contracts.RevisePlanRequest,
) (domain.PlanRevision, error) {
	if err := ctx.Err(); err != nil {
		return domain.PlanRevision{}, err
	}

	var plan domain.PrepPlan
	var previous []domain.PlanRevision
	var progressCount int
	var existing domain.PlanRevision
	alreadyApplied := false
	err := s.repo.Read(func(m *repository.Mutation) error {
		found, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		plan = found
		if value, ok := m.FindPlanRevisionByRequest(planID, request.RequestID); ok {
			existing = value
			alreadyApplied = true
			return nil
		}
		previous = m.PlanRevisions(planID)
		progressCount = len(m.ProgressForPlan(planID))
		return nil
	})
	if err != nil {
		return domain.PlanRevision{}, err
	}
	if alreadyApplied {
		return existing, nil
	}

	revision, err := domain.NewPlanRevision(
		s.nextID("revision"),
		plan,
		previous,
		progressCount,
		request,
		s.now(),
	)
	if err != nil {
		return domain.PlanRevision{}, err
	}

	var created domain.PlanRevision
	err = s.repo.Mutate(func(m *repository.Mutation) error {
		current, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		if err := current.ApplyRevision(revision.Conditions, s.now()); err != nil {
			return err
		}
		m.AppendPlanRevision(revision)
		m.PutPlan(current)
		created = revision
		return nil
	})
	return created, err
}

func (s *Service) ListPlansForSpecimen(
	ctx context.Context,
	specimenID string,
) ([]domain.PrepPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.PrepPlan
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		values = m.ListPlansForSpecimen(specimenID)
		return nil
	})
	return values, err
}
