// Package static embeds the dashboard so deployment needs no source checkout.
package static

import "embed"

//go:embed dashboard.html dashboard.css dashboard.js showcase.js admin-showcase.js admin.css assets/showcase landing.html landing.css landing.js verify.html verify.js share.html share.js admin.html admin.js
var Assets embed.FS
