package dispatcharr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	streamPageSize        = 10000
	maxStreamPages        = 100
	maxStreamCount        = 100000
	maxAuthResponseSize   = 1 << 20
	maxStreamResponseSize = 64 << 20
	maxTokenSize          = 64 << 10
	maxStreamNameSize     = 512
	maxTVGIDSize          = 255
)

type Client struct {
	httpClient *http.Client
	authMu     sync.Mutex
	session    authSession
}

type authSession struct {
	fingerprint string
	access      string
	refresh     string
}

type tokenPair struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

type streamPage struct {
	Count   int      `json:"count"`
	Next    *string  `json:"next"`
	Results []Stream `json:"results"`
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 25 * time.Second,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) > 0 && (request.URL.Scheme != via[0].URL.Scheme || request.URL.Host != via[0].URL.Host) {
					return errors.New("Dispatcharr redirected to a different origin")
				}
				if len(via) >= 5 {
					return errors.New("too many Dispatcharr redirects")
				}
				return nil
			},
		}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Reset() {
	c.authMu.Lock()
	c.session = authSession{}
	c.authMu.Unlock()
}

func (c *Client) Test(ctx context.Context, config Config) error {
	config, err := config.Normalized()
	if err != nil {
		return err
	}
	// Match history intentionally survives password changes, but connection
	// testing still forces a fresh login so an old access token cannot validate
	// credentials that Dispatcharr would now reject.
	c.Reset()
	var page streamPage
	endpoint := config.BaseURL + "/api/channels/streams/?page=1&page_size=1&m3u_account_is_active=true&hide_stale=true"
	return c.getAuthorizedJSON(ctx, config, endpoint, &page)
}

func (c *Client) Streams(ctx context.Context, config Config) ([]Stream, error) {
	config, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	streams := make([]Stream, 0)
	for pageNumber := 1; pageNumber <= maxStreamPages; pageNumber++ {
		query := url.Values{
			"page":                  {strconv.Itoa(pageNumber)},
			"page_size":             {strconv.Itoa(streamPageSize)},
			"m3u_account_is_active": {"true"},
			"hide_stale":            {"true"},
			"ordering":              {"name"},
		}
		endpoint := config.BaseURL + "/api/channels/streams/?" + query.Encode()
		var page streamPage
		if err := c.getAuthorizedJSON(ctx, config, endpoint, &page); err != nil {
			return nil, err
		}
		for _, stream := range page.Results {
			stream.Name = strings.TrimSpace(stream.Name)
			stream.TVGID = strings.TrimSpace(stream.TVGID)
			if stream.ID <= 0 || stream.M3UAccountID <= 0 || stream.Name == "" || len(stream.Name) > maxStreamNameSize {
				continue
			}
			if len(stream.TVGID) > maxTVGIDSize {
				stream.TVGID = ""
			}
			streams = append(streams, stream)
			if len(streams) > maxStreamCount {
				return nil, fmt.Errorf("Dispatcharr returned more than %d active M3U streams", maxStreamCount)
			}
		}
		if page.Next == nil {
			return streams, nil
		}
	}
	return nil, fmt.Errorf("Dispatcharr stream listing exceeded %d pages", maxStreamPages)
}

func (c *Client) getAuthorizedJSON(ctx context.Context, config Config, endpoint string, target any) error {
	access, err := c.accessToken(ctx, config)
	if err != nil {
		return err
	}
	status, data, err := c.request(ctx, http.MethodGet, endpoint, access, nil, maxStreamResponseSize)
	if err != nil {
		return errors.New("Dispatcharr could not be reached")
	}
	if status == http.StatusUnauthorized {
		access, err = c.renewToken(ctx, config)
		if err != nil {
			return err
		}
		status, data, err = c.request(ctx, http.MethodGet, endpoint, access, nil, maxStreamResponseSize)
		if err != nil {
			return errors.New("Dispatcharr could not be reached")
		}
	}
	if status == http.StatusForbidden {
		return errors.New("Dispatcharr denied access from this host")
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("Dispatcharr stream request failed (HTTP %d)", status)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("Dispatcharr returned an unreadable stream response")
	}
	return nil
}

func (c *Client) accessToken(ctx context.Context, config Config) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	fingerprint := authFingerprint(config)
	if c.session.fingerprint == fingerprint && c.session.access != "" {
		return c.session.access, nil
	}
	pair, err := c.login(ctx, config)
	if err != nil {
		return "", err
	}
	c.session = authSession{fingerprint: fingerprint, access: pair.Access, refresh: pair.Refresh}
	return pair.Access, nil
}

func (c *Client) renewToken(ctx context.Context, config Config) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	fingerprint := authFingerprint(config)
	if c.session.fingerprint == fingerprint && c.session.refresh != "" {
		body, _ := json.Marshal(map[string]string{"refresh": c.session.refresh})
		status, data, requestErr := c.request(ctx, http.MethodPost, config.BaseURL+"/api/accounts/token/refresh/", "", body, maxAuthResponseSize)
		if requestErr == nil && status >= 200 && status < 300 {
			var refreshed struct {
				Access  string `json:"access"`
				Refresh string `json:"refresh"`
			}
			if json.Unmarshal(data, &refreshed) == nil && validToken(refreshed.Access) && (refreshed.Refresh == "" || validToken(refreshed.Refresh)) {
				c.session.access = refreshed.Access
				if refreshed.Refresh != "" {
					c.session.refresh = refreshed.Refresh
				}
				return refreshed.Access, nil
			}
		}
	}
	pair, err := c.login(ctx, config)
	if err != nil {
		return "", err
	}
	c.session = authSession{fingerprint: fingerprint, access: pair.Access, refresh: pair.Refresh}
	return pair.Access, nil
}

func authFingerprint(config Config) string {
	sum := sha256.Sum256([]byte(config.Fingerprint() + "\x00" + config.Password))
	return string(sum[:])
}

func (c *Client) login(ctx context.Context, config Config) (tokenPair, error) {
	body, _ := json.Marshal(map[string]string{"username": config.Username, "password": config.Password})
	status, data, err := c.request(ctx, http.MethodPost, config.BaseURL+"/api/accounts/token/", "", body, maxAuthResponseSize)
	if err != nil {
		return tokenPair{}, errors.New("Dispatcharr could not be reached")
	}
	switch status {
	case http.StatusUnauthorized:
		return tokenPair{}, errors.New("Dispatcharr rejected the username or password")
	case http.StatusForbidden:
		return tokenPair{}, errors.New("Dispatcharr denied login from this host")
	case http.StatusTooManyRequests:
		return tokenPair{}, errors.New("Dispatcharr login is temporarily rate limited")
	}
	if status < 200 || status >= 300 {
		return tokenPair{}, fmt.Errorf("Dispatcharr authentication failed (HTTP %d)", status)
	}
	var pair tokenPair
	if err := json.Unmarshal(data, &pair); err != nil || !validToken(pair.Access) || !validToken(pair.Refresh) {
		return tokenPair{}, errors.New("Dispatcharr returned an unreadable authentication response")
	}
	return pair, nil
}

func validToken(value string) bool {
	return value != "" && len(value) <= maxTokenSize
}

func (c *Client) request(ctx context.Context, method, endpoint, access string, body []byte, maxBytes int64) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "GraceNoteScraper-Dispatcharr-Matcher/1")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if access != "" {
		request.Header.Set("Authorization", "Bearer "+access)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return response.StatusCode, nil, err
	}
	if int64(len(data)) > maxBytes {
		return response.StatusCode, nil, errors.New("Dispatcharr response was too large")
	}
	return response.StatusCode, data, nil
}
