// Copyright 2026 The Carvel Authors.
// SPDX-License-Identifier: Apache-2.0

package app

// defaultMaxStatusOutputBytes is the default cap for each stdout/stderr string
// written into an App status sub-field (deploy, fetch, inspect).
// etcd rejects objects larger than its 1.5 MiB limit (often 2 MiB at the
// Kubernetes gRPC client level); on clusters with large number of nodes (ex 250+),
// a single kapp deploy can produce several MiB of output, causing the UpdateStatus call to
// fail and delay reconciliation until a subsequent no-op reconcile fits.
//
// Keeping the tail is intentional: the most actionable content (resource
// summary, error lines) always appears at the end of kapp output.
const defaultMaxStatusOutputBytes = 1 * 1024 * 1024 // 1 MiB

// maxOutputBytes returns the configured output cap for this App, falling back
// to defaultMaxStatusOutputBytes when Opts.MaxStatusOutputBytes is zero.
func (a *App) maxOutputBytes() int {
	if a.opts.MaxStatusOutputBytes > 0 {
		return a.opts.MaxStatusOutputBytes
	}
	return defaultMaxStatusOutputBytes
}
