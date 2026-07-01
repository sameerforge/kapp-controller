// Copyright 2026 The Carvel Authors.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"strings"
	"testing"

	"carvel.dev/kapp-controller/pkg/apis/kappctrl/v1alpha1"
	"carvel.dev/kapp-controller/pkg/deploy"
	"carvel.dev/kapp-controller/pkg/exec"
	"carvel.dev/kapp-controller/pkg/fetch"
	"carvel.dev/kapp-controller/pkg/metrics"
	"carvel.dev/kapp-controller/pkg/template"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

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
	assert.True(t, strings.HasPrefix(got, exec.TruncationMarker),
		"deploy stdout should start with truncation marker when over limit")
	assert.LessOrEqual(t, len(got), len(exec.TruncationMarker)+limit,
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
	assert.True(t, strings.HasPrefix(got, exec.TruncationMarker))
	assert.LessOrEqual(t, len(got), len(exec.TruncationMarker)+limit)
}

func Test_updateLastDeploy_NoTruncation_WhenUnderLimit(t *testing.T) {
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: 10000})
	a.app.Status.Deploy = &v1alpha1.AppStatusDeploy{}

	smallStdout := "Changes\n\nOp: 1 add\n"
	a.updateLastDeploy(exec.CmdRunResult{Stdout: smallStdout, Finished: true, ExitCode: 0})

	assert.NotEmpty(t, a.app.Status.Deploy.Stdout)
	assert.False(t, strings.HasPrefix(a.app.Status.Deploy.Stdout, exec.TruncationMarker),
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
	assert.True(t, strings.HasPrefix(got, exec.TruncationMarker))
	assert.LessOrEqual(t, len(got), len(exec.TruncationMarker)+limit)
}

func Test_setUsefulErrorMessage_NoTruncation_WhenUnderLimit(t *testing.T) {
	a := newAppForTest(t, v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "test-app", Namespace: "default"},
	}, Opts{MaxStatusOutputBytes: 10000})

	stderr := "kapp: Error: resource configmap/foo not found"
	a.setUsefulErrorMessage(exec.CmdRunResult{Stderr: stderr, ExitCode: 1})

	assert.Equal(t, stderr, a.app.Status.UsefulErrorMessage)
}
