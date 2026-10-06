package repository

import (
	"sort"
	"sync"

	"example.com/paleo-prep-station/internal/domain"
)

type state struct {
	specimens       map[string]domain.Specimen
	plans           map[string]domain.PrepPlan
	revisions       map[string][]domain.PlanRevision
	progress        map[string][]domain.ProgressEntry
	releases        map[string]domain.ReleaseRecord
	evidence        map[string]domain.EvidenceRecord
	fixations       map[string]domain.EvidenceFixation
	planFixation    map[string]string
	releaseFixation map[string]string
}

type MemoryRepository struct {
	mu    sync.Mutex
	state state
}

func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		state: state{
			specimens:       make(map[string]domain.Specimen),
			plans:           make(map[string]domain.PrepPlan),
			revisions:       make(map[string][]domain.PlanRevision),
			progress:        make(map[string][]domain.ProgressEntry),
			releases:        make(map[string]domain.ReleaseRecord),
			evidence:        make(map[string]domain.EvidenceRecord),
			fixations:       make(map[string]domain.EvidenceFixation),
			planFixation:    make(map[string]string),
			releaseFixation: make(map[string]string),
		},
	}
}

func (r *MemoryRepository) Read(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(cloneMutation(r.state))
}

func (r *MemoryRepository) Mutate(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx := cloneMutation(r.state)
	if err := fn(tx); err != nil {
		return err
	}
	r.state = tx.snapshot()
	return nil
}

func cloneMutation(source state) *Mutation {
	copied := state{
		specimens:       make(map[string]domain.Specimen, len(source.specimens)),
		plans:           make(map[string]domain.PrepPlan, len(source.plans)),
		revisions:       make(map[string][]domain.PlanRevision, len(source.revisions)),
		progress:        make(map[string][]domain.ProgressEntry, len(source.progress)),
		releases:        make(map[string]domain.ReleaseRecord, len(source.releases)),
		evidence:        make(map[string]domain.EvidenceRecord, len(source.evidence)),
		fixations:       make(map[string]domain.EvidenceFixation, len(source.fixations)),
		planFixation:    make(map[string]string, len(source.planFixation)),
		releaseFixation: make(map[string]string, len(source.releaseFixation)),
	}
	for key, value := range source.specimens {
		copied.specimens[key] = value
	}
	for key, value := range source.plans {
		copied.plans[key] = value
	}
	for key, values := range source.revisions {
		copied.revisions[key] = append([]domain.PlanRevision(nil), values...)
	}
	for key, values := range source.progress {
		copied.progress[key] = append([]domain.ProgressEntry(nil), values...)
	}
	for key, value := range source.releases {
		copied.releases[key] = value
	}
	for key, value := range source.evidence {
		copied.evidence[key] = value
	}
	for key, value := range source.fixations {
		value.Records = append([]domain.EvidenceRecord(nil), value.Records...)
		copied.fixations[key] = value
	}
	for key, value := range source.planFixation {
		copied.planFixation[key] = value
	}
	for key, value := range source.releaseFixation {
		copied.releaseFixation[key] = value
	}
	return &Mutation{state: copied}
}

type Mutation struct {
	state state
}

func (m *Mutation) snapshot() state {
	return m.state
}

func (m *Mutation) GetSpecimen(id string) (domain.Specimen, bool) {
	value, ok := m.state.specimens[id]
	return value, ok
}

func (m *Mutation) FindSpecimenByCatalog(catalogNumber string) (domain.Specimen, bool) {
	for _, value := range m.state.specimens {
		if value.CatalogNumber == catalogNumber {
			return value, true
		}
	}
	return domain.Specimen{}, false
}

func (m *Mutation) ListSpecimens() []domain.Specimen {
	values := make([]domain.Specimen, 0, len(m.state.specimens))
	for _, value := range m.state.specimens {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutSpecimen(value domain.Specimen) {
	m.state.specimens[value.ID] = value
}

func (m *Mutation) GetPlan(id string) (domain.PrepPlan, bool) {
	value, ok := m.state.plans[id]
	return value, ok
}

func (m *Mutation) ListPlansForSpecimen(specimenID string) []domain.PrepPlan {
	values := make([]domain.PrepPlan, 0)
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutPlan(value domain.PrepPlan) {
	m.state.plans[value.ID] = value
}

func (m *Mutation) PlanRevisions(planID string) []domain.PlanRevision {
	return append([]domain.PlanRevision(nil), m.state.revisions[planID]...)
}

func (m *Mutation) AppendPlanRevision(value domain.PlanRevision) {
	m.state.revisions[value.PlanID] = append(m.state.revisions[value.PlanID], value)
}

func (m *Mutation) FindPlanRevisionByRequest(
	planID string,
	requestID string,
) (domain.PlanRevision, bool) {
	if requestID == "" {
		return domain.PlanRevision{}, false
	}
	for _, value := range m.state.revisions[planID] {
		if value.RequestID == requestID {
			return value, true
		}
	}
	return domain.PlanRevision{}, false
}

func (m *Mutation) RevisionForSequence(
	planID string,
	sequence int,
) (domain.PlanRevision, bool) {
	var selected domain.PlanRevision
	found := false
	for _, value := range m.state.revisions[planID] {
		if value.EffectiveSequence > sequence {
			continue
		}
		if !found || value.Number > selected.Number {
			selected = value
			found = true
		}
	}
	return selected, found
}

func (m *Mutation) HasOpenPlan(specimenID string) bool {
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID && value.IsOpen() {
			return true
		}
	}
	return false
}

func (m *Mutation) ProgressForPlan(planID string) []domain.ProgressEntry {
	return append([]domain.ProgressEntry(nil), m.state.progress[planID]...)
}

func (m *Mutation) AppendProgress(value domain.ProgressEntry) {
	m.state.progress[value.PlanID] = append(m.state.progress[value.PlanID], value)
}

func (m *Mutation) ReleaseForSpecimen(specimenID string) (domain.ReleaseRecord, bool) {
	for _, value := range m.state.releases {
		if value.SpecimenID == specimenID {
			return value, true
		}
	}
	return domain.ReleaseRecord{}, false
}

func (m *Mutation) PutRelease(value domain.ReleaseRecord) {
	m.state.releases[value.ID] = value
}

func (m *Mutation) PutEvidence(value domain.EvidenceRecord) {
	m.state.evidence[value.ID] = value
}

func (m *Mutation) GetEvidence(id string) (domain.EvidenceRecord, bool) {
	value, ok := m.state.evidence[id]
	return value, ok
}

func (m *Mutation) sortedEvidence(include func(domain.EvidenceRecord) bool) []domain.EvidenceRecord {
	values := make([]domain.EvidenceRecord, 0)
	for _, value := range m.state.evidence {
		if include(value) {
			values = append(values, value)
		}
	}
	sortEvidence(values)
	return values
}

// sortEvidence 是仓储侧的排序，保持与 domain.EvidenceFixation 相同的确定顺序。
func sortEvidence(values []domain.EvidenceRecord) {
	sort.Slice(values, func(i, j int) bool {
		if !values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].CreatedAt.Before(values[j].CreatedAt)
		}
		return values[i].ID < values[j].ID
	})
}

// EvidenceForTarget 返回某一精确关联坐标上的全部证据。
func (m *Mutation) EvidenceForTarget(target domain.EvidenceTarget) []domain.EvidenceRecord {
	return m.sortedEvidence(func(value domain.EvidenceRecord) bool {
		return value.Target == target
	})
}

// EvidenceForSpecimen 返回直接关联到标本的证据。
func (m *Mutation) EvidenceForSpecimen(specimenID string) []domain.EvidenceRecord {
	return m.sortedEvidence(func(value domain.EvidenceRecord) bool {
		return value.Target.Kind == domain.EvidenceTargetSpecimen &&
			value.Target.SpecimenID == specimenID
	})
}

// EvidenceForPlanCompletion 收集方案完成时必须固定的证据：
// 方案级证据以及该方案下所有进度序号的证据。
func (m *Mutation) EvidenceForPlanCompletion(planID string) []domain.EvidenceRecord {
	return m.sortedEvidence(func(value domain.EvidenceRecord) bool {
		switch value.Target.Kind {
		case domain.EvidenceTargetPlan:
			return value.Target.PlanID == planID
		case domain.EvidenceTargetProgress:
			return value.Target.PlanID == planID
		default:
			return false
		}
	})
}

// EvidenceForRelease 收集放行时必须固定的证据：
// 标本自身、标本所有方案以及这些方案下全部进度序号上的证据。
func (m *Mutation) EvidenceForRelease(specimenID string) []domain.EvidenceRecord {
	planIDs := make(map[string]struct{})
	for _, plan := range m.state.plans {
		if plan.SpecimenID == specimenID {
			planIDs[plan.ID] = struct{}{}
		}
	}
	return m.sortedEvidence(func(value domain.EvidenceRecord) bool {
		switch value.Target.Kind {
		case domain.EvidenceTargetSpecimen:
			return value.Target.SpecimenID == specimenID
		case domain.EvidenceTargetPlan, domain.EvidenceTargetProgress:
			_, ok := planIDs[value.Target.PlanID]
			return ok
		default:
			return false
		}
	})
}

func (m *Mutation) PutFixation(value domain.EvidenceFixation) {
	copied := value
	copied.Records = append([]domain.EvidenceRecord(nil), value.Records...)
	m.state.fixations[value.ID] = copied
	switch value.Scope {
	case domain.FixationPlanCompletion:
		m.state.planFixation[value.PlanID] = value.ID
	case domain.FixationRelease:
		m.state.releaseFixation[value.ReleaseID] = value.ID
	}
}

func (m *Mutation) GetFixation(id string) (domain.EvidenceFixation, bool) {
	value, ok := m.state.fixations[id]
	if ok {
		value.Records = append([]domain.EvidenceRecord(nil), value.Records...)
	}
	return value, ok
}

func (m *Mutation) FixationForPlan(planID string) (domain.EvidenceFixation, bool) {
	id, ok := m.state.planFixation[planID]
	if !ok {
		return domain.EvidenceFixation{}, false
	}
	return m.GetFixation(id)
}

func (m *Mutation) FixationForRelease(releaseID string) (domain.EvidenceFixation, bool) {
	id, ok := m.state.releaseFixation[releaseID]
	if !ok {
		return domain.EvidenceFixation{}, false
	}
	return m.GetFixation(id)
}
