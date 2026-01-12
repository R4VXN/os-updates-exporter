package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type State struct {
	Version   int                     `json:"version"`
	LastRunTS int64                   `json:"last_run_ts"`
	Managers  map[string]ManagerState `json:"managers"`

	LastUpdateCheckTS    int64 `json:"last_update_check_ts"`
	LastUpdateAvailable  bool  `json:"last_update_available"`
	LastUpdateRunTS      int64 `json:"last_update_run_ts"`
	LastUpdateRunSuccess bool  `json:"last_update_run_success"`
}

type ManagerState struct {
	PendingAll      int `json:"pending_all"`
	PendingSecurity int `json:"pending_security"`
	PendingBugfix   int `json:"pending_bugfix"`

	RepoUnreachable int `json:"repo_unreachable"`
	RepoTotal       int `json:"repo_total"`

	RebootRequired bool `json:"reboot_required"`

	OldestAllSeen      int64 `json:"oldest_all_seen"`
	OldestSecuritySeen int64 `json:"oldest_security_seen"`
	OldestBugfixSeen   int64 `json:"oldest_bugfix_seen"`
}

func New() *State {
	return &State{Version: 1, Managers: map[string]ManagerState{}}
}

func Load(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Managers == nil {
		s.Managers = map[string]ManagerState{}
	}
	return &s, nil
}

func SaveAtomic(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) GetManager(name string) ManagerState {
	if s == nil || s.Managers == nil {
		return ManagerState{}
	}
	return s.Managers[name]
}

func (s *State) SetManager(name string, ms ManagerState) {
	if s.Managers == nil {
		s.Managers = map[string]ManagerState{}
	}
	s.Managers[name] = ms
}

func (s *State) ensureManager(name string) ManagerState {
	ms := s.GetManager(name)
	s.SetManager(name, ms)
	return ms
}

func (s *State) Oldest(manager, typ string) int64 {
	ms := s.GetManager(manager)
	switch typ {
	case "security":
		return ms.OldestSecuritySeen
	case "bugfix":
		return ms.OldestBugfixSeen
	default:
		return ms.OldestAllSeen
	}
}

// UpdateOldestSeen implements pragmatic aging: "oldest seen while pending>0".
func (s *State) UpdateOldestSeen(manager, typ string, pending int, now int64) float64 {
	ms := s.ensureManager(manager)

	get := func() *int64 {
		switch typ {
		case "security":
			return &ms.OldestSecuritySeen
		case "bugfix":
			return &ms.OldestBugfixSeen
		default:
			return &ms.OldestAllSeen
		}
	}

	ptr := get()
	if pending <= 0 {
		*ptr = 0
		s.SetManager(manager, ms)
		return 0
	}
	if *ptr == 0 {
		*ptr = now
		s.SetManager(manager, ms)
		return 0
	}
	s.SetManager(manager, ms)
	return float64(now - *ptr)
}
