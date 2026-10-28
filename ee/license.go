// Package ee contains Enterprise Edition features.
// EE features are gated behind a valid license key.
// Without a license, all EE features are disabled — the OSS core continues to work.
package ee

import "fmt"

// License represents an EE license.
type License struct {
	key string
}

// ParseLicense creates a License from a key string.
// An empty key produces an invalid (OSS-only) license.
func ParseLicense(key string) License {
	return License{key: key}
}

// IsValid reports whether the license allows EE features.
func (l License) IsValid() bool {
	return l.key != ""
}

// Gate returns true if the feature is available under this license.
// Use this for soft-gating: feature degrades gracefully when false.
func (l License) Gate(_ string) bool {
	return l.IsValid()
}

// Require panics if the license is not valid.
// Use this for hard-gating: call at startup for features that must not run without a license.
func (l License) Require(feature string) {
	if !l.IsValid() {
		panic(fmt.Sprintf("ee: feature %q requires a valid Enterprise Edition license", feature))
	}
}
