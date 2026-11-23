package dispatcharr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type lifecycleFixture struct {
	version                                 string
	level                                   int
	accountBody, streamBody, historicalBody string
	calls                                   map[string]int
	hook                                    func(*http.Request, int) (int, string, bool)
}

func (f *lifecycleFixture) client() *Client {
	f.calls = map[string]int{}
	return NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.calls[r.URL.Path]++
		n := f.calls[r.URL.Path]
		if f.hook != nil {
			if code, body, ok := f.hook(r, n); ok {
				return testResponse(r, code, body), nil
			}
		}
		switch r.URL.Path {
		case "/api/core/version/":
			return testResponse(r, 200, fmt.Sprintf(`{"version":%q}`, f.version)), nil
		case "/api/accounts/users/me/":
			return testResponse(r, 200, fmt.Sprintf(`{"id":7,"user_level":%d,"api_key":"do-not-retain"}`, f.level)), nil
		case "/api/m3u/accounts/":
			return testResponse(r, 200, f.accountBody), nil
		case "/api/channels/streams/":
			return testResponse(r, 200, f.streamBody), nil
		case "/api/channels/streams/by-ids/":
			return testResponse(r, 200, f.historicalBody), nil
		}
		return testResponse(r, 404, ""), nil
	})})
}
func lifecycleGood() *lifecycleFixture {
	return &lifecycleFixture{version: "0.33.0", level: 10, accountBody: `[{"id":1,"is_active":true,"status":"success","username":"secret"}]`, streamBody: `{"count":2,"next":null,"results":[{"id":2,"m3u_account":null,"name":" Manual ","url":"secret"},{"id":1,"m3u_account":1,"name":"FX","is_stale":true}]}`, historicalBody: `[{"id":1,"m3u_account":1,"name":"FX"}]`}
}
func lifecycleConfig() Config {
	return Config{BaseURL: "https://dispatcharr.test", AuthMethod: AuthAPIKey, APIKey: "key"}
}
func TestLifecycleCompleteTrustedVersionsSafeAndImmutable(t *testing.T) {
	for _, version := range []string{"0.32.0", "0.33.0"} {
		t.Run(version, func(t *testing.T) {
			f := lifecycleGood()
			f.version = version
			c := f.client()
			r, err := c.CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 1, AccountID: 1}})
			if err != nil || r.Coverage() != LifecycleCoverageComplete {
				t.Fatalf("receipt %s %v", r.Coverage(), err)
			}
			if len(r.Streams()) != 2 || r.Streams()[1].AccountID != 0 || r.Streams()[1].Name != " Manual " || r.InventoryDigest() == "" || r.AccountDigest() == "" || r.ServerVersion() != version {
				t.Fatalf("safe receipt %+v", r)
			}
			encoded, _ := json.Marshal(r.Streams())
			if strings.Contains(string(encoded), "secret") {
				t.Fatal("secret retained")
			}
			refs := r.References()
			refs[0].StreamID = 99
			if r.References()[0].StreamID != 1 {
				t.Fatal("mutable references")
			}
			rows := r.Streams()
			rows[0].Name = "changed"
			if r.Streams()[0].Name == "changed" {
				t.Fatal("mutable receipt")
			}
			if f.calls["/api/channels/streams/"] != 2 || f.calls["/api/m3u/accounts/"] != 4 {
				t.Fatalf("capture count %+v", f.calls)
			}
		})
	}
}
func TestLifecycleInactivePreservesPositiveByIDButDefersAbsence(t *testing.T) {
	f := lifecycleGood()
	f.accountBody = `[{"id":1,"is_active":true,"status":"success"},{"id":9,"is_active":false,"status":"disabled"}]`
	f.historicalBody = `[{"id":8,"m3u_account":9,"name":"Hidden"}]`
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 8, AccountID: 9}, {StreamID: 99, AccountID: 9}})
	if e != nil || r.Coverage() != LifecycleCoveragePresenceOnly || len(r.HistoricalRows()) != 1 || r.HistoricalRows()[0].ID != 8 {
		t.Fatalf("receipt %+v %v", r, e)
	}
}
func TestLifecycleFailsClosedOnMalformedOrAmbiguousInputs(t *testing.T) {
	cases := []struct {
		name   string
		change func(*lifecycleFixture)
	}{
		{"unknown version", func(f *lifecycleFixture) { f.version = "0.34.0" }}, {"nonadmin", func(f *lifecycleFixture) { f.level = 9 }},
		{"missing account status", func(f *lifecycleFixture) { f.accountBody = `[{"id":1}]` }}, {"duplicate account", func(f *lifecycleFixture) {
			f.accountBody = `[{"id":1,"is_active":true,"status":"success"},{"id":1,"is_active":true,"status":"success"}]`
		}},
		{"missing count", func(f *lifecycleFixture) { f.streamBody = `{"next":null,"results":[]}` }}, {"missing next", func(f *lifecycleFixture) { f.streamBody = `{"count":0,"results":[]}` }},
		{"null results", func(f *lifecycleFixture) { f.streamBody = `{"count":0,"next":null,"results":null}` }}, {"premature", func(f *lifecycleFixture) {
			f.streamBody = `{"count":2,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX"}]}`
		}},
		{"duplicate stream", func(f *lifecycleFixture) {
			f.streamBody = `{"count":2,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX"},{"id":1,"m3u_account":1,"name":"FX"}]}`
		}},
		{"missing account field", func(f *lifecycleFixture) { f.streamBody = `{"count":1,"next":null,"results":[{"id":1,"name":"FX"}]}` }}, {"unlisted account", func(f *lifecycleFixture) {
			f.streamBody = `{"count":1,"next":null,"results":[{"id":1,"m3u_account":9,"name":"FX"}]}`
		}},
		{"unexpected historical", func(f *lifecycleFixture) { f.historicalBody = `[{"id":9,"m3u_account":1,"name":"FX"}]` }},
		{"cross origin next", func(f *lifecycleFixture) {
			f.streamBody = `{"count":2,"next":"https://evil.test/api/channels/streams/?page=2","results":[{"id":1,"m3u_account":1,"name":"FX"}]}`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := lifecycleGood()
			tc.change(f)
			r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 1, AccountID: 1}})
			if e == nil || r.Coverage() != LifecycleCoverageUnavailable || len(r.Streams()) != 0 {
				t.Fatalf("not failed closed %s %v", r.Coverage(), e)
			}
		})
	}
	f := lifecycleGood()
	f.streamBody = `{"count":0,"next":null,"results":[]}`
	f.historicalBody = `[]`
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
	if e != nil || r.Coverage() != LifecycleCoveragePresenceOnly {
		t.Fatalf("empty must defer %s %v", r.Coverage(), e)
	}
}
func TestLifecycleDetectsChurnAndAuthLoss(t *testing.T) {
	for _, kind := range []string{"name", "id", "principal", "account", "auth"} {
		t.Run(kind, func(t *testing.T) {
			f := lifecycleGood()
			f.hook = func(r *http.Request, n int) (int, string, bool) {
				switch kind {
				case "name":
					if r.URL.Path == "/api/channels/streams/" && n == 2 {
						return 200, strings.Replace(f.streamBody, `"FX"`, `"renamed"`, 1), true
					}
				case "id":
					if r.URL.Path == "/api/channels/streams/" && n == 2 {
						return 200, strings.Replace(f.streamBody, `"id":2`, `"id":3`, 1), true
					}
				case "principal":
					if r.URL.Path == "/api/accounts/users/me/" && n == 2 {
						return 200, `{"id":8,"user_level":10}`, true
					}
				case "account":
					if r.URL.Path == "/api/m3u/accounts/" && n == 2 {
						return 200, `[{"id":1,"is_active":false,"status":"disabled"}]`, true
					}
				case "auth":
					if r.URL.Path == "/api/channels/streams/" {
						return 403, "secret", true
					}
				}
				return 0, "", false
			}
			r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
			if e == nil || r.Coverage() != LifecycleCoverageUnavailable || strings.Contains(e.Error(), "secret") {
				t.Fatalf("not deferred %v %v", r.Coverage(), e)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := lifecycleGood()
	f.hook = func(r *http.Request, n int) (int, string, bool) {
		if r.Context().Err() != nil {
			return 500, "", true
		}
		return 0, "", false
	}
	r, e := f.client().CaptureLifecycleInventory(ctx, lifecycleConfig(), nil)
	if e == nil || r.Coverage() != LifecycleCoverageUnavailable {
		t.Fatal("cancel accepted")
	}
}

func TestLifecyclePaginationAndCanonicalOrder(t *testing.T) {
	f := lifecycleGood()
	f.hook = func(r *http.Request, n int) (int, string, bool) {
		if r.URL.Path != "/api/channels/streams/" {
			return 0, "", false
		}
		if r.URL.Query().Get("hide_stale") != "false" || r.URL.Query().Get("m3u_account_is_active") != "" {
			t.Error("filtered stale or inactive scope")
		}
		if r.URL.Query().Get("page") == "1" {
			return 200, `{"count":2,"next":"https://dispatcharr.test/api/channels/streams/?page=2","results":[{"id":2,"m3u_account":null,"name":" Manual "}]}`, true
		}
		return 200, `{"count":2,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX"}]}`, true
	}
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 1, AccountID: 1}})
	if e != nil || r.Coverage() != LifecycleCoverageComplete || r.PageCount() != 2 || len(r.Streams()) != 2 {
		t.Fatalf("pagination %s %v", r.Coverage(), e)
	}
}
func TestLifecyclePaginationCountDriftDuplicateAndLimit(t *testing.T) {
	for _, kind := range []string{"count", "duplicate", "limit"} {
		t.Run(kind, func(t *testing.T) {
			f := lifecycleGood()
			f.hook = func(r *http.Request, n int) (int, string, bool) {
				if r.URL.Path != "/api/channels/streams/" {
					return 0, "", false
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if kind == "limit" {
					return 200, fmt.Sprintf(`{"count":101,"next":"https://dispatcharr.test/api/channels/streams/?page=%d","results":[{"id":%d,"m3u_account":1,"name":"FX"}]}`, page+1, page), true
				}
				if page == 1 {
					return 200, `{"count":2,"next":"https://dispatcharr.test/api/channels/streams/?page=2","results":[{"id":1,"m3u_account":1,"name":"FX"}]}`, true
				}
				if kind == "count" {
					return 200, `{"count":3,"next":null,"results":[{"id":2,"m3u_account":1,"name":"FX"}]}`, true
				}
				return 200, `{"count":2,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX"}]}`, true
			}
			r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
			if e == nil || r.Coverage() != LifecycleCoverageUnavailable {
				t.Fatalf("accepted %s: %v", kind, e)
			}
		})
	}
}
func TestLifecycleUnsettledStatusDefersAndByIDMismatchIsPresence(t *testing.T) {
	f := lifecycleGood()
	f.accountBody = `[{"id":1,"is_active":true,"status":"fetching"}]`
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
	if e != nil || r.Coverage() != LifecycleCoveragePresenceOnly {
		t.Fatalf("active refresh accepted: %s %v", r.Coverage(), e)
	}
	f = lifecycleGood()
	f.accountBody = `[{"id":1,"is_active":true,"status":"success"},{"id":9,"is_active":false,"status":"disabled"}]`
	f.historicalBody = `[{"id":8,"m3u_account":9,"name":"Moved"}]`
	r, e = f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 8, AccountID: 1}})
	if e != nil || r.Coverage() != LifecycleCoveragePresenceOnly || r.HistoricalRows()[0].AccountID != 9 {
		t.Fatalf("moved positive not retained: %+v %v", r, e)
	}
}

func TestLifecycleRejectsHistoricalLookupOmittingVisibleID(t *testing.T) {
	f := lifecycleGood()
	f.historicalBody = `[]`
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 1, AccountID: 1}})
	if e == nil || r.Coverage() != LifecycleCoverageUnavailable {
		t.Fatal("omitted visible historical ID accepted")
	}
}
func TestLifecycleSameIDRenamedStillPresent(t *testing.T) {
	f := lifecycleGood()
	f.streamBody = strings.Replace(f.streamBody, `"FX"`, `"FX RENAMED"`, 1)
	f.historicalBody = strings.Replace(f.historicalBody, `"FX"`, `"FX RENAMED"`, 1)
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), []LifecycleReference{{StreamID: 1, AccountID: 1}})
	if e != nil || r.Coverage() != LifecycleCoverageComplete || len(r.HistoricalRows()) != 1 || r.HistoricalRows()[0].Name != "FX RENAMED" {
		t.Fatalf("renamed existing ID lost: %+v %v", r, e)
	}
}

func TestLifecycleAcceptsReorderedIdenticalObservations(t *testing.T) {
	f := lifecycleGood()
	f.hook = func(r *http.Request, n int) (int, string, bool) {
		if r.URL.Path == "/api/channels/streams/" && n == 2 {
			return 200, `{"count":2,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX"},{"id":2,"m3u_account":null,"name":" Manual "}]}`, true
		}
		return 0, "", false
	}
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
	if e != nil || r.Coverage() != LifecycleCoverageComplete {
		t.Fatalf("stable reordered rows rejected: %s %v", r.Coverage(), e)
	}
}
func TestLifecycleRejectsInvalidUTF8Names(t *testing.T) {
	f := lifecycleGood()
	f.streamBody = "{\"count\":1,\"next\":null,\"results\":[{\"id\":1,\"m3u_account\":1,\"name\":\"" + string([]byte{0xff}) + "\"}]}"
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
	if e == nil || r.Coverage() != LifecycleCoverageUnavailable {
		t.Fatal("invalid raw name accepted")
	}
}

func TestLifecycleRejectsDuplicateJSONIdentityFields(t *testing.T) {
	for _, body := range []string{
		`{"count":1,"Count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX"}]}`,
		`{"count":1,"next":null,"results":[{"id":1,"ID":2,"m3u_account":1,"name":"FX"}]}`,
		`{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"FX","Name":"Other"}]}`,
	} {
		f := lifecycleGood()
		f.streamBody = body
		r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), nil)
		if e == nil || r.Coverage() != LifecycleCoverageUnavailable {
			t.Fatalf("ambiguous JSON accepted: %s %v", r.Coverage(), e)
		}
	}
}

func TestLifecycleHistoricalLookupUsesBoundedBatches(t *testing.T) {
	f := lifecycleGood()
	f.historicalBody = `[]`
	refs := make([]LifecycleReference, 257)
	for i := range refs {
		refs[i] = LifecycleReference{StreamID: int64(1000 + i), AccountID: 1}
	}
	f.hook = func(r *http.Request, n int) (int, string, bool) {
		if r.URL.Path != "/api/channels/streams/by-ids/" {
			return 0, "", false
		}
		if r.Method != http.MethodPost {
			t.Error("lookup is not read-only API action")
		}
		var body struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 || len(body.IDs) > lifecycleBatchSize {
			t.Errorf("bad batch size %d: %v", len(body.IDs), err)
		}
		return 200, `[]`, true
	}
	r, e := f.client().CaptureLifecycleInventory(t.Context(), lifecycleConfig(), refs)
	if e != nil || r.Coverage() != LifecycleCoverageComplete || len(r.References()) != 257 || f.calls["/api/channels/streams/by-ids/"] != 4 {
		t.Fatalf("batch capture %s %v calls=%d", r.Coverage(), e, f.calls["/api/channels/streams/by-ids/"])
	}
}
