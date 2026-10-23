package lineuparr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func TestCapturedSourcesPreserveReferenceBytesAndCurrentChoicesWithoutAcquisition(t *testing.T) {
	svc := newTestService(t, `{"package":"Reference","categories":{"Movies":[{"name":"TWO","number":2,"aliases":["Reference Label"]}]}}`, "")
	lineup := testContext("source")
	inputs := []InputChannel{{Key: "two", StationID: "station", Number: "2", CallSign: "TWO"}}
	before, err := svc.Build(context.Background(), lineup, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if before.SourceCapture == nil {
		t.Fatal("source capture missing")
	}
	if err := os.RemoveAll(svc.options.CacheDir); err != nil {
		t.Fatal(err)
	}
	acquired := 0
	svc.options.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { acquired++; return nil, errors.New("must not acquire") })}
	after, err := svc.BuildCaptured(context.Background(), lineup, inputs, before.SourceCapture)
	if err != nil {
		t.Fatal(err)
	}
	if acquired != 0 || !reflect.DeepEqual(ExportFromDraft(before), ExportFromDraft(after)) {
		t.Fatal("captured reference/export changed", acquired)
	}
	encoded, _ := json.Marshal(after)
	var decoded Draft
	json.Unmarshal(encoded, &decoded)
	if decoded.SourceCapture != nil {
		t.Fatal("private source bytes serialized")
	}
	included := false
	if err := svc.UpdateChannel("source", "two", ChannelUpdate{Included: &included}); err != nil {
		t.Fatal(err)
	}
	changed, err := svc.BuildCaptured(context.Background(), lineup, inputs, before.SourceCapture)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Channels[0].Included || acquired != 0 {
		t.Fatal("captured replay ignored current choice or acquired source")
	}
	if _, err := svc.BuildCaptured(context.Background(), lineup, inputs, nil); err == nil {
		t.Fatal("absent capture accepted")
	}
}
