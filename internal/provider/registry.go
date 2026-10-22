package provider

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Registry holds the named provider profiles a deployment configured, so each kitty picks one by
// name ("strong", "cheap", "local") instead of carrying keys of its own.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]Client
	options map[string]Options
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{clients: map[string]Client{}, options: map[string]Options{}}
}

// Add registers a client under name, replacing one with the same name.
func (r *Registry) Add(name string, c Client, opts Options) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[name] = c
	r.options[name] = opts
}

// Build creates the HTTP client for opts and registers it under name.
func (r *Registry) Build(name string, opts Options) error {
	c, err := New(opts)
	if err != nil {
		return fmt.Errorf("provider %q: %w", name, err)
	}
	r.Add(name, c, opts)
	return nil
}

// Remove forgets the profile called name.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, name)
	delete(r.options, name)
}

// Get returns the client called name.
func (r *Registry) Get(name string) (Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider profile %q (configured: %v)", name, r.namesLocked())
	}
	return c, nil
}

// Names lists the registered profiles, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namesLocked()
}

func (r *Registry) namesLocked() []string {
	out := make([]string, 0, len(r.clients))
	for n := range r.clients {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Info is what a registry says about one profile, with the key left out.
type Info struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	Describe string `json:"describe"`
}

// List describes every profile, keys left out.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.clients))
	for _, n := range r.namesLocked() {
		o := r.options[n]
		out = append(out, Info{Name: n, Kind: o.Kind, Model: o.Model, BaseURL: o.BaseURL, Describe: r.clients[n].Describe()})
	}
	return out
}

// Func adapts a function into a Client, for tests and for the built-in echo provider.
type Func func(ctx context.Context, messages []Message) (Reply, error)

// Complete calls the function.
func (f Func) Complete(ctx context.Context, messages []Message) (Reply, error) {
	return f(ctx, messages)
}

// Describe names the adapter.
func (f Func) Describe() string { return "func" }
