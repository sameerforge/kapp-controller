// Copyright 2026 The Carvel Authors.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"unicode/utf8"
)

// defaultMaxStatusOutputBytes is the default cap for each stdout/stderr string
// written into an App status sub-field (deploy, fetch, inspect).
// etcd rejects objects larger than its 1.5 MiB limit (often 2 MiB at the
// Kubernetes gRPC client level); on clusters with 250+ nodes, a single kapp
// deploy can produce several MiB of output, causing the UpdateStatus call to
// fail and delay reconciliation until a subsequent no-op reconcile fits.
//
// Keeping the tail of the output is intentional: the most actionable content
// (final resource summary, error lines) always appears at the end of kapp output.
const defaultMaxStatusOutputBytes = 1 * 1024 * 1024 // 1 MiB

const truncationMarker = "[output truncated]\n"

// maxOutputBytes returns the configured output cap for this App, falling back
// to defaultMaxStatusOutputBytes when Opts.MaxStatusOutputBytes is zero.
func (a *App) maxOutputBytes() int {
	if a.opts.MaxStatusOutputBytes > 0 {
		return a.opts.MaxStatusOutputBytes
	}
	return defaultMaxStatusOutputBytes
}

// truncateOutput returns s unchanged when len(s) <= maxBytes.  Otherwise it
// returns a truncation notice followed by the last maxBytes bytes of s,
// adjusted forward to a valid UTF-8 rune boundary so the result is always
// valid UTF-8.
func truncateOutput(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	// Advance past any UTF-8 continuation bytes so we don't split a multi-byte
	// rune in half.
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return truncationMarker + s[start:]
}
