// Package ui holds the templates and the static shell, embedded in the binary.
package ui

import "embed"

//go:embed templates/*.html
var Templates embed.FS

//go:embed static/*
var Static embed.FS
