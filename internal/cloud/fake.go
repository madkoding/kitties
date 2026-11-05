package cloud

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Fake is an in-memory Provider for tests, with switches for the failures a real cloud has.
type Fake struct {
	// MaxRunning is the running limit; zero means no limit.
	MaxRunning int

	mu       sync.Mutex
	next     int
	machines map[string]*Machine
	byKey    map[string]string

	loseNextCreate bool
	failList       bool
}

// NewFake returns an empty provider.
func NewFake(maxRunning int) *Fake {
	return &Fake{MaxRunning: maxRunning, machines: map[string]*Machine{}, byKey: map[string]string{}}
}

// LoseNextCreate makes the next Create make its machine and then answer with an error.
func (f *Fake) LoseNextCreate() { f.mu.Lock(); f.loseNextCreate = true; f.mu.Unlock() }

// FailList makes List fail, as an API outage does.
func (f *Fake) FailList(on bool) { f.mu.Lock(); f.failList = on; f.mu.Unlock() }

func (f *Fake) running() int {
	n := 0
	for _, m := range f.machines {
		if m.State == Running {
			n++
		}
	}
	return n
}

func (f *Fake) Create(_ context.Context, req CreateRequest) (Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byKey[req.Key]; ok {
		return *f.machines[id], nil
	}
	if f.MaxRunning > 0 && f.running() >= f.MaxRunning {
		return Machine{}, ErrLimit
	}
	f.next++
	m := &Machine{ID: fmt.Sprintf("vm-%d", f.next), Name: req.Name, Key: req.Key, State: Running, Generation: 1}
	f.machines[m.ID] = m
	f.byKey[req.Key] = m.ID
	if f.loseNextCreate {
		f.loseNextCreate = false
		return Machine{}, errors.New("connection reset")
	}
	return *m, nil
}

func (f *Fake) Get(_ context.Context, id string) (Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return Machine{}, ErrNotFound
	}
	return *m, nil
}

func (f *Fake) List(context.Context) ([]Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList {
		return nil, errors.New("api unavailable")
	}
	out := make([]Machine, 0, len(f.machines))
	for _, m := range f.machines {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *Fake) Sleep(_ context.Context, id string) (Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return Machine{}, ErrNotFound
	}
	if m.State == Running {
		m.State = Asleep
		m.Checkpoint = fmt.Sprintf("%s@%d", m.ID, m.Generation)
	}
	return *m, nil
}

func (f *Fake) Wake(_ context.Context, id string) (Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return Machine{}, ErrNotFound
	}
	if m.State == Asleep {
		if f.MaxRunning > 0 && f.running() >= f.MaxRunning {
			return Machine{}, ErrLimit
		}
		m.State = Running
		m.Generation++
	}
	return *m, nil
}

func (f *Fake) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return ErrNotFound
	}
	delete(f.byKey, m.Key)
	delete(f.machines, id)
	return nil
}
