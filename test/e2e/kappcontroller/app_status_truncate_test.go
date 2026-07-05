// Copyright 2026 The Carvel Authors.
// SPDX-License-Identifier: Apache-2.0

package kappcontroller

// Test_AppStatus_DeployStdout_CapturedAndBounded validates the end-to-end path
// for status.deploy.stdout:
//
//  1. After a successful deploy, status.deploy.stdout is non-empty and contains
//     recognisable kapp change-summary output.
//  2. When a deploy produces output that fits within the 1 MiB limit it is
//     stored verbatim (no truncation marker).
//  3. When output exceeds the limit the stored value begins with the truncation
//     marker "[output truncated]\n" so operators can immediately tell the field
//     was clipped (verified via unit tests in pkg/app/app_status_truncate_test.go
//     because generating >1 MiB in-cluster would require thousands of resources).
//
// Background: https://github.com/carvel-dev/kapp-controller/issues/1839

import (
	"fmt"
	"strings"
	"testing"

	"carvel.dev/kapp-controller/pkg/apis/kappctrl/v1alpha1"
	"carvel.dev/kapp-controller/test/e2e"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// generateConfigMapBundle returns inline YAML with count ConfigMaps, each with
// a small data payload.  Deploying 30+ resources produces several KB of kapp
// stdout and is enough to verify the output is captured in status without
// approaching the 1 MiB truncation limit.
func generateConfigMapBundle(count int) string {
	var sb strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&sb, "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: trunc-test-cm-%03d\ndata:\n  index: \"%d\"\n", i, i)
	}
	return sb.String()
}

func Test_AppStatus_DeployStdout_CapturedAndBounded(t *testing.T) {
	env := e2e.BuildEnv(t)
	logger := e2e.Logger{}
	kapp := e2e.Kapp{T: t, Namespace: env.Namespace, L: logger}
	sas := e2e.ServiceAccounts{Namespace: env.Namespace}

	name := "deploy-stdout-truncate-test"

	// 30 ConfigMaps → several KB of kapp output; well under the 1 MiB limit so
	// no truncation occurs on this path.  The truncation behaviour itself is
	// covered by unit tests in pkg/app/app_status_truncate_test.go.
	configMaps := generateConfigMapBundle(30)

	appYaml := fmt.Sprintf(`
---
apiVersion: kappctrl.k14s.io/v1alpha1
kind: App
metadata:
  name: %s
  annotations:
    kapp.k14s.io/change-group: kappctrl-e2e.k14s.io/apps
spec:
  serviceAccountName: kappctrl-e2e-ns-sa
  fetch:
    - inline:
        paths:
          resources.yml: |
%s
  template:
    - ytt: {}
  deploy:
    - kapp:
        intoNs: %s
`, name, indentYAML(configMaps, 12), env.Namespace) + sas.ForNamespaceYAML()

	cleanUp := func() {
		kapp.Run([]string{"delete", "-a", name})
	}
	cleanUp()
	defer cleanUp()

	logger.Section("deploy app with 30 ConfigMaps", func() {
		_, err := kapp.RunWithOpts([]string{"deploy", "-f", "-", "-a", name},
			e2e.RunOpts{StdinReader: strings.NewReader(appYaml)})
		require.NoError(t, err)
	})

	out := kapp.Run([]string{"inspect", "-a", name, "--raw", "--tty=false", "--filter-kind=App"})
	require.Greater(t, len(out), 100, "inspect output should be non-trivial")

	var cr v1alpha1.App
	err := yaml.Unmarshal([]byte(out), &cr)
	require.NoError(t, err, "failed to unmarshal App CR")

	// --- deploy status assertions ---
	require.NotNil(t, cr.Status.Deploy, "status.deploy must be set after a successful reconcile")

	t.Run("stdout validation", func(t *testing.T) {
		stdout := cr.Status.Deploy.Stdout

		assert.NotEmpty(t, stdout, "stdout must contain kapp output after a successful deploy")
		assert.Contains(t, stdout, "Op:", "stdout should contain the kapp Op summary line")
		assert.False(t, strings.HasPrefix(stdout, "[output truncated]\n"), "small output must not be truncated")

		const oneMiB = 1 * 1024 * 1024
		assert.Less(t, len(stdout), oneMiB, "stdout must never exceed the 1 MiB cap")
	})

	t.Run("reconcile succeeded", func(t *testing.T) {
		assert.Equal(t, v1alpha1.ReconcileSucceeded, cr.Status.Conditions[0].Type)
	})
}

// indentYAML prepends spaces to every line so the YAML block fits as a
// literal block scalar inside the parent document.
func indentYAML(s string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
