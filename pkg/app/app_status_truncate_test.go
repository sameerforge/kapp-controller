// Copyright 2026 The Carvel Authors.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"carvel.dev/kapp-controller/pkg/apis/kappctrl/v1alpha1"
	"carvel.dev/kapp-controller/pkg/deploy"
	"carvel.dev/kapp-controller/pkg/exec"
	"carvel.dev/kapp-controller/pkg/fetch"
	"carvel.dev/kapp-controller/pkg/metrics"
	"carvel.dev/kapp-controller/pkg/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func Test_truncateOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		check    func(t *testing.T, result string)
	}{
		{
			name:     "under limit is unchanged",
			input:    "small output",
			maxBytes: 1024,
			check: func(t *testing.T, result string) {
				assert.Equal(t, "small output", result)
			},
		},
		{
			name:     "exactly at limit is unchanged",
			input:    strings.Repeat("x", 100),
			maxBytes: 100,
			check: func(t *testing.T, result string) {
				assert.Equal(t, strings.Repeat("x", 100), result)
			},
		},
		{
			name:     "empty string is unchanged",
			input:    "",
			maxBytes: 100,
			check: func(t *testing.T, result string) {
				assert.Equal(t, "", result)
			},
		},
		{
			name:     "over limit has marker prefix and tail suffix",
			input:    strings.Repeat("a", 200) + strings.Repeat("z", 50),
			maxBytes: 50,
			check: func(t *testing.T, result string) {
				assert.True(t, strings.HasPrefix(result, truncationMarker))
				assert.True(t, strings.HasSuffix(result, strings.Repeat("z", 50)))
				// Marker does not count toward maxBytes.
				assert.Equal(t, len(truncationMarker)+50, len(result))
			},
		},
		{
			name: "tail content preserved for realistic kapp output",
			// Many per-resource lines followed by an actionable summary.  maxBytes
			// is set to hold only the last ~100 bytes so truncation is guaranteed,
			// and the summary at the tail must survive.
			input: strings.Repeat("op add configmap/cm-001 (v1) namespace: default\n", 100) +
				"Op: 100 add, 0 delete\nWait to: 100 reconcile\n",
			maxBytes: 100,
			check: func(t *testing.T, result string) {
				assert.True(t, strings.HasPrefix(result, truncationMarker),
					"large output must start with the truncation marker")
				assert.Contains(t, result, "100 add",
					"the actionable summary at the tail must be preserved after truncation")
			},
		},
		{
			name: "UTF-8 boundary: continuation byte is skipped",
			// s = 98 'a's + "éé"  (102 bytes)
			// maxBytes = 3 → start = 99 → s[99] = 0xA9 (continuation of first 'é')
			// → advance to 100 → s[100] = 0xC3 (leading byte of second 'é') → stop
			// kept = s[100:] = "é"
			input:    strings.Repeat("a", 98) + "éé",
			maxBytes: 3,
			check: func(t *testing.T, result string) {
				require.Equal(t, 102, len(strings.Repeat("a", 98)+"éé"))
				assert.True(t, utf8.ValidString(result), "result must be valid UTF-8")
				assert.Equal(t, truncationMarker+"é", result)
			},
		},
		{
			name:     "pure multibyte runes produce valid UTF-8",
			input:    strings.Repeat("あ", 100), // 300 bytes; each rune is 3 bytes
			maxBytes: 100,
			check: func(t *testing.T, result string) {
				// 100 / 3 = 33 complete runes (99 bytes); byte 100 is a continuation
				// byte and is skipped.
				assert.True(t, utf8.ValidString(result))
				assert.Equal(t, truncationMarker+strings.Repeat("あ", 33), result)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, truncateOutput(tc.input, tc.maxBytes))
		})
	}
}

// newAppForTest returns a minimal *App wired with no-op hooks.
func newAppForTest(t *testing.T, appModel v1alpha1.App, opts Opts) *App {
	t.Helper()
	return NewApp(appModel, Hooks{
		BlockDeletion:   func() error { return nil },
		UnblockDeletion: func() error { return nil },
		UpdateStatus:    func(string) error { return nil },
	}, fetch.Factory{}, template.Factory{}, deploy.Factory{},
		logf.Log.WithName("test"),
		opts,
		metrics.NewMetrics(),
		FakeComponentInfo{},
	)
}

func Test_updateLastDeploy_TruncatesLargeStdout(t *testing.T) {
	const limit = 50
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: limit})
	a.app.Status.Deploy = &v1alpha1.AppStatusDeploy{}

	largeStdout := strings.Repeat("resource change line\n", 1000)
	a.updateLastDeploy(exec.CmdRunResult{Stdout: largeStdout, Finished: true, ExitCode: 0})

	got := a.app.Status.Deploy.Stdout
	assert.True(t, strings.HasPrefix(got, truncationMarker),
		"deploy stdout should start with truncation marker when over limit")
	assert.LessOrEqual(t, len(got), len(truncationMarker)+limit,
		"deploy stdout should not exceed marker + limit bytes")
}

func Test_updateLastDeploy_TruncatesLargeStderr(t *testing.T) {
	const limit = 50
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: limit})
	a.app.Status.Deploy = &v1alpha1.AppStatusDeploy{}

	largeStderr := strings.Repeat("error line\n", 1000)
	a.updateLastDeploy(exec.CmdRunResult{Stderr: largeStderr, Finished: true, ExitCode: 1})

	got := a.app.Status.Deploy.Stderr
	assert.True(t, strings.HasPrefix(got, truncationMarker))
	assert.LessOrEqual(t, len(got), len(truncationMarker)+limit)
}

func Test_updateLastDeploy_NoTruncation_WhenUnderLimit(t *testing.T) {
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: 10000})
	a.app.Status.Deploy = &v1alpha1.AppStatusDeploy{}

	smallStdout := "Changes\n\nOp: 1 add\n"
	a.updateLastDeploy(exec.CmdRunResult{Stdout: smallStdout, Finished: true, ExitCode: 0})

	assert.NotEmpty(t, a.app.Status.Deploy.Stdout)
	assert.False(t, strings.HasPrefix(a.app.Status.Deploy.Stdout, truncationMarker),
		"output under the limit must not be prefixed with the truncation marker")
}

func Test_updateLastDeploy_TailIsPreserved(t *testing.T) {
	const limit = 40
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: limit})
	a.app.Status.Deploy = &v1alpha1.AppStatusDeploy{}

	// WithFriendlyYAMLStrings (called inside updateLastDeploy) strips trailing
	// whitespace, so do not include a trailing newline in the expected assertion.
	tail := "IMPORTANT: deploy succeeded"
	a.updateLastDeploy(exec.CmdRunResult{
		Stdout:   strings.Repeat("x", 500) + tail + "\n",
		Finished: true,
		ExitCode: 0,
	})

	assert.Contains(t, a.app.Status.Deploy.Stdout, tail,
		"the tail (most recent output) must survive truncation")
}

func Test_setUsefulErrorMessage_TruncatesLargeStderr(t *testing.T) {
	const limit = 50
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: limit})

	largeStderr := strings.Repeat("kapp: Error: resource conflict\n", 1000)
	a.setUsefulErrorMessage(exec.CmdRunResult{Stderr: largeStderr, ExitCode: 1})

	got := a.app.Status.UsefulErrorMessage
	assert.True(t, strings.HasPrefix(got, truncationMarker))
	assert.LessOrEqual(t, len(got), len(truncationMarker)+limit)
}

func Test_setUsefulErrorMessage_NoTruncation_WhenUnderLimit(t *testing.T) {
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: 10000})

	stderr := "kapp: Error: resource configmap/foo not found"
	a.setUsefulErrorMessage(exec.CmdRunResult{Stderr: stderr, ExitCode: 1})

	assert.Equal(t, stderr, a.app.Status.UsefulErrorMessage)
}
