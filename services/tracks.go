package services

import (
	"errors"
	"strings"
)

const (
	Tier1SingleTrackAmountKobo int64 = 500000
	Tier1BundleAmountKobo      int64 = 800000
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

// Tier1TrackAmount returns the server-owned price for one track or the
// discounted two-track bundle, followed by any earned course discount.
func Tier1TrackAmount(tracks string, discountPercent int) (int64, error) {
	canonical, err := CanonicalTracks(strings.Split(tracks, ","))
	if err != nil || canonical != tracks {
		return 0, errors.New("invalid canonical tracks")
	}
	amount := Tier1SingleTrackAmountKobo
	if canonical == "landing_pages,video" {
		amount = Tier1BundleAmountKobo
	}
	if discountPercent < 0 || discountPercent > 100 {
		return 0, errors.New("invalid discount")
	}
	return amount - amount*int64(discountPercent)/100, nil
}

func ValidTrackAmount(amount int64, tracks string) bool {
	if tracks == "" {
		tracks = "landing_pages"
	}
	canonical, err := CanonicalTracks(strings.Split(tracks, ","))
	if err != nil || canonical != tracks {
		return false
	}
	full, err := Tier1TrackAmount(canonical, 0)
	if err != nil {
		return false
	}
	discounted, _ := Tier1TrackAmount(canonical, 10)
	return amount == full || amount == discounted
}
