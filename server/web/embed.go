// Package web embeds the dashboard assets served at "/".
// Until the Vue build lands this is a single placeholder page.
package web

import "embed"

//go:embed index.html
var Assets embed.FS
