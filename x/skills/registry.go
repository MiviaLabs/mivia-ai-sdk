package skills

import "sync"

// Registry holds skills by name. Built only through New. Safe for
// concurrent Add, Get, Remove, Names, and Match; a sync.RWMutex
// guards the map.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Add calls s.Validate and returns its error unchanged on failure.
// Rejects a duplicate Name with ErrDuplicateName. Registers s under
// s.Name otherwise. Add does not defensively copy s.Triggers or
// s.RequiredTools; see Skill's doc comment.
func (r *Registry) Add(s Skill) error {
	if err := s.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.skills[s.Name]; ok {
		return ErrDuplicateName
	}
	r.skills[s.Name] = s
	return nil
}

// Get resolves name to a Skill. Returns false when name is absent.
func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// Remove removes name from the registry. Returns whether name was
// present. Removing an absent name is not a fault; it returns false
// and changes nothing.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.skills[name]; !ok {
		return false
	}
	delete(r.skills, name)
	return true
}

// Names lists every registered name. Order is unspecified; a caller
// that needs a stable order sorts the result.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return names
}
