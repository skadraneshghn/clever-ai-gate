package jobs

import (
	"fmt"
	"sync"
)

// Registry maps job type strings to their executor functions.
// All built-in and user-registered job types must be added to the registry
// before the scheduler starts.
type Registry struct {
	mu        sync.RWMutex
	executors map[string]ExecutorFunc
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		executors: make(map[string]ExecutorFunc),
	}
}

// Register associates a job type name with an executor function.
// Panics if the same type is registered twice (programming error).
func (r *Registry) Register(jobType string, fn ExecutorFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.executors[jobType]; exists {
		panic(fmt.Sprintf("job type %q already registered", jobType))
	}
	r.executors[jobType] = fn
}

// Get returns the executor for the given job type.
// Returns nil, false if not found.
func (r *Registry) Get(jobType string) (ExecutorFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.executors[jobType]
	return fn, ok
}

// ListTypes returns all registered job type names.
func (r *Registry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.executors))
	for t := range r.executors {
		types = append(types, t)
	}
	return types
}

// Has returns true if the job type is registered.
func (r *Registry) Has(jobType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.executors[jobType]
	return ok
}
