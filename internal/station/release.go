package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// ReleaseSnapshot pairs the immutable release record with the evidence
// relations frozen at release time.
type ReleaseSnapshot struct {
	domain.ReleaseRecord
	EvidenceSeal domain.EvidenceSeal `json:"evidence_seal"`
}

func (s *Service) ReleaseSpecimen(
	ctx context.Context,
	specimenID string,
	request contracts.ReleaseRequest,
) (ReleaseSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ReleaseSnapshot{}, err
	}

	var created ReleaseSnapshot
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
		seal, err := domain.NewEvidenceSeal(
			s.nextID("seal"),
			domain.SealRelease,
			specimen.ID,
			"",
			m.EvidenceForSpecimen(specimenID),
			s.now(),
		)
		if err != nil {
			return err
		}
		m.PutRelease(record)
		m.PutReleaseEvidenceSeal(seal)
		m.PutSpecimen(specimen)
		created = ReleaseSnapshot{ReleaseRecord: record, EvidenceSeal: seal}
		return nil
	})
	return created, err
}

func (s *Service) GetRelease(ctx context.Context, specimenID string) (ReleaseSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ReleaseSnapshot{}, err
	}
	var snapshot ReleaseSnapshot
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		record, ok := m.ReleaseForSpecimen(specimenID)
		if !ok {
			return notFoundRelease(specimenID)
		}
		seal, ok := m.ReleaseEvidenceSeal(specimenID)
		if !ok {
			return contracts.Internal("release_seal_missing", "放行记录缺少证据封存", nil)
		}
		snapshot = ReleaseSnapshot{ReleaseRecord: record, EvidenceSeal: seal}
		return nil
	})
	return snapshot, err
}
