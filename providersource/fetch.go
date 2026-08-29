package providersource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxHTMLBytes = 4 << 20
	maxJSONBytes = 16 << 20
	maxPDFBytes  = 24 << 20
)

func (s *Service) fetchBytes(ctx context.Context, endpoint, accept, label string, limit int64, privateURL bool) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building %s request: %w", label, err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "GraceNoteScraper provider enrichment")
	response, err := s.httpClient.Do(request)
	if err != nil {
		if privateURL {
			return nil, fmt.Errorf("%s request failed", label)
		}
		return nil, fmt.Errorf("%s request failed: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("%s returned %s", label, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if privateURL {
			return nil, fmt.Errorf("reading %s", label)
		}
		return nil, fmt.Errorf("reading %s: %w", label, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %s response limit", label, byteLimitLabel(limit))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s returned an empty response", label)
	}
	return data, nil
}

func (s *Service) postJSON(ctx context.Context, endpoint, label string, payload any, limit int64, privateRequest bool) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding %s request", label)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building %s request", label)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "GraceNoteScraper provider enrichment")
	response, err := s.httpClient.Do(request)
	if err != nil {
		if privateRequest {
			return nil, fmt.Errorf("%s request failed", label)
		}
		return nil, fmt.Errorf("%s request failed: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("%s returned %s", label, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s", label)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %s response limit", label, byteLimitLabel(limit))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s returned an empty response", label)
	}
	return data, nil
}

func byteLimitLabel(limit int64) string {
	if limit%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", limit>>20)
	}
	return fmt.Sprintf("%d-byte", limit)
}

func sourceFailure(source catalogSource, err error) providerResult {
	source.Status = "error"
	source.Message = err.Error()
	return providerResult{source: source, err: err}
}

type providerResult struct {
	source catalogSource
	err    error
}

func requireEntries(source catalogSource) providerResult {
	if len(source.Entries) == 0 {
		return sourceFailure(source, errors.New(source.Label+" returned no usable channels"))
	}
	return providerResult{source: source}
}

func splitChannelNumbers(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == ',' || r == ';' || r == '|' || r == '\n'
	})
	result := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(part, "HD"), "SD"))
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		result = append(result, part)
	}
	return result
}
