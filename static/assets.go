// Package static embeds the dashboard so deployment needs no source checkout.
package static

import "embed"

//go:embed dashboard.html dashboard.css dashboard.js landing.html landing.css landing.js verify.html verify.js share.html share.js
var Assets embed.FS
