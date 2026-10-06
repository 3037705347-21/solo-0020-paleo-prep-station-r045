package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

func (s *Service) ReleaseSpecimen(
	ctx context.Context,
	specimenID string,
	request contracts.ReleaseRequest,
) (domain.ReleaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReleaseRecord{}, err
	}

	var created domain.ReleaseRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}
		if m.HasOpenPlan(specimenID) {
			return contracts.Conflict("open_plan_blocks_release", "存在未结束方案，不能办理研究放行")
		}
		if _, exists := m.ReleaseForSpecimen(specimenID); exists {
			return contracts.Conflict("release_exists", "该标本已经存在放行记录")
		}
		record, err := domain.NewReleaseRecord(
			s.nextID("release"),
			specimen,
			request.Purpose,
			request.Recipient,
			request.ApprovedBy,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := specimen.MarkAvailable(s.now()); err != nil {
			return err
		}
		// 放行与证据固定必须在同一提交内生效：固定清单覆盖标本、
		// 标本全部方案以及方案下所有进度序号上的证据。
		if _, exists := m.FixationForRelease(record.ID); !exists {
			fixation, err := domain.NewEvidenceFixation(
				s.nextID("fixation"),
				domain.FixationRelease,
				specimen.ID,
				"",
				record.ID,
				m.EvidenceForRelease(specimenID),
				s.now(),
			)
			if err != nil {
				return err
			}
			m.PutFixation(fixation)
		}
		m.PutRelease(record)
		m.PutSpecimen(specimen)
		created = record
		return nil
	})
	return created, err
}

// GetRelease 读取某标本的放行记录，并带出办理放行时固定的证据关系。
func (s *Service) GetRelease(ctx context.Context, specimenID string) (ReleaseOutcome, error) {
	if err := ctx.Err(); err != nil {
		return ReleaseOutcome{}, err
	}
	var outcome ReleaseOutcome
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		record, ok := m.ReleaseForSpecimen(specimenID)
		if !ok {
			return notFoundRelease(specimenID)
		}
		outcome.Release = record
		if fixation, exists := m.FixationForRelease(record.ID); exists {
			fixed := fixation
			outcome.EvidenceFixation = &fixed
		}
		return nil
	})
	return outcome, err
}
