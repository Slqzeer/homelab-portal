package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseRunbookChecksBothPlatformProvenance(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(releaseRoot(), "docs", "runbooks", "release.md"))
	require.NoError(t, err)
	marker := "docker buildx imagetools inspect \"$IMAGE_REF\" --format '{{json .Provenance"
	start := strings.Index(string(data), marker)
	require.NotEqual(t, -1, start, "runbook must inspect registry provenance")
	tail := string(data)[start:]
	end := strings.Index(tail, "\n"+strings.Repeat("`", 3))
	require.Positive(t, end, "provenance command must end in the Bash example")
	command := tail[:end]

	dir := t.TempDir()
	stub := `#!/usr/bin/env bash
case "$*" in
  *'{{json .Provenance}}'*) printf '%s\n' "$PROVENANCE_JSON";;
  *) printf 'null\n';;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(stub), 0755))

	buildType := "https://mobyproject.org/buildkit@v1"
	valid := `{"linux/amd64":{"SLSA":{"buildType":"` + buildType + `"}},"linux/arm64":{"SLSA":{"buildType":"` + buildType + `"}}}`
	cases := []struct {
		name       string
		provenance string
		valid      bool
	}{
		{"both platforms", valid, true},
		{"missing arm64", strings.Replace(valid, `"linux/arm64"`, `"linux/other"`, 1), false},
		{"wrong amd64 build type", strings.Replace(valid, buildType, "wrong", 1), false},
		{"wrong arm64 build type", strings.Replace(valid, buildType+`"}}}`, `wrong"}}}`, 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", command)
			cmd.Env = append(os.Environ(),
				"PATH="+dir+":"+os.Getenv("PATH"),
				"IMAGE_REF=ghcr.io/slqzeer/homelab-portal@sha256:"+strings.Repeat("a", 64),
				"PROVENANCE_JSON="+tc.provenance)
			out, err := cmd.CombinedOutput()
			if tc.valid {
				require.NoError(t, err, string(out))
			} else {
				require.Error(t, err, string(out))
			}
		})
	}
}
