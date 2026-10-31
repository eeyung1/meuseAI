// Package static embeds the dashboard so deployment needs no source checkout.
package static

import "embed"

//go:embed tier1.html tier1.js appearance.js appearance.css dashboard.html dashboard.css dashboard.js admin.css landing.html landing.css landing.js verify.html verify.js share.html share.js admin.html admin.js
var Assets embed.FS
