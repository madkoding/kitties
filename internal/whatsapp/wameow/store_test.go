package wameow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

var ctx = context.Background()

func jid(s string) types.JID {
	j, err := types.ParseJID(s)
	if err != nil {
		panic(err)
	}
	return j
}

// paired is a store whose device was linked (as whatsmeow does after the QR is scanned).
func paired(t *testing.T) (*Store, *store.Device) {
	t.Helper()
	s, dev, err := Open(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != nil || dev.Initialized {
		t.Fatal("a new device is linked")
	}
	if _, err := s.Export(); err == nil {
		t.Fatal("a device not linked was exported")
	}
	id := jid("15550001:7@s.whatsapp.net")
	dev.ID = &id
	dev.LID = jid("999:7@lid")
	dev.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte("details"), AccountSignature: []byte("sig")}
	dev.PushName, dev.Platform, dev.BusinessName = "Kitties", "android", "biz"
	dev.FacebookUUID = uuid.New()
	if err := dev.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if !dev.Initialized || dev.Sessions == nil || dev.LIDs == nil {
		t.Fatal("the device has no stores after it was kept")
	}
	if !s.Dirty() || s.Dirty() {
		t.Fatal("Dirty does not report a change once")
	}
	return s, dev
}

func reopen(t *testing.T, s *Store) (*Store, *store.Device) {
	t.Helper()
	blob, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	s2, dev, err := Open(blob, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s2, dev
}

func TestDeviceRoundTrip(t *testing.T) {
	s, dev := paired(t)
	s2, dev2 := reopen(t, s)
	if dev2.ID.String() != dev.ID.String() || dev2.LID != dev.LID || dev2.PushName != "Kitties" || dev2.Platform != "android" ||
		dev2.BusinessName != "biz" || dev2.FacebookUUID != dev.FacebookUUID || dev2.RegistrationID != dev.RegistrationID {
		t.Fatalf("device = %+v", dev2)
	}
	if *dev2.NoiseKey.Pub != *dev.NoiseKey.Pub || *dev2.IdentityKey.Priv != *dev.IdentityKey.Priv ||
		*dev2.SignedPreKey.Signature != *dev.SignedPreKey.Signature || dev2.SignedPreKey.KeyID != dev.SignedPreKey.KeyID {
		t.Fatal("the keys changed")
	}
	if string(dev2.Account.Details) != "details" || string(dev2.AdvSecretKey) != string(dev.AdvSecretKey) {
		t.Fatal("the account changed")
	}
	if dev2.Container != s2 || !dev2.Initialized {
		t.Fatal("the device is not on its store")
	}
	if err := dev2.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Export(); err == nil {
		t.Fatal("a deleted device was exported")
	}
}

func TestOpenRejectsDamage(t *testing.T) {
	s, _ := paired(t)
	blob, _ := s.Export()
	edit := func(fn func(d map[string]any)) []byte {
		var m map[string]any
		_ = json.Unmarshal(blob, &m)
		fn(m["device"].(map[string]any))
		out, _ := json.Marshal(m)
		return out
	}
	for name, bad := range map[string][]byte{
		"json":     []byte("{"),
		"version":  []byte(`{"v":9,"device":{}}`),
		"nodevice": []byte(`{"v":1}`),
		"noise":    edit(func(d map[string]any) { d["noise"] = "AAAA" }),
		"identity": edit(func(d map[string]any) { d["identity"] = "AAAA" }),
		"prekey":   edit(func(d map[string]any) { d["pre_key"] = "AAAA" }),
		"sig":      edit(func(d map[string]any) { d["pre_key_sig"] = "AAAA" }),
		"id":       edit(func(d map[string]any) { d["id"] = "a:x@s.whatsapp.net" }),
		"lid":      edit(func(d map[string]any) { d["lid"] = "a:x@s.whatsapp.net" }),
		"account":  edit(func(d map[string]any) { d["account"] = "/w==" }),
	} {
		if _, _, err := Open(bad, nil); err == nil {
			t.Fatalf("%s: a damaged session was opened", name)
		}
	}
	if err := s.PutDevice(ctx, &store.Device{}); err == nil {
		t.Fatal("a device without an id was kept")
	}
}

func TestIdentitiesSessionsAndMigration(t *testing.T) {
	s, _ := paired(t)
	k1, k2 := [32]byte{1}, [32]byte{2}
	if ok, _ := s.IsTrustedIdentity(ctx, "111:0", k1); !ok {
		t.Fatal("an unknown identity is not trusted")
	}
	_ = s.PutIdentity(ctx, "111:0", k1)
	_ = s.PutIdentity(ctx, "111:1", k1)
	_ = s.PutIdentity(ctx, "222:0", k2)
	if ok, _ := s.IsTrustedIdentity(ctx, "111:0", k2); ok {
		t.Fatal("a changed identity is trusted")
	}
	if ok, _ := s.IsTrustedIdentity(ctx, "111:0", k1); !ok {
		t.Fatal("the known identity is not trusted")
	}
	s.data.Identities["bad:0"] = []byte{1}
	if _, err := s.IsTrustedIdentity(ctx, "bad:0", k1); err == nil {
		t.Fatal("a damaged identity was read")
	}
	_ = s.DeleteIdentity(ctx, "bad:0")
	_ = s.DeleteAllIdentities(ctx, "222")
	if len(s.data.Identities) != 2 {
		t.Fatalf("identities = %v", s.data.Identities)
	}

	_ = s.PutSession(ctx, "111:0", []byte("a"))
	_ = s.PutManySessions(ctx, map[string][]byte{"111:1": []byte("b"), "333:0": []byte("c")})
	if has, _ := s.HasSession(ctx, "111:1"); !has {
		t.Fatal("no session")
	}
	if got, _ := s.GetSession(ctx, "333:0"); string(got) != "c" {
		t.Fatalf("session = %q", got)
	}
	many, _ := s.GetManySessions(ctx, []string{"111:0", "nope"})
	if string(many["111:0"]) != "a" || many["nope"] != nil || len(many) != 2 {
		t.Fatalf("many = %v", many)
	}
	if m, _ := s.GetManySessions(ctx, nil); m != nil {
		t.Fatal("sessions for nothing")
	}
	_ = s.PutSenderKey(ctx, "group@g.us", "111:0", []byte("sk"))
	pn, lid := jid("111@s.whatsapp.net"), jid("777@lid")
	if err := s.MigratePNToLID(ctx, pn, lid); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSession(ctx, "777_1:0"); string(got) != "a" {
		t.Fatalf("migrated session = %q (%v)", got, s.data.Sessions)
	}
	if got, _ := s.GetSenderKey(ctx, "group@g.us", "777_1:0"); string(got) != "sk" {
		t.Fatalf("migrated sender key = %q (%v)", got, s.data.SenderKeys)
	}
	if has, _ := s.HasSession(ctx, "111:0"); has {
		t.Fatal("the old session stayed")
	}
	if _, ok := s.data.Identities["777_1:1"]; !ok {
		t.Fatalf("identities not migrated: %v", s.data.Identities)
	}
	_ = s.DeleteSession(ctx, "333:0")
	_ = s.DeleteAllSessions(ctx, "777_1")
	if len(s.data.Sessions) != 0 {
		t.Fatalf("sessions = %v", s.data.Sessions)
	}
	s2, _ := reopen(t, s)
	if got, _ := s2.GetSenderKey(ctx, "group@g.us", "777_1:0"); string(got) != "sk" {
		t.Fatal("the sender key was not kept")
	}
}

func TestPreKeys(t *testing.T) {
	s, _ := paired(t)
	keys, err := s.GetOrGenPreKeys(ctx, 3)
	if err != nil || len(keys) != 3 || keys[0].KeyID != 1 || keys[2].KeyID != 3 {
		t.Fatalf("keys = %v %v", keys, err)
	}
	again, _ := s.GetOrGenPreKeys(ctx, 2)
	if again[0].KeyID != 1 || *again[0].Priv != *keys[0].Priv {
		t.Fatal("the keys not uploaded were not handed again")
	}
	_ = s.MarkPreKeysAsUploaded(ctx, 2)
	if n, _ := s.UploadedPreKeyCount(ctx); n != 2 {
		t.Fatalf("uploaded = %d", n)
	}
	more, _ := s.GetOrGenPreKeys(ctx, 2)
	if more[0].KeyID != 3 || more[1].KeyID != 4 {
		t.Fatalf("more = %d %d", more[0].KeyID, more[1].KeyID)
	}
	one, _ := s.GenOnePreKey(ctx)
	if one.KeyID != 5 {
		t.Fatalf("one = %d", one.KeyID)
	}
	if got, _ := s.GetPreKey(ctx, 2); got == nil || *got.Priv != *keys[1].Priv {
		t.Fatal("the pre-key changed")
	}
	_ = s.RemovePreKey(ctx, 2)
	if got, _ := s.GetPreKey(ctx, 2); got != nil {
		t.Fatal("a removed pre-key is there")
	}
	s.data.PreKeys[9] = preKeyData{Priv: []byte{1}}
	if _, err := s.GetPreKey(ctx, 9); err == nil {
		t.Fatal("a damaged pre-key was read")
	}
	if _, err := s.GetOrGenPreKeys(ctx, 9); err == nil {
		t.Fatal("a damaged pre-key was handed")
	}
}

func TestAppState(t *testing.T) {
	s, _ := paired(t)
	_ = s.PutAppStateSyncKey(ctx, []byte{1}, store.AppStateSyncKey{Data: []byte("old"), Timestamp: 10})
	_ = s.PutAppStateSyncKey(ctx, []byte{2}, store.AppStateSyncKey{Data: []byte("new"), Timestamp: 20})
	_ = s.PutAppStateSyncKey(ctx, []byte{3}, store.AppStateSyncKey{Data: []byte("same"), Timestamp: 20})
	_ = s.PutAppStateSyncKey(ctx, []byte{1}, store.AppStateSyncKey{Data: []byte("older"), Timestamp: 5})
	if k, _ := s.GetAppStateSyncKey(ctx, []byte{1}); string(k.Data) != "old" {
		t.Fatalf("an older key replaced a newer one: %q", k.Data)
	}
	if k, _ := s.GetAppStateSyncKey(ctx, []byte{9}); k != nil {
		t.Fatal("a key that is not there")
	}
	if id, _ := s.GetLatestAppStateSyncKeyID(ctx); len(id) != 1 || id[0] != 2 {
		t.Fatalf("latest = %v", id)
	}
	all, _ := s.GetAllAppStateSyncKeys(ctx)
	if len(all) != 3 || string(all[2].Data) != "old" {
		t.Fatalf("all = %v", all)
	}
	empty := newStore(nil)
	if id, _ := empty.GetLatestAppStateSyncKeyID(ctx); id != nil {
		t.Fatal("a latest key in an empty store")
	}

	if v, h, err := s.GetAppStateVersion(ctx, "regular"); v != 0 || h != [128]byte{} || err != nil {
		t.Fatal("a version that is not there")
	}
	_ = s.PutAppStateVersion(ctx, "regular", 4, [128]byte{7})
	if v, h, _ := s.GetAppStateVersion(ctx, "regular"); v != 4 || h[0] != 7 {
		t.Fatal("the version changed")
	}
	s.data.AppStates["bad"] = appState{Version: 1, Hash: []byte{1}}
	if _, _, err := s.GetAppStateVersion(ctx, "bad"); err == nil {
		t.Fatal("a damaged version was read")
	}
	_ = s.DeleteAppStateVersion(ctx, "regular")

	_ = s.PutAppStateMutationMACs(ctx, "regular", 2, []store.AppStateMutationMAC{{IndexMAC: []byte{1}, ValueMAC: []byte("v2")}})
	_ = s.PutAppStateMutationMACs(ctx, "regular", 1, []store.AppStateMutationMAC{{IndexMAC: []byte{1}, ValueMAC: []byte("v1")}, {IndexMAC: []byte{2}, ValueMAC: []byte("w")}})
	_ = s.PutAppStateMutationMACs(ctx, "regular", 3, nil)
	if v, _ := s.GetAppStateMutationMAC(ctx, "regular", []byte{1}); string(v) != "v2" {
		t.Fatalf("mac = %q", v)
	}
	_ = s.DeleteAppStateMutationMACs(ctx, "regular", [][]byte{{1}})
	if v, _ := s.GetAppStateMutationMAC(ctx, "regular", []byte{1}); v != nil {
		t.Fatal("a deleted mac is there")
	}
	if v, _ := s.GetAppStateMutationMAC(ctx, "regular", []byte{2}); string(v) != "w" {
		t.Fatal("the other mac went")
	}
}

func TestContactsAndChats(t *testing.T) {
	s, _ := paired(t)
	u := jid("111@s.whatsapp.net")
	if changed, _, _ := s.PutPushName(ctx, u, "Mad"); !changed {
		t.Fatal("a new push name is not a change")
	}
	if changed, _, _ := s.PutPushName(ctx, u, "Mad"); changed {
		t.Fatal("the same push name is a change")
	}
	if changed, prev, _ := s.PutPushName(ctx, u, "Madko"); !changed || prev != "Mad" {
		t.Fatal("the previous push name is lost")
	}
	if changed, _, _ := s.PutBusinessName(ctx, u, "Biz"); !changed {
		t.Fatal("a business name is not a change")
	}
	if changed, _, _ := s.PutBusinessName(ctx, u, "Biz"); changed {
		t.Fatal("the same business name is a change")
	}
	_ = s.PutContactName(ctx, u, "M", "Mad K")
	_ = s.PutContactName(ctx, u, "M", "Mad K")
	_ = s.PutAllContactNames(ctx, []store.ContactEntry{{JID: jid("222@s.whatsapp.net"), FirstName: "B", FullName: "Bo"}})
	_ = s.PutManyRedactedPhones(ctx, []store.RedactedPhoneEntry{{JID: u, RedactedPhone: "+1 ***"}})
	c, _ := s.GetContact(ctx, u)
	if !c.Found || c.PushName != "Madko" || c.BusinessName != "Biz" || c.FullName != "Mad K" || c.RedactedPhone != "+1 ***" {
		t.Fatalf("contact = %+v", c)
	}
	if c, _ := s.GetContact(ctx, jid("333@s.whatsapp.net")); c.Found {
		t.Fatal("an unknown contact is found")
	}
	s.data.Contacts["1.2.3@s.whatsapp.net"] = c
	all, _ := s.GetAllContacts(ctx)
	if len(all) != 2 || all[jid("222@s.whatsapp.net")].FullName != "Bo" {
		t.Fatalf("all = %v", all)
	}

	ch := jid("111@s.whatsapp.net")
	if cs, _ := s.GetChatSettings(ctx, ch); cs.Found {
		t.Fatal("settings of an unknown chat")
	}
	_ = s.PutMutedUntil(ctx, ch, store.MutedForever)
	_ = s.PutPinned(ctx, ch, true)
	_ = s.PutArchived(ctx, ch, true)
	_ = s.PutWASARootSecretID(ctx, ch, "root")
	cs, _ := s.GetChatSettings(ctx, ch)
	if !cs.Found || !cs.Pinned || !cs.Archived || cs.MutedUntil != store.MutedForever {
		t.Fatalf("settings = %+v", cs)
	}
	if id, _ := s.GetWASARootSecretID(ctx, ch); id != "root" {
		t.Fatal("the root secret changed")
	}
	until := time.Unix(1_900_000_000, 0)
	_ = s.PutMutedUntil(ctx, ch, until)
	if cs, _ := s.GetChatSettings(ctx, ch); !cs.MutedUntil.Equal(until) {
		t.Fatalf("muted = %v", cs.MutedUntil)
	}
	_ = s.PutMutedUntil(ctx, ch, time.Time{})
	if cs, _ := s.GetChatSettings(ctx, ch); !cs.MutedUntil.IsZero() {
		t.Fatal("still muted")
	}
}

func TestSecretsPrivacyAndLIDs(t *testing.T) {
	s, dev := paired(t)
	pn, lid := jid("111@s.whatsapp.net"), jid("777@lid")
	if err := s.PutLIDMapping(ctx, pn, lid); err == nil {
		t.Fatal("a mapping the wrong way round was kept")
	}
	_ = s.PutLIDMapping(ctx, lid, pn)
	_ = s.PutLIDMapping(ctx, lid, pn)
	_ = s.PutManyLIDMappings(ctx, []store.LIDMapping{{LID: jid("888@lid"), PN: jid("222@s.whatsapp.net")}, {LID: pn, PN: lid}})
	if got, _ := s.GetPNForLID(ctx, jid("777:3@lid")); got.String() != "111:3@s.whatsapp.net" {
		t.Fatalf("pn = %s", got)
	}
	if got, _ := s.GetLIDForPN(ctx, jid("222@s.whatsapp.net")); got.String() != "888@lid" {
		t.Fatalf("lid = %s", got)
	}
	if got, _ := dev.GetAltJID(ctx, pn); got != lid {
		t.Fatalf("alt = %s", got)
	}
	if _, err := s.GetPNForLID(ctx, pn); err == nil {
		t.Fatal("a PN was looked up as a LID")
	}
	if _, err := s.GetLIDForPN(ctx, lid); err == nil {
		t.Fatal("a LID was looked up as a PN")
	}
	if got, _ := s.GetPNForLID(ctx, jid("1@lid")); !got.IsEmpty() {
		t.Fatal("an unknown LID has a PN")
	}
	if got, _ := s.GetLIDForPN(ctx, jid("1@s.whatsapp.net")); !got.IsEmpty() {
		t.Fatal("an unknown PN has a LID")
	}
	many, _ := s.GetManyLIDsForPNs(ctx, []types.JID{pn, jid("1@s.whatsapp.net"), lid})
	if len(many) != 1 || many[pn] != lid {
		t.Fatalf("many = %v", many)
	}
	if m, _ := s.GetManyLIDsForPNs(ctx, nil); m != nil {
		t.Fatal("lids for nothing")
	}
	// A number that moves to another LID loses the old one.
	_ = s.PutLIDMapping(ctx, jid("999@lid"), pn)
	if got, _ := s.GetPNForLID(ctx, lid); !got.IsEmpty() {
		t.Fatal("the old LID still maps")
	}
	_ = s.PutLIDMapping(ctx, jid("999@lid"), jid("333@s.whatsapp.net"))
	if got, _ := s.GetLIDForPN(ctx, pn); !got.IsEmpty() {
		t.Fatal("the old PN still maps")
	}
	_ = s.PutLIDMapping(ctx, lid, pn)

	// Secrets are found through the other form of the chat or the sender.
	_ = s.PutMessageSecrets(ctx, []store.MessageSecretInsert{{Chat: pn, Sender: pn, ID: "m1", Secret: []byte("s1")}, {Chat: pn}})
	_ = s.PutMessageSecret(ctx, lid, lid, "m2", []byte("s2"))
	_ = s.PutMessageSecret(ctx, lid, lid, "m2", []byte("other"))
	if sec, real, _ := s.GetMessageSecret(ctx, lid, lid, "m1"); string(sec) != "s1" || real != pn {
		t.Fatalf("secret = %q %s", sec, real)
	}
	if sec, _, _ := s.GetMessageSecret(ctx, pn, pn, "m2"); string(sec) != "s2" {
		t.Fatalf("secret 2 = %q", sec)
	}
	if sec, _, _ := s.GetMessageSecret(ctx, pn, pn, "nope"); sec != nil {
		t.Fatal("a secret that is not there")
	}

	now := time.Unix(1_800_000_000, 0)
	_ = s.PutPrivacyTokens(ctx, store.PrivacyToken{User: pn, Token: []byte("t1"), Timestamp: now, SenderTimestamp: now})
	_ = s.PutPrivacyTokens(ctx, store.PrivacyToken{User: pn, Token: []byte("t0"), Timestamp: now.Add(-time.Hour)})
	_ = s.PutPrivacyTokens(ctx, store.PrivacyToken{User: lid, Token: []byte("t2"), Timestamp: now.Add(time.Hour)})
	if tok, _ := s.GetPrivacyToken(ctx, pn); string(tok.Token) != "t2" {
		t.Fatalf("token = %q", tok.Token)
	}
	if tok, _ := s.GetPrivacyToken(ctx, jid("5@s.whatsapp.net")); tok != nil {
		t.Fatal("a token that is not there")
	}
	_ = s.PutPrivacyTokens(ctx, store.PrivacyToken{User: pn, Token: []byte("t3"), Timestamp: now.Add(time.Minute)})
	if tok, _ := s.GetPrivacyToken(ctx, jid("111@s.whatsapp.net")); tok.SenderTimestamp.IsZero() && string(tok.Token) == "t3" {
		t.Fatal("the sender time was lost")
	}
	if n, _ := s.DeleteExpiredPrivacyTokens(ctx, now.Add(30*time.Minute)); n != 1 {
		t.Fatalf("expired = %d", n)
	}
	if n, _ := s.DeleteExpiredPrivacyTokens(ctx, now); n != 0 {
		t.Fatalf("expired again = %d", n)
	}

	_ = s.PutNCTSalt(ctx, []byte("salt"))
	if got, _ := s.GetNCTSalt(ctx); string(got) != "salt" {
		t.Fatal("no salt")
	}
	s2, _ := reopen(t, s)
	if got, _ := s2.GetPNForLID(ctx, lid); got != pn {
		t.Fatal("the LID map was not kept")
	}
	if got, _ := s2.GetNCTSalt(ctx); string(got) != "salt" {
		t.Fatal("the salt was not kept")
	}
	_ = s.DeleteNCTSalt(ctx)
	if got, _ := s.GetNCTSalt(ctx); got != nil {
		t.Fatal("the salt stayed")
	}
}

func TestBuffers(t *testing.T) {
	s := newStore(nil)
	h := [32]byte{1}
	if e, _ := s.GetBufferedEvent(ctx, h); e != nil {
		t.Fatal("a buffered event that is not there")
	}
	_ = s.PutBufferedEvent(ctx, h, []byte("plain"), time.Now())
	if e, _ := s.GetBufferedEvent(ctx, h); string(e.Plaintext) != "plain" {
		t.Fatal("the buffered event changed")
	}
	_ = s.ClearBufferedEventPlaintext(ctx, h)
	if e, _ := s.GetBufferedEvent(ctx, h); e.Plaintext != nil {
		t.Fatal("the plaintext stayed")
	}
	s.buffered[[32]byte{2}] = &store.BufferedEvent{InsertTime: time.Now().Add(-15 * 24 * time.Hour)}
	_ = s.DeleteOldBufferedHashes(ctx)
	if len(s.buffered) != 1 {
		t.Fatalf("buffered = %d", len(s.buffered))
	}
	ran := false
	_ = s.DoDecryptionTxn(ctx, func(context.Context) error { ran = true; return nil })
	if !ran {
		t.Fatal("the transaction did not run")
	}
	chat, alt := jid("111@s.whatsapp.net"), jid("777@lid")
	_ = s.AddOutgoingEvent(ctx, alt, "m1", "proto", []byte("x"))
	if f, d, err := s.GetOutgoingEvent(ctx, chat, alt, "m1"); err != nil || f != "proto" || string(d) != "x" {
		t.Fatalf("outgoing = %q %q %v", f, d, err)
	}
	_ = s.DeleteOldOutgoingEvents(ctx)
	if _, _, err := s.GetOutgoingEvent(ctx, chat, alt, "m1"); err == nil {
		t.Fatal("a deleted outgoing event is there")
	}
}
