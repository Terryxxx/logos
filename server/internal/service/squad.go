package service

import (
	"github.com/logos-app/logos/server/internal/events"
	"github.com/logos-app/logos/server/internal/store"
	"github.com/logos-app/logos/server/pkg/protocol"
)

// SquadService is a thin facade over store.Squad CRUD that adds WS
// event publishing. Squads have no runtime lifecycle of their own
// (the actual squad behaviour lives in CommentService.PostAgent +
// the runner's leader-prompt injection), so this layer stays small.
type SquadService struct {
	st  *store.Store
	bus *events.Bus
}

func NewSquadService(st *store.Store, bus *events.Bus) *SquadService {
	return &SquadService{st: st, bus: bus}
}

func (s *SquadService) Create(p store.CreateSquadParams) (*store.Squad, error) {
	sq, err := s.st.CreateSquad(p)
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventSquadCreated, sq)
	return sq, nil
}

func (s *SquadService) Update(id string, p store.UpdateSquadParams) (*store.Squad, error) {
	sq, err := s.st.UpdateSquad(id, p)
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventSquadUpdated, sq)
	return sq, nil
}

func (s *SquadService) Delete(id string) error {
	if err := s.st.DeleteSquad(id); err != nil {
		return err
	}
	s.bus.Publish(protocol.EventSquadDeleted, map[string]string{"id": id})
	return nil
}

func (s *SquadService) AddMember(squadID, agentID, role string) (*store.Squad, error) {
	if err := s.st.AddSquadMember(squadID, agentID, role); err != nil {
		return nil, err
	}
	sq, err := s.st.GetSquad(squadID)
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventSquadUpdated, sq)
	return sq, nil
}

func (s *SquadService) RemoveMember(squadID, agentID string) (*store.Squad, error) {
	if err := s.st.RemoveSquadMember(squadID, agentID); err != nil {
		return nil, err
	}
	sq, err := s.st.GetSquad(squadID)
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventSquadUpdated, sq)
	return sq, nil
}
