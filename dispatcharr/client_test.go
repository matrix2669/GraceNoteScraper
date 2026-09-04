package dispatcharr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestClientAuthenticatesRefreshesAndPaginatesSafeStreams(t *testing.T) {
	var mu sync.Mutex
	loginCount := 0
	refreshCount := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/accounts/token/":
			mu.Lock()
			loginCount++
			mu.Unlock()
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "admin" || body["password"] != "secret" {
				return testResponse(r, http.StatusUnauthorized, "bad credentials"), nil
			}
			return testResponse(r, http.StatusOK, "{\"access\":\"expired\",\"refresh\":\"refresh-token\"}"), nil
		case "/api/accounts/token/refresh/":
			mu.Lock()
			refreshCount++
			mu.Unlock()
			return testResponse(r, http.StatusOK, "{\"access\":\"fresh\"}"), nil
		case "/api/channels/streams/":
			if r.Header.Get("Authorization") == "Bearer expired" {
				return testResponse(r, http.StatusUnauthorized, "expired"), nil
			}
			if r.Header.Get("Authorization") != "Bearer fresh" {
				return testResponse(r, http.StatusUnauthorized, "missing token"), nil
			}
			if r.URL.Query().Get("m3u_account_is_active") != "true" || r.URL.Query().Get("page_size") != strconv.Itoa(streamPageSize) {
				t.Errorf("stream query = %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("page") == "1" {
				return testResponse(r, http.StatusOK, "{\"count\":2,\"next\":\"ignored\",\"results\":[{\"id\":1,\"name\":\"US| ESPN HD\",\"url\":\"http://secret/stream\",\"tvg_id\":\"ESPN.us\",\"m3u_account\":3,\"channel_group\":8,\"stream_chno\":570}]}"), nil
			}
			return testResponse(r, http.StatusOK, "{\"count\":2,\"next\":null,\"results\":[{\"id\":2,\"name\":\"CNN\",\"url\":\"http://secret/other\",\"tvg_id\":\"CNN.us\",\"m3u_account\":3,\"channel_group\":9}]}"), nil
		default:
			return testResponse(r, http.StatusNotFound, "not found"), nil
		}
	})}

	client := NewClient(httpClient)
	streams, err := client.Streams(t.Context(), Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 2 || streams[0].Name != "US| ESPN HD" || streams[0].M3UAccountID != 3 || streams[1].TVGID != "CNN.us" {
		t.Fatalf("streams = %+v", streams)
	}
	mu.Lock()
	defer mu.Unlock()
	if loginCount != 1 || refreshCount != 1 {
		t.Fatalf("login/refresh counts = %d/%d", loginCount, refreshCount)
	}
	encoded, _ := json.Marshal(streams)
	if strings.Contains(string(encoded), "secret/stream") {
		t.Fatalf("stream URL leaked into safe model: %s", encoded)
	}
}

func TestClientAuthenticationErrorDoesNotEchoResponseBody(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return testResponse(r, http.StatusUnauthorized, "password secret rejected"), nil
	})}
	client := NewClient(httpClient)
	err := client.Test(t.Context(), Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("sanitized auth error = %v", err)
	}
}

func TestClientTestAlwaysUsesSubmittedPassword(t *testing.T) {
	var passwords []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/accounts/token/" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			passwords = append(passwords, body["password"])
			return testResponse(r, http.StatusOK, "{\"access\":\""+body["password"]+"\",\"refresh\":\"refresh\"}"), nil
		}
		if r.Header.Get("Authorization") == "" {
			return testResponse(r, http.StatusUnauthorized, "missing"), nil
		}
		return testResponse(r, http.StatusOK, "{\"count\":0,\"next\":null,\"results\":[]}"), nil
	})}
	client := NewClient(httpClient)
	for _, password := range []string{"old-password", "new-password"} {
		if err := client.Test(t.Context(), Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: password}); err != nil {
			t.Fatal(err)
		}
	}
	if len(passwords) != 2 || passwords[0] != "old-password" || passwords[1] != "new-password" {
		t.Fatalf("submitted login passwords = %v", passwords)
	}
}

func TestClientSessionDoesNotCrossPasswordChanges(t *testing.T) {
	var passwords []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/accounts/token/" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			passwords = append(passwords, body["password"])
			return testResponse(r, http.StatusOK, "{\"access\":\""+body["password"]+"\",\"refresh\":\"refresh\"}"), nil
		}
		return testResponse(r, http.StatusOK, "{\"count\":0,\"next\":null,\"results\":[]}"), nil
	})}
	client := NewClient(httpClient)
	for _, password := range []string{"old-password", "new-password"} {
		_, err := client.Streams(t.Context(), Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: password})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(passwords) != 2 || passwords[0] != "old-password" || passwords[1] != "new-password" {
		t.Fatalf("session login passwords = %v", passwords)
	}
}

func TestClientUsesAPIKeyWithoutJWTLogin(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/accounts/token/" {
			t.Fatal("API-key authentication attempted a JWT login")
		}
		if r.Header.Get("X-API-Key") != "api-secret" || r.Header.Get("Authorization") != "" {
			return testResponse(r, http.StatusUnauthorized, "missing API key"), nil
		}
		return testResponse(r, http.StatusOK, `{"count":0,"next":null,"results":[]}`), nil
	})}
	config := Config{BaseURL: "https://dispatcharr.test", AuthMethod: AuthAPIKey, APIKey: "api-secret"}
	if err := NewClient(httpClient).Test(t.Context(), config); err != nil {
		t.Fatal(err)
	}
}

func TestClientTrimsAndBoundsSafeStreamMetadata(t *testing.T) {
	longName := strings.Repeat("x", maxStreamNameSize+1)
	longTVGID := strings.Repeat("y", maxTVGIDSize+1)
	page, _ := json.Marshal(streamPage{Results: []Stream{
		{ID: 1, Name: "  CNN  ", TVGID: " CNN.us ", M3UAccountID: 3},
		{ID: 2, Name: longName, M3UAccountID: 3},
		{ID: 3, Name: "ESPN", TVGID: longTVGID, M3UAccountID: 3},
	}})
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/accounts/token/" {
			return testResponse(r, http.StatusOK, `{"access":"access","refresh":"refresh"}`), nil
		}
		return testResponse(r, http.StatusOK, string(page)), nil
	})}
	streams, err := NewClient(httpClient).Streams(t.Context(), Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 2 || streams[0].Name != "CNN" || streams[0].TVGID != "CNN.us" || streams[1].Name != "ESPN" || streams[1].TVGID != "" {
		t.Fatalf("bounded streams = %+v", streams)
	}
}

func TestClientRejectsOversizedTokensAndResponses(t *testing.T) {
	oversizedToken := strings.Repeat("x", maxTokenSize+1)
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/accounts/token/" {
			return testResponse(r, http.StatusOK, `{"access":"`+oversizedToken+`","refresh":"refresh"}`), nil
		}
		return testResponse(r, http.StatusOK, "12345"), nil
	})}
	client := NewClient(httpClient)
	if err := client.Test(t.Context(), Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret"}); err == nil {
		t.Fatal("oversized token was accepted")
	}
	if _, _, err := client.request(context.Background(), http.MethodGet, "https://dispatcharr.test/oversized", "", nil, 4); err == nil {
		t.Fatal("oversized response was accepted")
	}
}
