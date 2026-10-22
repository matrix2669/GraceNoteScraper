package dispatcharr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const ImpactProfile = "exact95-c766a6b-builtins-country-embedded-specific-numbering-quality-off-v1"

type ImpactStream struct {
	Name      string `json:"name"`
	GroupName string `json:"groupName,omitempty"`
}
type ImpactRequest struct {
	Country  string         `json:"country"`
	Baseline string         `json:"baseline"`
	Proposed string         `json:"proposed"`
	Streams  []ImpactStream `json:"streams"`
}
type ImpactMatch struct {
	Category   string   `json:"category"`
	EntryIndex int      `json:"entryIndex"`
	Name       string   `json:"name"`
	Number     string   `json:"number"`
	Names      []string `json:"names"`
}
type ImpactResult struct {
	Baseline []ImpactMatch `json:"baseline"`
	Proposed []ImpactMatch `json:"proposed"`
	Revision string        `json:"revision"`
}

// CheckSupplementalImpact is invoked only by the explicit impact action. The
// unchanged matcher/alias modules run in a separate, bounded, one-shot process.
func CheckSupplementalImpact(ctx context.Context, request ImpactRequest) (ImpactResult, error) {
	var result ImpactResult
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	dir, err := os.MkdirTemp("", "gracenote-impact-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"impact_runner.py", "fuzzy_matcher.py", "matching_core.py", "aliases.py", "LICENSE"} {
		data, e := matcher.Files.ReadFile(name)
		if e != nil {
			return result, e
		}
		if e = os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
			return result, e
		}
	}
	python := os.Getenv("LINEUPARR_MATCHER_PYTHON")
	if python == "" {
		python = "python3"
	}
	run, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(run, python, "-I", "-B", filepath.Join(dir, "impact_runner.py"))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(payload)
	output := &limitedMatcherOutput{limit: 64 << 20}
	cmd.Stdout = output
	cmd.Stderr = &matcherProgressWriter{notify: matcherProgressCallback(ctx)}
	if err = cmd.Run(); err != nil {
		if run.Err() != nil {
			return result, run.Err()
		}
		return result, fmt.Errorf("consumer impact failed (Python 3 required): %w", err)
	}
	if err = json.Unmarshal(output.Bytes(), &result); err != nil {
		return result, err
	}
	if result.Revision != matcher.Revision {
		return result, errors.New("unexpected impact consumer revision")
	}
	return result, nil
}
