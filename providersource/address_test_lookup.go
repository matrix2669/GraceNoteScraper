package providersource

import (
	"context"

	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
)

// AddressCheck reports retrieval, not postal verification or station matching.
// Never include request URLs, addresses, tokens or raw responses here.
type AddressCheck struct {
	Provider string `json:"provider"`
	Verified bool   `json:"verified"`
	Channels int    `json:"channels"`
	Message  string `json:"message"`
}

func (s *Service) TestAddress(ctx context.Context, request lineupindex.ProviderEvidenceRequest) AddressCheck {
	result := AddressCheck{Provider: request.Provider.Name}
	var fetched providerResult
	switch OfficialSourceID(request.Provider.Name) {
	case "xfinity-official-lineup":
		fetched = s.fetchXfinity(ctx, request)
	case "optimum-official-lineup":
		fetched = s.fetchOptimum(ctx, request)
	default:
		result.Message = "No address-test adapter is available for this provider."
		return result
	}
	result.Channels = len(fetched.source.Entries)
	result.Verified = fetched.err == nil && result.Channels > 0
	if result.Verified {
		result.Message = "Provider returned channel data. This is not USPS address verification."
	} else {
		result.Message = "Provider did not return usable channel data. Check the pasted address or retry later; the source may be unavailable or rate-limited."
	}
	return result
}
