package lineuparr

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlacementCollisionDoesNotMoveLegacyChoices(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	st, err := LoadStateStore(p)
	if err != nil {
		t.Fatal(err)
	}
	inc := false
	if err = st.Update("source", "raw", ChannelUpdate{Included: &inc}); err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st}
	a := InputChannel{Key: "raw", PlacementID: "raw", StationID: "station", Number: "2.1"}
	b := InputChannel{Key: "raw", PlacementID: "raw", StationID: "station", Number: "1002"}
	c := InputChannel{Key: "unique", PlacementID: "unique", StationID: "other", Number: "5"}
	first := []InputChannel{a, b, c}
	if err = s.StabilizePlacementKeys("source", first); err != nil {
		t.Fatal(err)
	}
	if first[0].Key == "raw" || first[0].Key == first[1].Key || !first[0].AmbiguousLegacyPlacement || first[2].Key != "unique" {
		t.Fatalf("bad keys %+v", first)
	}
	if saved := st.Snapshot("source"); saved["raw"].Included == nil || *saved["raw"].Included || len(saved) != 1 {
		t.Fatalf("legacy choices rewritten %+v", saved)
	}
	st, err = LoadStateStore(p)
	if err != nil {
		t.Fatal(err)
	}
	s.store = st
	reverse := []InputChannel{b, a}
	if err = s.StabilizePlacementKeys("source", reverse); err != nil {
		t.Fatal(err)
	}
	if reverse[0].Key != first[1].Key || reverse[1].Key != first[0].Key {
		t.Fatalf("order dependent %+v", reverse)
	}
	partial := []InputChannel{b}
	if err = s.StabilizePlacementKeys("source", partial); err != nil {
		t.Fatal(err)
	}
	if partial[0].Key != first[1].Key {
		t.Fatal("partial refresh reused old choice key")
	}
	other := []InputChannel{b}
	if err = s.StabilizePlacementKeys("other-source", other); err != nil {
		t.Fatal(err)
	}
	if other[0].Key != "raw" {
		t.Fatal("collision leaked across sources")
	}
}

func TestCollapsedPlacementExportCarriesSameRules(t *testing.T) {
	c := []DraftChannel{
		{ID: "one", Name: "WTEST", Number: "2.1", Category: "Entertainment", StationID: "s", PlacementID: "raw", Included: true, Aliases: []string{"east", "reject"}},
		{ID: "two", Name: "WTEST", Number: "1002", Category: "Entertainment", StationID: "s", PlacementID: "raw", Included: true, Aliases: []string{"west"}, ExcludedAliases: []string{"reject"}},
		{ID: "three", Name: "WTEST", Number: "3", Category: "Sports", StationID: "t", Included: true, Aliases: []string{"unrelated"}},
	}
	got := shareCollapsedPlacementRules(c)
	for _, i := range []int{0, 1} {
		if !reflect.DeepEqual(got[i].Aliases, []string{"east", "west"}) || !reflect.DeepEqual(got[i].ExcludedAliases, []string{"reject"}) || got[i].Number != c[i].Number {
			t.Fatalf("row %+v", got[i])
		}
	}
	rows := ExportFromDraft(&Draft{Channels: c}).Categories["Entertainment"]
	if len(rows) != 2 || rows[0].Number != int64(1002) || rows[1].Number != float64(2.1) || !reflect.DeepEqual(rows[0].Aliases, rows[1].Aliases) {
		t.Fatalf("consumer winner or rules: %+v", rows)
	}
	c[0].Included = false
	rows = ExportFromDraft(&Draft{Channels: c}).Categories["Entertainment"]
	if len(rows) != 1 || rows[0].Number != int64(1002) {
		t.Fatalf("explicit keeper ignored: %+v", rows)
	}
	c[0].Included = true
	if !reflect.DeepEqual(got[2], c[2]) || !reflect.DeepEqual(c[0].Aliases, []string{"east", "reject"}) {
		t.Fatal("unrelated row or draft changed")
	}
}
