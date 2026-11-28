package guide

import (
	"net/url"
	"regexp"
)

var gracenoteStationLogoPath = regexp.MustCompile(`^/h3/NowShowing/([0-9]+)/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\.png$`)

const gracenoteLogoHost = "zpmc.tmsimg.com"

// StationThumbnailURL accepts only a provider-supplied station image whose
// directory identifies this exact station. The generic image and another
// station's icon are not evidence. This is a direct link, not a server fetch.
func StationThumbnailURL(stationID, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host != gracenoteLogoHost || u.User != nil ||
		(u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") ||
		u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return ""
	}
	match := gracenoteStationLogoPath.FindStringSubmatch(u.Path)
	if match == nil || match[1] != stationID {
		return ""
	}
	return "https://" + gracenoteLogoHost + u.Path
}
