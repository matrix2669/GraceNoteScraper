package appconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateAddressPersistenceAndProviderChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, _ := LoadStore(path)
	config := Config{Version: CurrentVersion, Gracenote: GracenoteConfig{Country: "USA", PostalCode: "33308", Language: "en-us", Device: "X", LineupID: "L1", HeadendID: "H1", ProviderName: "Xfinity"}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	address := json.RawMessage(`{"streetAddress":"1 Test Street"}`)
	if err := store.SaveAddress(config.Fingerprint(), address); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + ".address.json")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("address is not private", err)
	}
	restored, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.Address(config.Fingerprint())
	if err != nil || string(got) != string(address) {
		t.Fatal("address did not survive reload", err)
	}
	if err := restored.Save(config); err != nil {
		t.Fatal(err)
	}
	if got, _ := restored.Address(config.Fingerprint()); len(got) == 0 {
		t.Fatal("same provider erased address")
	}
	old := config
	config.Gracenote.LineupID = "L2"
	if err := restored.Save(config); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".address.json"); !os.IsNotExist(err) {
		t.Fatal("address file survived provider change")
	}
	if err := restored.SaveAddress(old.Fingerprint(), address); err == nil {
		t.Fatal("accepted stale browser")
	}
	if err := restored.Save(old); err != nil {
		t.Fatal(err)
	}
	if got, _ := restored.Address(old.Fingerprint()); len(got) > 0 {
		t.Fatal("old address reappeared")
	}
}
