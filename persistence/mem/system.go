package mem

import "github.com/VolantMQ/volantmq/persistence/types"

type system struct {
	status *dbStatus
}

func (s *system) GetInfo() (*persistenceTypes.SystemState, error) {
	state := &persistenceTypes.SystemState{}

	return state, nil
}

func (s *system) SetInfo(state *persistenceTypes.SystemState) error {
	return nil
}
