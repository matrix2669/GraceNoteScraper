package providersource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type addressTransport func(*http.Request) (*http.Response, error)

func (f addressTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestXfinityAddressLookupRequiresChannelData(t *testing.T) {
	for _, tc := range []struct {
		status   int
		body     string
		verified bool
	}{
		{200, `{"channels":[{"channelNumber":"2","channelName":"Test Network"}]}`, true},
		{200, `{"channels":[]}`, false}, {404, `private request detail`, false}, {429, `rate limit`, false},
	} {
		client := &http.Client{Transport: addressTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("address") != "1 NE Example St, Test City, FL 33308" {
				t.Fatal("address changed")
			}
			return &http.Response{StatusCode: tc.status, Status: http.StatusText(tc.status), Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
		})}
		got := newService(client).TestAddress(context.Background(), lineupindex.ProviderEvidenceRequest{Provider: web.Provider{Name: "Xfinity"}, ServiceAddress: lineupindex.ProviderAddress{FormattedAddress: "1 NE Example St, Test City, FL 33308"}})
		if got.Verified != tc.verified || strings.Contains(got.Message, "private request detail") || strings.Contains(got.Message, "Example St") {
			t.Fatalf("unsafe/incorrect result %+v", got)
		}
	}
}
