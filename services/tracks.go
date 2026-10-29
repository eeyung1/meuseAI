package services

import (
	"errors"
	"strings"
)

// CanonicalTracks accepts only the two server-defined learning products.
// A missing legacy selection defaults to the original landing-page product.
func CanonicalTracks(tracks []string) (string, error) {
	if tracks == nil {
		return "landing_pages", nil
	}
	if len(tracks) < 1 || len(tracks) > 2 {
		return "", errors.New("select one or both tracks")
	}
	landing, video := false, false
	for _, t := range tracks {
		switch t {
		case "landing_pages":
			if landing {
				return "", errors.New("duplicate track")
			}
			landing = true
		case "video":
			if video {
				return "", errors.New("duplicate track")
			}
			video = true
		default:
			return "", errors.New("unknown track")
		}
	}
	if landing && video {
		return "landing_pages,video", nil
	}
	if video {
		return "video", nil
	}
	return "landing_pages", nil
}
func ValidTrackAmount(amount int64, tracks string) bool {
	if tracks == "" {
		tracks = "landing_pages"
	}
	canonical, err := CanonicalTracks(strings.Split(tracks, ","))
	if err != nil || canonical != tracks {
		return false
	}
	count := int64(len(strings.Split(canonical, ",")))
	return amount%count == 0 && ValidTier1Amount(amount/count)
}
