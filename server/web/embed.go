// Package web embeds the dashboard assets served at "/".
// Static dashboard: two HTML pages plus app.js/style.css, no build step.
package web

import "embed"

//go:embed *.html *.js *.css
var Assets embed.FS
