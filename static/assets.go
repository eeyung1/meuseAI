// Package static embeds the dashboard so deployment needs no source checkout.
package static

import "embed"

//go:embed dashboard.html dashboard.css dashboard.js
var Assets embed.FS
