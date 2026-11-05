// Package cloud is where a kitty's desktop runs when it is not a local container: a persistent
// VM at a provider. A Provider is the small set of calls the gateway needs from one; the design
// is in docs/design/cloud-compute.md.
//
// Rules every provider follows (Contract checks them):
//   - Create is idempotent by Key: the same key never makes a second machine.
//   - A lost or ambiguous answer never makes a blank replacement: Ensure looks the machine up by
//     key before it creates again, and gives up with ErrAmbiguous when it cannot tell.
//   - Sleep keeps a checkpoint; Wake brings the same machine back from it.
package cloud

import (
	"context"
	"errors"
	"fmt"
)

// State is where a machine is in its life.
type State string

const (
	Running State = "running"
	Asleep  State = "asleep"
)

var (
	// ErrNotFound is returned for an unknown machine.
	ErrNotFound = errors.New("no such machine")
	// ErrLimit is returned when creating would pass the provider's running limit.
	ErrLimit = errors.New("too many running machines")
	// ErrAmbiguous is returned when it cannot be told whether a machine was made. Nothing is
	// created behind that answer; the owner decides.
	ErrAmbiguous = errors.New("cannot tell whether the machine exists")
)

// Machine is one VM as the provider reports it.
type Machine struct {
	ID         string
	Name       string // the kitty's name
	Key        string // the idempotency key it was created with
	State      State
	Generation int    // grows each time the machine is restored into a new VM
	Checkpoint string // the permanent saved state, empty until the first Sleep
}

// CreateRequest is saved by the caller before it is sent, so a retry reuses the same Key.
type CreateRequest struct {
	Key  string
	Name string
	Size string
}

// Provider is what the gateway needs from a cloud.
type Provider interface {
	Create(ctx context.Context, req CreateRequest) (Machine, error)
	Get(ctx context.Context, id string) (Machine, error)
	List(ctx context.Context) ([]Machine, error)
	// Sleep saves a checkpoint and stops the machine.
	Sleep(ctx context.Context, id string) (Machine, error)
	// Wake brings an asleep machine back from its checkpoint.
	Wake(ctx context.Context, id string) (Machine, error)
	Delete(ctx context.Context, id string) error
}

// Ensure creates the machine for req, or finds the one a lost answer already made. When even the
// lookup fails it returns ErrAmbiguous instead of creating again.
func Ensure(ctx context.Context, p Provider, req CreateRequest) (Machine, error) {
	if req.Key == "" || req.Name == "" {
		return Machine{}, errors.New("cloud: a request needs a key and a name")
	}
	m, err := p.Create(ctx, req)
	if err == nil {
		return m, nil
	}
	if errors.Is(err, ErrLimit) {
		return Machine{}, err
	}
	all, lerr := p.List(ctx)
	if lerr != nil {
		return Machine{}, fmt.Errorf("%w: create: %v; list: %v", ErrAmbiguous, err, lerr)
	}
	for _, m := range all {
		if m.Key == req.Key {
			return m, nil
		}
	}
	return Machine{}, err
}
