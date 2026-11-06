package wameow

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	mathRand "math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/util/keys"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"crypto/rand"
)

// Store keeps everything of one linked device in memory: its keys, the Signal sessions, the
// contacts and the rest whatsmeow needs. Export makes one blob of it, which the bridge seals
// with the vault's key, and Import reads it back: nothing is written to disk in the clear (the
// library's own store is a SQL database in a file).
type Store struct {
	mu   sync.Mutex
	data snapshot
	// dirty is set by every change, for the client to export the session.
	dirty atomic.Bool
	// What is not worth keeping: the decryption buffer and the copies kept for retries.
	buffered map[[32]byte]*store.BufferedEvent
	outgoing map[string]outgoingEvent
	device   *store.Device
	log      waLog.Logger
}

type outgoingEvent struct {
	format string
	data   []byte
}

// snapshotVersion is the shape of the blob.
const snapshotVersion = 1

type snapshot struct {
	Version      int                              `json:"v"`
	Device       *deviceData                      `json:"device,omitempty"`
	Identities   map[string][]byte                `json:"identities,omitempty"`
	Sessions     map[string][]byte                `json:"sessions,omitempty"`
	PreKeys      map[uint32]preKeyData            `json:"pre_keys,omitempty"`
	SenderKeys   map[string][]byte                `json:"sender_keys,omitempty"`
	SyncKeys     map[string]store.AppStateSyncKey `json:"sync_keys,omitempty"`
	AppStates    map[string]appState              `json:"app_states,omitempty"`
	MACs         map[string]map[string]macEntry   `json:"macs,omitempty"`
	Contacts     map[string]types.ContactInfo     `json:"contacts,omitempty"`
	ChatSettings map[string]chatSettings          `json:"chat_settings,omitempty"`
	Secrets      map[string]msgSecret             `json:"secrets,omitempty"`
	Privacy      map[string]privacyToken          `json:"privacy,omitempty"`
	NCTSalt      []byte                           `json:"nct_salt,omitempty"`
	LIDToPN      map[string]string                `json:"lid_to_pn,omitempty"`
	PNToLID      map[string]string                `json:"pn_to_lid,omitempty"`
}

type deviceData struct {
	Noise          []byte `json:"noise"`
	Identity       []byte `json:"identity"`
	PreKey         []byte `json:"pre_key"`
	PreKeyID       uint32 `json:"pre_key_id"`
	PreKeySig      []byte `json:"pre_key_sig"`
	RegistrationID uint32 `json:"registration_id"`
	AdvSecret      []byte `json:"adv_secret"`
	ID             string `json:"id"`
	LID            string `json:"lid,omitempty"`
	Account        []byte `json:"account,omitempty"`
	Platform       string `json:"platform,omitempty"`
	BusinessName   string `json:"business_name,omitempty"`
	PushName       string `json:"push_name,omitempty"`
	FacebookUUID   string `json:"facebook_uuid,omitempty"`
	LIDMigration   int64  `json:"lid_migration,omitempty"`
	MetaNonce      string `json:"meta_nonce,omitempty"`
}

type preKeyData struct {
	Priv     []byte `json:"priv"`
	Uploaded bool   `json:"uploaded,omitempty"`
}

type appState struct {
	Version uint64 `json:"version"`
	Hash    []byte `json:"hash"`
}

type macEntry struct {
	Version uint64 `json:"version"`
	Value   []byte `json:"value"`
}

type chatSettings struct {
	MutedUntil int64  `json:"muted_until,omitempty"`
	Pinned     bool   `json:"pinned,omitempty"`
	Archived   bool   `json:"archived,omitempty"`
	WASA       string `json:"wasa,omitempty"`
}

type msgSecret struct {
	Secret []byte `json:"secret"`
	Sender string `json:"sender"`
}

type privacyToken struct {
	Token     []byte `json:"token"`
	Timestamp int64  `json:"ts"`
	Sender    int64  `json:"sender_ts,omitempty"`
}

var (
	_ store.AllStores       = (*Store)(nil)
	_ store.DeviceContainer = (*Store)(nil)
)

// errInvalid is a kept session that is not whole.
var errInvalid = errors.New("the kept WhatsApp session is damaged")

func newStore(log waLog.Logger) *Store {
	if log == nil {
		log = waLog.Noop
	}
	s := &Store{log: log, buffered: map[[32]byte]*store.BufferedEvent{}, outgoing: map[string]outgoingEvent{}}
	s.data = snapshot{Version: snapshotVersion}
	s.fill()
	return s
}

// fill makes the maps a blob may leave out.
func (s *Store) fill() {
	d := &s.data
	if d.Identities == nil {
		d.Identities = map[string][]byte{}
	}
	if d.Sessions == nil {
		d.Sessions = map[string][]byte{}
	}
	if d.PreKeys == nil {
		d.PreKeys = map[uint32]preKeyData{}
	}
	if d.SenderKeys == nil {
		d.SenderKeys = map[string][]byte{}
	}
	if d.SyncKeys == nil {
		d.SyncKeys = map[string]store.AppStateSyncKey{}
	}
	if d.AppStates == nil {
		d.AppStates = map[string]appState{}
	}
	if d.MACs == nil {
		d.MACs = map[string]map[string]macEntry{}
	}
	if d.Contacts == nil {
		d.Contacts = map[string]types.ContactInfo{}
	}
	if d.ChatSettings == nil {
		d.ChatSettings = map[string]chatSettings{}
	}
	if d.Secrets == nil {
		d.Secrets = map[string]msgSecret{}
	}
	if d.Privacy == nil {
		d.Privacy = map[string]privacyToken{}
	}
	if d.LIDToPN == nil {
		d.LIDToPN = map[string]string{}
	}
	if d.PNToLID == nil {
		d.PNToLID = map[string]string{}
	}
}

// Open reads a blob made by Export (nil or empty makes a new device to link) and returns the
// device whatsmeow runs on.
func Open(blob []byte, log waLog.Logger) (*Store, *store.Device, error) {
	s := newStore(log)
	if len(blob) == 0 {
		return s, s.NewDevice(), nil
	}
	var snap snapshot
	if err := json.Unmarshal(blob, &snap); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errInvalid, err)
	}
	if snap.Version != snapshotVersion || snap.Device == nil {
		return nil, nil, errInvalid
	}
	s.data = snap
	s.fill()
	dev, err := s.deviceOf(snap.Device)
	if err != nil {
		return nil, nil, err
	}
	return s, dev, nil
}

func key32(b []byte) (*[32]byte, error) {
	if len(b) != 32 {
		return nil, errInvalid
	}
	return (*[32]byte)(b), nil
}

func (s *Store) deviceOf(d *deviceData) (*store.Device, error) {
	noise, err := key32(d.Noise)
	if err != nil {
		return nil, err
	}
	ident, err := key32(d.Identity)
	if err != nil {
		return nil, err
	}
	pre, err := key32(d.PreKey)
	if err != nil {
		return nil, err
	}
	if len(d.PreKeySig) != 64 {
		return nil, errInvalid
	}
	id, err := types.ParseJID(d.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalid, err)
	}
	var account waAdv.ADVSignedDeviceIdentity
	if err := proto.Unmarshal(d.Account, &account); err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalid, err)
	}
	dev := &store.Device{
		Log:            s.log,
		NoiseKey:       keys.NewKeyPairFromPrivateKey(*noise),
		IdentityKey:    keys.NewKeyPairFromPrivateKey(*ident),
		SignedPreKey:   &keys.PreKey{KeyPair: *keys.NewKeyPairFromPrivateKey(*pre), KeyID: d.PreKeyID, Signature: (*[64]byte)(d.PreKeySig)},
		RegistrationID: d.RegistrationID,
		AdvSecretKey:   d.AdvSecret,
		ID:             &id,
		Account:        &account,
		Platform:       d.Platform,
		BusinessName:   d.BusinessName,
		PushName:       d.PushName,

		LIDMigrationTimestamp: d.LIDMigration,
		CompanionMetaNonce:    d.MetaNonce,
	}
	if d.LID != "" {
		if dev.LID, err = types.ParseJID(d.LID); err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalid, err)
		}
	}
	if d.FacebookUUID != "" {
		dev.FacebookUUID, _ = uuid.Parse(d.FacebookUUID)
	}
	s.initialize(dev)
	return dev, nil
}

func (s *Store) initialize(dev *store.Device) {
	dev.SetAllStores(s)
	dev.LIDs = s
	dev.Container = s
	dev.Initialized = true
	s.device = dev
}

// NewDevice is a device not linked yet: its keys are kept once the phone scans the code.
func (s *Store) NewDevice() *store.Device {
	dev := &store.Device{
		Log:            s.log,
		Container:      s,
		NoiseKey:       keys.NewKeyPair(),
		IdentityKey:    keys.NewKeyPair(),
		RegistrationID: mathRand.Uint32(),
		AdvSecretKey:   make([]byte, 32),
	}
	_, _ = rand.Read(dev.AdvSecretKey)
	dev.SignedPreKey = dev.IdentityKey.CreateSignedPreKey(1)
	s.device = dev
	return dev
}

// Export is the whole state as one blob, for Open. A device not linked yet has nothing to keep.
func (s *Store) Export() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Device == nil {
		return nil, errors.New("the device is not linked yet")
	}
	return json.Marshal(s.data)
}

// Dirty reports, once, whether anything changed since the last call.
func (s *Store) Dirty() bool { return s.dirty.Swap(false) }

// changed marks a change; call with mu held.
func (s *Store) changed() { s.dirty.Store(true) }

// PutDevice implements store.DeviceContainer.
func (s *Store) PutDevice(_ context.Context, dev *store.Device) error {
	if dev.ID == nil {
		return errors.New("device JID must be known before it is kept")
	}
	d := &deviceData{
		Noise:          dev.NoiseKey.Priv[:],
		Identity:       dev.IdentityKey.Priv[:],
		PreKey:         dev.SignedPreKey.Priv[:],
		PreKeyID:       dev.SignedPreKey.KeyID,
		PreKeySig:      dev.SignedPreKey.Signature[:],
		RegistrationID: dev.RegistrationID,
		AdvSecret:      dev.AdvSecretKey,
		ID:             dev.ID.String(),
		Platform:       dev.Platform,
		BusinessName:   dev.BusinessName,
		PushName:       dev.PushName,
		LIDMigration:   dev.LIDMigrationTimestamp,
		MetaNonce:      dev.CompanionMetaNonce,
	}
	if !dev.LID.IsEmpty() {
		d.LID = dev.LID.String()
	}
	if dev.FacebookUUID != uuid.Nil {
		d.FacebookUUID = dev.FacebookUUID.String()
	}
	if dev.Account != nil {
		acc, err := proto.Marshal(dev.Account)
		if err != nil {
			return err
		}
		d.Account = acc
	}
	s.mu.Lock()
	s.data.Device = d
	s.changed()
	s.mu.Unlock()
	if !dev.Initialized {
		s.initialize(dev)
	}
	return nil
}

// DeleteDevice implements store.DeviceContainer: everything of the device goes.
func (s *Store) DeleteDevice(_ context.Context, _ *store.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = snapshot{Version: snapshotVersion}
	s.fill()
	s.changed()
	return nil
}

// Identities.

func (s *Store) PutIdentity(_ context.Context, address string, key [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Identities[address] = slices.Clone(key[:])
	s.changed()
	return nil
}

// deletePrefix removes the keys of a map that start with prefix.
func deletePrefix[V any](m map[string]V, prefix string) {
	for k := range m {
		if strings.HasPrefix(k, prefix) {
			delete(m, k)
		}
	}
}

func (s *Store) DeleteAllIdentities(_ context.Context, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	deletePrefix(s.data.Identities, phone+":")
	s.changed()
	return nil
}

func (s *Store) DeleteIdentity(_ context.Context, address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Identities, address)
	s.changed()
	return nil
}

func (s *Store) IsTrustedIdentity(_ context.Context, address string, key [32]byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	known, ok := s.data.Identities[address]
	if !ok {
		// Trusted when not known: it is kept right after.
		return true, nil
	}
	if len(known) != 32 {
		return false, errInvalid
	}
	return [32]byte(known) == key, nil
}

// Sessions.

func (s *Store) GetSession(_ context.Context, address string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.Sessions[address]), nil
}

func (s *Store) HasSession(_ context.Context, address string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data.Sessions[address]
	return ok, nil
}

func (s *Store) GetManySessions(_ context.Context, addresses []string) (map[string][]byte, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(addresses))
	for _, a := range addresses {
		out[a] = slices.Clone(s.data.Sessions[a])
	}
	return out, nil
}

func (s *Store) PutSession(_ context.Context, address string, session []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sessions[address] = slices.Clone(session)
	s.changed()
	return nil
}

func (s *Store) PutManySessions(_ context.Context, sessions map[string][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for a, v := range sessions {
		s.data.Sessions[a] = slices.Clone(v)
	}
	s.changed()
	return nil
}

func (s *Store) DeleteAllSessions(_ context.Context, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	deletePrefix(s.data.Sessions, phone+":")
	s.changed()
	return nil
}

func (s *Store) DeleteSession(_ context.Context, address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Sessions, address)
	s.changed()
	return nil
}

// migrate moves the entries of a map from the phone number's addresses to the LID's.
func migrate(m map[string][]byte, keyOf func(string) (string, string, bool), join func(string, string) string, pn, lid string) {
	for k, v := range m {
		scope, addr, ok := keyOf(k)
		if !ok || !strings.HasPrefix(addr, pn+":") {
			continue
		}
		m[join(scope, lid+strings.TrimPrefix(addr, pn))] = v
		delete(m, k)
	}
}

func plainKey(k string) (string, string, bool) { return "", k, true }
func plainJoin(_, addr string) string          { return addr }

func (s *Store) MigratePNToLID(_ context.Context, pn, lid types.JID) error {
	from, to := pn.SignalAddressUser(), lid.SignalAddressUser()
	s.mu.Lock()
	defer s.mu.Unlock()
	migrate(s.data.Sessions, plainKey, plainJoin, from, to)
	migrate(s.data.Identities, plainKey, plainJoin, from, to)
	migrate(s.data.SenderKeys, splitSenderKey, senderKey, from, to)
	s.changed()
	return nil
}

// Pre-keys.

func (s *Store) nextPreKeyID() uint32 {
	var top uint32
	for id := range s.data.PreKeys {
		top = max(top, id)
	}
	return top + 1
}

func (s *Store) genPreKey(id uint32, uploaded bool) *keys.PreKey {
	k := keys.NewPreKey(id)
	s.data.PreKeys[id] = preKeyData{Priv: slices.Clone(k.Priv[:]), Uploaded: uploaded}
	s.changed()
	return k
}

func preKeyOf(id uint32, d preKeyData) (*keys.PreKey, error) {
	priv, err := key32(d.Priv)
	if err != nil {
		return nil, err
	}
	return &keys.PreKey{KeyPair: *keys.NewKeyPairFromPrivateKey(*priv), KeyID: id}, nil
}

func (s *Store) GetOrGenPreKeys(_ context.Context, count uint32) ([]*keys.PreKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []uint32
	for id, d := range s.data.PreKeys {
		if !d.Uploaded {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	out := make([]*keys.PreKey, 0, count)
	for _, id := range ids {
		if uint32(len(out)) == count {
			break
		}
		k, err := preKeyOf(id, s.data.PreKeys[id])
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	next := s.nextPreKeyID()
	for uint32(len(out)) < count {
		out = append(out, s.genPreKey(next, false))
		next++
	}
	return out, nil
}

func (s *Store) GenOnePreKey(_ context.Context) (*keys.PreKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.genPreKey(s.nextPreKeyID(), true), nil
}

func (s *Store) GetPreKey(_ context.Context, id uint32) (*keys.PreKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data.PreKeys[id]
	if !ok {
		return nil, nil
	}
	return preKeyOf(id, d)
}

func (s *Store) RemovePreKey(_ context.Context, id uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.PreKeys, id)
	s.changed()
	return nil
}

func (s *Store) MarkPreKeysAsUploaded(_ context.Context, upTo uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.data.PreKeys {
		if id <= upTo && !d.Uploaded {
			d.Uploaded = true
			s.data.PreKeys[id] = d
		}
	}
	s.changed()
	return nil
}

func (s *Store) UploadedPreKeyCount(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, d := range s.data.PreKeys {
		if d.Uploaded {
			n++
		}
	}
	return n, nil
}

// Sender keys of groups, by group and sender.

func senderKey(group, user string) string { return group + "\x00" + user }

func splitSenderKey(k string) (string, string, bool) {
	return strings.Cut(k, "\x00")
}

func (s *Store) PutSenderKey(_ context.Context, group, user string, session []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.SenderKeys[senderKey(group, user)] = slices.Clone(session)
	s.changed()
	return nil
}

func (s *Store) GetSenderKey(_ context.Context, group, user string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.SenderKeys[senderKey(group, user)]), nil
}

// App state keys and versions.

func (s *Store) PutAppStateSyncKey(_ context.Context, id []byte, key store.AppStateSyncKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := hex.EncodeToString(id)
	if old, ok := s.data.SyncKeys[k]; ok && key.Timestamp <= old.Timestamp {
		return nil
	}
	s.data.SyncKeys[k] = key
	s.changed()
	return nil
}

func (s *Store) GetAppStateSyncKey(_ context.Context, id []byte) (*store.AppStateSyncKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.data.SyncKeys[hex.EncodeToString(id)]
	if !ok {
		return nil, nil
	}
	return &k, nil
}

// syncKeysByAge are the ids of the sync keys, newest first.
func (s *Store) syncKeysByAge() []string {
	ids := slices.Collect(maps.Keys(s.data.SyncKeys))
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.data.SyncKeys[ids[i]], s.data.SyncKeys[ids[j]]
		if a.Timestamp != b.Timestamp {
			return a.Timestamp > b.Timestamp
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (s *Store) GetLatestAppStateSyncKeyID(_ context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.syncKeysByAge()
	if len(ids) == 0 {
		return nil, nil
	}
	return hex.DecodeString(ids[0])
}

func (s *Store) GetAllAppStateSyncKeys(_ context.Context) ([]*store.AppStateSyncKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*store.AppStateSyncKey
	for _, id := range s.syncKeysByAge() {
		k := s.data.SyncKeys[id]
		out = append(out, &k)
	}
	return out, nil
}

func (s *Store) PutAppStateVersion(_ context.Context, name string, version uint64, hash [128]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.AppStates[name] = appState{Version: version, Hash: slices.Clone(hash[:])}
	s.changed()
	return nil
}

func (s *Store) GetAppStateVersion(_ context.Context, name string) (uint64, [128]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.data.AppStates[name]
	if !ok {
		return 0, [128]byte{}, nil
	}
	if len(st.Hash) != 128 {
		return 0, [128]byte{}, errInvalid
	}
	return st.Version, [128]byte(st.Hash), nil
}

func (s *Store) DeleteAppStateVersion(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.AppStates, name)
	s.changed()
	return nil
}

func (s *Store) PutAppStateMutationMACs(_ context.Context, name string, version uint64, mutations []store.AppStateMutationMAC) error {
	if len(mutations) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.MACs[name]
	if m == nil {
		m = map[string]macEntry{}
		s.data.MACs[name] = m
	}
	for _, mu := range mutations {
		k := hex.EncodeToString(mu.IndexMAC)
		if old, ok := m[k]; ok && old.Version > version {
			continue
		}
		m[k] = macEntry{Version: version, Value: slices.Clone(mu.ValueMAC)}
	}
	s.changed()
	return nil
}

func (s *Store) DeleteAppStateMutationMACs(_ context.Context, name string, indexMACs [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, im := range indexMACs {
		delete(s.data.MACs[name], hex.EncodeToString(im))
	}
	s.changed()
	return nil
}

func (s *Store) GetAppStateMutationMAC(_ context.Context, name string, indexMAC []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.MACs[name][hex.EncodeToString(indexMAC)].Value), nil
}

// Contacts.

func (s *Store) contact(user types.JID) types.ContactInfo { return s.data.Contacts[user.String()] }

func (s *Store) putContact(user types.JID, c types.ContactInfo) {
	c.Found = true
	s.data.Contacts[user.String()] = c
	s.changed()
}

func (s *Store) PutPushName(_ context.Context, user types.JID, name string) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.contact(user)
	if c.PushName == name {
		return false, "", nil
	}
	prev := c.PushName
	c.PushName = name
	s.putContact(user, c)
	return true, prev, nil
}

func (s *Store) PutBusinessName(_ context.Context, user types.JID, name string) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.contact(user)
	if c.BusinessName == name {
		return false, "", nil
	}
	prev := c.BusinessName
	c.BusinessName = name
	s.putContact(user, c)
	return true, prev, nil
}

func (s *Store) PutContactName(_ context.Context, user types.JID, firstName, fullName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.contact(user)
	if c.FirstName != firstName || c.FullName != fullName {
		c.FirstName, c.FullName = firstName, fullName
		s.putContact(user, c)
	}
	return nil
}

func (s *Store) PutAllContactNames(_ context.Context, contacts []store.ContactEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range contacts {
		c := s.contact(e.JID)
		c.FirstName, c.FullName = e.FirstName, e.FullName
		s.putContact(e.JID, c)
	}
	return nil
}

func (s *Store) PutManyRedactedPhones(_ context.Context, entries []store.RedactedPhoneEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		c := s.contact(e.JID)
		c.RedactedPhone = e.RedactedPhone
		s.putContact(e.JID, c)
	}
	return nil
}

func (s *Store) GetContact(_ context.Context, user types.JID) (types.ContactInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contact(user), nil
}

func (s *Store) GetAllContacts(_ context.Context) (map[types.JID]types.ContactInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[types.JID]types.ContactInfo, len(s.data.Contacts))
	for k, c := range s.data.Contacts {
		if j, err := types.ParseJID(k); err == nil {
			out[j] = c
		}
	}
	return out, nil
}

// Chat settings.

func (s *Store) editChat(chat types.JID, fn func(*chatSettings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.data.ChatSettings[chat.String()]
	fn(&c)
	s.data.ChatSettings[chat.String()] = c
	s.changed()
	return nil
}

func (s *Store) PutMutedUntil(_ context.Context, chat types.JID, until time.Time) error {
	return s.editChat(chat, func(c *chatSettings) {
		switch {
		case until == store.MutedForever:
			c.MutedUntil = -1
		case until.IsZero():
			c.MutedUntil = 0
		default:
			c.MutedUntil = until.Unix()
		}
	})
}

func (s *Store) PutPinned(_ context.Context, chat types.JID, pinned bool) error {
	return s.editChat(chat, func(c *chatSettings) { c.Pinned = pinned })
}

func (s *Store) PutArchived(_ context.Context, chat types.JID, archived bool) error {
	return s.editChat(chat, func(c *chatSettings) { c.Archived = archived })
}

func (s *Store) PutWASARootSecretID(_ context.Context, chat types.JID, id types.MessageID) error {
	return s.editChat(chat, func(c *chatSettings) { c.WASA = id })
}

func (s *Store) GetWASARootSecretID(_ context.Context, chat types.JID) (types.MessageID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.ChatSettings[chat.String()].WASA, nil
}

func (s *Store) GetChatSettings(_ context.Context, chat types.JID) (types.LocalChatSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data.ChatSettings[chat.String()]
	if !ok {
		return types.LocalChatSettings{}, nil
	}
	out := types.LocalChatSettings{Found: true, Pinned: c.Pinned, Archived: c.Archived}
	switch {
	case c.MutedUntil < 0:
		out.MutedUntil = store.MutedForever
	case c.MutedUntil > 0:
		out.MutedUntil = time.Unix(c.MutedUntil, 0)
	}
	return out, nil
}

// Message secrets (reactions, polls, edits), by chat, sender and id.

func secretKey(chat, sender types.JID, id types.MessageID) string {
	return chat.ToNonAD().String() + "|" + sender.ToNonAD().String() + "|" + id
}

func (s *Store) PutMessageSecrets(_ context.Context, inserts []store.MessageSecretInsert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range inserts {
		if in.Chat.IsEmpty() || in.Sender.IsEmpty() || in.ID == "" || len(in.Secret) == 0 {
			continue
		}
		s.putSecret(in.Chat, in.Sender, in.ID, in.Secret)
	}
	return nil
}

func (s *Store) putSecret(chat, sender types.JID, id types.MessageID, secret []byte) {
	k := secretKey(chat, sender, id)
	if _, ok := s.data.Secrets[k]; ok {
		return
	}
	s.data.Secrets[k] = msgSecret{Secret: slices.Clone(secret), Sender: sender.ToNonAD().String()}
	s.changed()
}

func (s *Store) PutMessageSecret(_ context.Context, chat, sender types.JID, id types.MessageID, secret []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putSecret(chat, sender, id, secret)
	return nil
}

// alts is a JID and its other form (phone number or LID), when it is known.
func (s *Store) alts(j types.JID) []types.JID {
	j = j.ToNonAD()
	out := []types.JID{j}
	switch j.Server {
	case types.HiddenUserServer:
		if pn := s.data.LIDToPN[j.User]; pn != "" {
			out = append(out, types.NewJID(pn, types.DefaultUserServer))
		}
	case types.DefaultUserServer:
		if lid := s.data.PNToLID[j.User]; lid != "" {
			out = append(out, types.NewJID(lid, types.HiddenUserServer))
		}
	}
	return out
}

func (s *Store) GetMessageSecret(_ context.Context, chat, sender types.JID, id types.MessageID) ([]byte, types.JID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.alts(chat) {
		for _, f := range s.alts(sender) {
			if sec, ok := s.data.Secrets[secretKey(c, f, id)]; ok {
				real, err := types.ParseJID(sec.Sender)
				return slices.Clone(sec.Secret), real, err
			}
		}
	}
	return nil, types.EmptyJID, nil
}

// Privacy tokens.

func (s *Store) PutPrivacyTokens(_ context.Context, tokens ...store.PrivacyToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range tokens {
		k := t.User.ToNonAD().String()
		old, had := s.data.Privacy[k]
		if had && t.Timestamp.Unix() < old.Timestamp {
			continue
		}
		p := privacyToken{Token: slices.Clone(t.Token), Timestamp: t.Timestamp.Unix(), Sender: old.Sender}
		if !t.SenderTimestamp.IsZero() {
			p.Sender = t.SenderTimestamp.Unix()
		}
		s.data.Privacy[k] = p
	}
	s.changed()
	return nil
}

func (s *Store) GetPrivacyToken(_ context.Context, user types.JID) (*store.PrivacyToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *store.PrivacyToken
	for _, j := range s.alts(user) {
		p, ok := s.data.Privacy[j.String()]
		if !ok || (best != nil && p.Timestamp <= best.Timestamp.Unix()) {
			continue
		}
		best = &store.PrivacyToken{User: user.ToNonAD(), Token: slices.Clone(p.Token), Timestamp: time.Unix(p.Timestamp, 0)}
		if p.Sender != 0 {
			best.SenderTimestamp = time.Unix(p.Sender, 0)
		}
	}
	return best, nil
}

func (s *Store) DeleteExpiredPrivacyTokens(_ context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for k, p := range s.data.Privacy {
		if p.Timestamp < cutoff.Unix() {
			delete(s.data.Privacy, k)
			n++
		}
	}
	if n > 0 {
		s.changed()
	}
	return n, nil
}

// The NCT salt.

func (s *Store) PutNCTSalt(_ context.Context, salt []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.NCTSalt = slices.Clone(salt)
	s.changed()
	return nil
}

func (s *Store) GetNCTSalt(_ context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.NCTSalt), nil
}

func (s *Store) DeleteNCTSalt(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.NCTSalt = nil
	s.changed()
	return nil
}

// The event buffer, kept in memory only: what it saves (a message decrypted twice after a
// restart) is not worth a write of the whole session for every message.

func (s *Store) GetBufferedEvent(_ context.Context, hash [32]byte) (*store.BufferedEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.buffered[hash]; ok {
		c := *e
		return &c, nil
	}
	return nil, nil
}

func (s *Store) PutBufferedEvent(_ context.Context, hash [32]byte, plaintext []byte, serverTime time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffered[hash] = &store.BufferedEvent{Plaintext: slices.Clone(plaintext), ServerTime: serverTime, InsertTime: time.Now()}
	return nil
}

func (s *Store) DoDecryptionTxn(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (s *Store) ClearBufferedEventPlaintext(_ context.Context, hash [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.buffered[hash]; ok {
		e.Plaintext = nil
	}
	return nil
}

func (s *Store) DeleteOldBufferedHashes(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	for h, e := range s.buffered {
		if e.InsertTime.Before(cutoff) {
			delete(s.buffered, h)
		}
	}
	return nil
}

func outgoingKey(chat types.JID, id types.MessageID) string { return chat.String() + "|" + id }

func (s *Store) GetOutgoingEvent(_ context.Context, chat, alt types.JID, id types.MessageID) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range []types.JID{chat, alt} {
		if e, ok := s.outgoing[outgoingKey(c, id)]; ok {
			return e.format, slices.Clone(e.data), nil
		}
	}
	return "", nil, errors.New("no such message")
}

func (s *Store) AddOutgoingEvent(_ context.Context, chat types.JID, id types.MessageID, format string, plaintext []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outgoing[outgoingKey(chat, id)] = outgoingEvent{format: format, data: slices.Clone(plaintext)}
	return nil
}

func (s *Store) DeleteOldOutgoingEvents(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.outgoing)
	return nil
}

// The map of LIDs (WhatsApp's hidden ids) to phone numbers.

func (s *Store) PutLIDMapping(_ context.Context, lid, pn types.JID) error {
	if lid.Server != types.HiddenUserServer || pn.Server != types.DefaultUserServer {
		return fmt.Errorf("invalid PutLIDMapping call %s/%s", lid, pn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLID(lid.User, pn.User)
	return nil
}

func (s *Store) putLID(lid, pn string) {
	if s.data.PNToLID[pn] == lid && s.data.LIDToPN[lid] == pn {
		return
	}
	if old := s.data.PNToLID[pn]; old != "" && old != lid {
		delete(s.data.LIDToPN, old)
	}
	if old := s.data.LIDToPN[lid]; old != "" && old != pn {
		delete(s.data.PNToLID, old)
	}
	s.data.PNToLID[pn] = lid
	s.data.LIDToPN[lid] = pn
	s.changed()
}

func (s *Store) PutManyLIDMappings(_ context.Context, mappings []store.LIDMapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range mappings {
		if m.LID.Server == types.HiddenUserServer && m.PN.Server == types.DefaultUserServer {
			s.putLID(m.LID.User, m.PN.User)
		}
	}
	return nil
}

func (s *Store) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if lid.Server != types.HiddenUserServer {
		return types.JID{}, fmt.Errorf("invalid GetPNForLID call with non-LID JID %s", lid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pn := s.data.LIDToPN[lid.User]
	if pn == "" {
		return types.JID{}, nil
	}
	return types.JID{User: pn, Device: lid.Device, Server: types.DefaultUserServer}, nil
}

func (s *Store) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	if pn.Server != types.DefaultUserServer {
		return types.JID{}, fmt.Errorf("invalid GetLIDForPN call with non-PN JID %s", pn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lid := s.data.PNToLID[pn.User]
	if lid == "" {
		return types.JID{}, nil
	}
	return types.JID{User: lid, Device: pn.Device, Server: types.HiddenUserServer}, nil
}

func (s *Store) GetManyLIDsForPNs(_ context.Context, pns []types.JID) (map[types.JID]types.JID, error) {
	if len(pns) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[types.JID]types.JID, len(pns))
	for _, pn := range pns {
		if pn.Server != types.DefaultUserServer {
			continue
		}
		if lid := s.data.PNToLID[pn.User]; lid != "" {
			out[pn] = types.JID{User: lid, Device: pn.Device, Server: types.HiddenUserServer}
		}
	}
	return out, nil
}
