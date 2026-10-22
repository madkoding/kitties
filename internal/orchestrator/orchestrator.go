// Package orchestrator manages the fleet: it creates kitties from the configuration or on
// demand, gives each one its brain (a provider client), its body (a container) and its
// connectors, and keeps the list on disk so a restart finds them again.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/soul"
)

// Containers is what the orchestrator needs from the container package; a test fakes it.
type Containers interface {
	Up(ctx context.Context, s container.Spec) (string, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	Exec(ctx context.Context, name string, argv ...string) (string, error)
	Screenshot(ctx context.Context, name string) ([]byte, error)
	List(ctx context.Context) ([]container.Info, error)
}

// ErrNotFound is returned for an unknown kitty.
var ErrNotFound = errors.New("no such kitty")

// Manager is the orchestrator.
type Manager struct {
	cfg        config.Config
	providers  *provider.Registry
	connectors *connector.Registry
	containers Containers // nil when no runtime is available
	runtimeErr error

	mu      sync.RWMutex
	kitties map[string]*kitty.Kitty
	path    string
	// overlay is what the settings page changed; buildErrors says why a profile has no client.
	overlay     Settings
	buildErrors map[string]string
	now         func() time.Time
	newID       func() string
}

// Option tunes a manager.
type Option func(*Manager)

// WithContainers sets the container runtime (nil runs every kitty brain-only).
func WithContainers(c Containers, err error) Option {
	return func(m *Manager) {
		m.containers = c
		m.runtimeErr = err
	}
}

// WithProviders sets a prepared provider registry instead of building one from the configuration.
func WithProviders(r *provider.Registry) Option {
	return func(m *Manager) { m.providers = r }
}

// New builds the manager from cfg, building the providers and connectors it names.
func New(cfg config.Config, opts ...Option) (*Manager, error) {
	m := &Manager{
		cfg: cfg, connectors: connector.NewRegistry(), kitties: map[string]*kitty.Kitty{},
		path: filepath.Join(cfg.DataDirOr(), "kitties.json"), now: time.Now, newID: randomID,
		buildErrors: map[string]string{},
	}
	for _, o := range opts {
		o(m)
	}
	if err := m.loadSettings(); err != nil {
		return nil, err
	}
	if m.providers == nil {
		m.providers = provider.NewRegistry()
		for name, p := range m.cfg.Providers {
			if err := m.providers.Build(name, p.Options()); err != nil {
				// A profile without its key is kept on the page, with the reason, instead of
				// stopping everything: the page is where the key gets typed.
				m.buildErrors[name] = err.Error()
			}
		}
	}
	for name, c := range m.cfg.Connectors {
		m.connectors.Add(name, buildConnector(c, m.cfg.Motita))
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// buildConnector makes the connector of one configured entry through the catalogue. An entry
// that does not validate (it was checked before) falls back to a webhook, which never happens
// in practice.
func buildConnector(c config.Connector, mt config.Motita) connector.Connector {
	s := c.ConnectorSettings()
	if c.Kind == "motita" && s["url"] == "" {
		s["url"], s["token"] = mt.URL, mt.Token
	}
	built, err := connector.Build(c.Kind, s, c.Description)
	if err != nil {
		return connector.Webhook{URL: s["url"], Description: c.Description}
	}
	return built
}

func randomID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Config is the configuration the manager runs with.
func (m *Manager) Config() config.Config { return m.cfg }

// Providers is the provider registry.
func (m *Manager) Providers() *provider.Registry { return m.providers }

// Connectors is the connector registry.
func (m *Manager) Connectors() *connector.Registry { return m.connectors }

// HasContainers reports whether kitties can have a body, and why not otherwise.
func (m *Manager) HasContainers() (bool, error) { return m.containers != nil, m.runtimeErr }

// --- persistence ---------------------------------------------------------------------------

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the kitties: %w", err)
	}
	var list []*kitty.Kitty
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("the kitties file is damaged (%s): %w", m.path, err)
	}
	for _, k := range list {
		m.kitties[k.ID] = k
	}
	return nil
}

// saveLocked writes the list; the caller holds the lock.
func (m *Manager) saveLocked() error {
	list := m.sortedLocked()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return fmt.Errorf("create the data folder: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("save the kitties: %w", err)
	}
	return os.Rename(tmp, m.path)
}

func (m *Manager) sortedLocked() []*kitty.Kitty {
	list := make([]*kitty.Kitty, 0, len(m.kitties))
	for _, k := range m.kitties {
		list = append(list, k)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// --- the fleet -----------------------------------------------------------------------------

// Spec is what a new kitty is made of.
type Spec struct {
	Name       string    `json:"name"`
	Provider   string    `json:"provider"`
	Soul       soul.Soul `json:"soul"`
	SoulFile   string    `json:"soul_file"`
	Connectors []string  `json:"connectors"`
	// Desktop is nil for the default (a container when a runtime exists).
	Desktop *bool `json:"desktop"`
	// Avatar is the pixel cat; empty parts come from the name.
	Avatar kitty.Avatar `json:"avatar"`
}

// Create adds a kitty. The container is created on Start.
func (m *Manager) Create(ctx context.Context, s Spec) (*kitty.Kitty, error) {
	if !config.ValidName(s.Name) {
		return nil, fmt.Errorf("%q is not a valid kitty name (lowercase letters, digits and dashes)", s.Name)
	}
	prov := s.Provider
	if prov == "" {
		prov = m.cfg.ProviderFor(config.Kitty{})
	}
	if _, err := m.providers.Get(prov); err != nil {
		return nil, err
	}
	for _, c := range s.Connectors {
		if _, ok := m.connectors.Get(c); !ok {
			return nil, fmt.Errorf("unknown connector %q", c)
		}
	}
	avatar, err := s.Avatar.Normalize(s.Name)
	if err != nil {
		return nil, err
	}
	personality := soul.Default(s.Name)
	if s.SoulFile != "" {
		fromFile, err := soul.Load(s.SoulFile)
		if err != nil {
			return nil, err
		}
		personality = soul.Merge(personality, fromFile)
	}
	personality = soul.Merge(personality, s.Soul)
	desktop := m.containers != nil
	if s.Desktop != nil {
		desktop = *s.Desktop && m.containers != nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.byNameLocked(s.Name); taken {
		return nil, fmt.Errorf("a kitty called %q already exists", s.Name)
	}
	k := &kitty.Kitty{
		ID: m.newID(), Name: s.Name, Provider: prov, Soul: personality, Connectors: s.Connectors,
		Desktop: desktop, Status: kitty.StatusCreated, CreatedAt: m.now(), Avatar: avatar,
	}
	if desktop {
		k.Container = kitty.ContainerName(s.Name)
		k.Workspace = filepath.Join(m.workspaceRoot(), s.Name)
	}
	m.kitties[k.ID] = k
	if err := m.saveLocked(); err != nil {
		delete(m.kitties, k.ID)
		return nil, err
	}
	return k, nil
}

func (m *Manager) workspaceRoot() string {
	if m.cfg.Containers.Workspace != "" {
		return m.cfg.Containers.Workspace
	}
	return filepath.Join(m.cfg.DataDirOr(), "work")
}

func (m *Manager) byNameLocked(name string) (*kitty.Kitty, bool) {
	for _, k := range m.kitties {
		if k.Name == name {
			return k, true
		}
	}
	return nil, false
}

// Get finds a kitty by id or name.
func (m *Manager) Get(ref string) (*kitty.Kitty, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if k, ok := m.kitties[ref]; ok {
		return k, nil
	}
	if k, ok := m.byNameLocked(ref); ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrNotFound, ref)
}

// List returns every kitty, by name.
func (m *Manager) List() []*kitty.Kitty {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.sortedLocked()
	copies := make([]*kitty.Kitty, len(out))
	for i, k := range out {
		c := *k
		copies[i] = &c
	}
	return copies
}

// Bootstrap creates the kitties the configuration lists that do not exist yet, replicas
// included, and starts them.
func (m *Manager) Bootstrap(ctx context.Context) error {
	var errs []string
	for _, k := range m.cfg.Kitties {
		names := []string{k.Name}
		if k.Replicas > 1 {
			names = names[:0]
			for i := 1; i <= k.Replicas; i++ {
				names = append(names, fmt.Sprintf("%s-%d", k.Name, i))
			}
		}
		for _, name := range names {
			if _, err := m.Get(name); err == nil {
				continue
			}
			spec := Spec{Name: name, Provider: k.Provider, Soul: k.Soul, SoulFile: k.SoulFile, Connectors: k.Connectors, Desktop: k.Desktop, Avatar: k.Avatar}
			if spec.Soul.Name == "" {
				spec.Soul.Name = k.Name
			}
			created, err := m.Create(ctx, spec)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := m.Start(ctx, created.ID); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Start brings the kitty up: its container when it has one.
func (m *Manager) Start(ctx context.Context, ref string) error {
	k, err := m.Get(ref)
	if err != nil {
		return err
	}
	if !k.Desktop {
		return m.setStatus(k.ID, kitty.StatusRunning, "")
	}
	exists, running, err := m.containerState(ctx, k.Container)
	if err != nil {
		return m.fail(k.ID, err)
	}
	switch {
	case running:
	case exists:
		if err := m.containers.Start(ctx, k.Container); err != nil {
			return m.fail(k.ID, err)
		}
	default:
		if err := os.MkdirAll(k.Workspace, 0o700); err != nil {
			return m.fail(k.ID, fmt.Errorf("create the workspace: %w", err))
		}
		c := m.cfg.Containers
		spec := container.Spec{
			Name: k.Container, KittyName: k.Name, Image: c.Image, MemoryMB: c.MemoryMB, CPUs: c.CPUs,
			Network: c.Network, Workspace: k.Workspace, Env: c.Env,
		}
		if _, err := m.containers.Up(ctx, spec); err != nil {
			return m.fail(k.ID, err)
		}
	}
	return m.setStatus(k.ID, kitty.StatusRunning, "")
}

func (m *Manager) containerState(ctx context.Context, name string) (exists, running bool, err error) {
	infos, err := m.containers.List(ctx)
	if err != nil {
		return false, false, err
	}
	for _, i := range infos {
		if i.Name == name {
			return true, i.Running, nil
		}
	}
	return false, false, nil
}

// Stop stops the kitty's container, keeping it.
func (m *Manager) Stop(ctx context.Context, ref string) error {
	k, err := m.Get(ref)
	if err != nil {
		return err
	}
	if k.Desktop {
		if exists, _, err := m.containerState(ctx, k.Container); err != nil {
			return m.fail(k.ID, err)
		} else if exists {
			if err := m.containers.Stop(ctx, k.Container); err != nil {
				return m.fail(k.ID, err)
			}
		}
	}
	return m.setStatus(k.ID, kitty.StatusStopped, "")
}

// Remove deletes the kitty and its container. The workspace stays on disk.
func (m *Manager) Remove(ctx context.Context, ref string) error {
	k, err := m.Get(ref)
	if err != nil {
		return err
	}
	if k.Desktop {
		if exists, _, err := m.containerState(ctx, k.Container); err != nil {
			return m.fail(k.ID, err)
		} else if exists {
			if err := m.containers.Remove(ctx, k.Container); err != nil {
				return m.fail(k.ID, err)
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.kitties, k.ID)
	return m.saveLocked()
}

// Scale makes sure exactly n replicas of the configured kitty called name exist and run
// (name-1 ... name-n), creating and removing as needed.
func (m *Manager) Scale(ctx context.Context, name string, n int) error {
	if n < 0 || n > 999 {
		return fmt.Errorf("%d replicas is not between 0 and 999", n)
	}
	var base config.Kitty
	found := false
	for _, k := range m.cfg.Kitties {
		if k.Name == name {
			base, found = k, true
		}
	}
	if !found {
		return fmt.Errorf("%q is not a kitty of the configuration", name)
	}
	for _, k := range m.List() {
		if idx, ok := replicaIndex(k.Name, name); ok && idx > n {
			if err := m.Remove(ctx, k.ID); err != nil {
				return err
			}
		}
	}
	for i := 1; i <= n; i++ {
		replica := fmt.Sprintf("%s-%d", name, i)
		if _, err := m.Get(replica); err == nil {
			continue
		}
		spec := Spec{Name: replica, Provider: base.Provider, Soul: base.Soul, SoulFile: base.SoulFile, Connectors: base.Connectors, Desktop: base.Desktop, Avatar: base.Avatar}
		if spec.Soul.Name == "" {
			spec.Soul.Name = name
		}
		k, err := m.Create(ctx, spec)
		if err != nil {
			return err
		}
		if err := m.Start(ctx, k.ID); err != nil {
			return err
		}
	}
	return nil
}

func replicaIndex(full, base string) (int, bool) {
	rest, ok := strings.CutPrefix(full, base+"-")
	if !ok {
		return 0, false
	}
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, rest != ""
}

func (m *Manager) setStatus(id, status, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.kitties[id]
	if !ok {
		return ErrNotFound
	}
	k.Status, k.Error = status, msg
	return m.saveLocked()
}

func (m *Manager) fail(id string, err error) error {
	_ = m.setStatus(id, kitty.StatusError, err.Error())
	return err
}

// Update is what can change on a kitty after it exists.
type Update struct {
	Avatar *kitty.Avatar `json:"avatar,omitempty"`
	Soul   *soul.Soul    `json:"soul,omitempty"`
}

// Edit applies an update to a kitty and saves it.
func (m *Manager) Edit(ref string, u Update) (*kitty.Kitty, error) {
	k, err := m.Get(ref)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if u.Avatar != nil {
		avatar, err := u.Avatar.Normalize(k.Name)
		if err != nil {
			return nil, err
		}
		k.Avatar = avatar
	}
	if u.Soul != nil {
		k.Soul = soul.Merge(k.Soul, *u.Soul)
	}
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	c := *k
	return &c, nil
}

// Brain returns the provider client the kitty thinks with.
func (m *Manager) Brain(k *kitty.Kitty) (provider.Client, error) {
	return m.providers.Get(k.Provider)
}

// Exec runs a command in the kitty's container.
func (m *Manager) Exec(ctx context.Context, ref string, argv ...string) (string, error) {
	k, err := m.Get(ref)
	if err != nil {
		return "", err
	}
	if !k.Desktop {
		return "", fmt.Errorf("%s has no container", k.Name)
	}
	return m.containers.Exec(ctx, k.Container, argv...)
}

// Screenshot captures the kitty's display.
func (m *Manager) Screenshot(ctx context.Context, ref string) ([]byte, error) {
	k, err := m.Get(ref)
	if err != nil {
		return nil, err
	}
	if !k.Desktop {
		return nil, fmt.Errorf("%s has no container", k.Name)
	}
	return m.containers.Screenshot(ctx, k.Container)
}

// StopAll stops every running kitty; used on shutdown when the operator asks for it.
func (m *Manager) StopAll(ctx context.Context) error {
	var errs []string
	for _, k := range m.List() {
		if k.Status == kitty.StatusRunning {
			if err := m.Stop(ctx, k.ID); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
