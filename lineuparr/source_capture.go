package lineuparr

import (
	"context"
	"encoding/json"
	"errors"
)

// SourceCapture is an opaque immutable copy of the source bytes used by one
// successful draft. It is process-local and is never serialized or fetched by
// BuildCaptured. Current durable decisions are still overlaid by the normal Build.
type SourceCapture struct {
	sources    []remoteSource
	additional []SourceStatus
}

func captureSources(sources []remoteSource, additional []SourceStatus) *SourceCapture {
	capture := &SourceCapture{sources: append([]remoteSource(nil), sources...)}
	for i := range capture.sources {
		capture.sources[i].Data = append([]byte(nil), capture.sources[i].Data...)
	}
	bytes, _ := json.Marshal(additional)
	_ = json.Unmarshal(bytes, &capture.additional)
	return capture
}
func (s *Service) BuildCaptured(ctx context.Context, lineup LineupContext, inputs []InputChannel, capture *SourceCapture) (*Draft, error) {
	if capture == nil {
		return nil, errors.New("successful source capture required")
	}
	// Isolate metadata which normal source-status consolidation enriches. Source
	// response bytes remain immutable and are only read by the existing parsers.
	bytes, _ := json.Marshal(capture.additional)
	_ = json.Unmarshal(bytes, &lineup.AdditionalSources)
	lineup.capturedSources = capture
	return s.Build(ctx, lineup, inputs)
}
