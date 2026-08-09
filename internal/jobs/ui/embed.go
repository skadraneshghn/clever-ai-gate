// Package ui embeds the Job Scheduler admin SPA into the binary.
package ui

import "embed"

//go:embed dist
var DistFS embed.FS
