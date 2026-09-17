package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type workflow struct {
	On          map[string]any         `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}
type workflowJob struct {
	Needs       []string          `yaml:"needs"`
	Uses        string            `yaml:"uses"`
	Environment string            `yaml:"environment"`
	Permissions map[string]string `yaml:"permissions"`
	Outputs     map[string]string `yaml:"outputs"`
	Steps       []workflowStep    `yaml:"steps"`
}
type workflowStep struct {
	ID              string            `yaml:"id"`
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	If              string            `yaml:"if"`
	With            map[string]string `yaml:"with"`
	Env             map[string]string `yaml:"env"`
	ContinueOnError bool              `yaml:"continue-on-error"`
}

func releaseRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}
func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(releaseRoot(), ".github", "workflows", name+".yaml"))
	require.NoError(t, err)
	var w workflow
	require.NoError(t, yaml.Unmarshal(data, &w))
	require.Empty(t, w.Permissions, "grant permissions only per job")
	for _, job := range w.Jobs {
		for _, step := range job.Steps {
			require.False(t, step.ContinueOnError)
			if !strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				require.Empty(t, step.If, "security gates must be unconditional")
			}
			if step.Uses != "" {
				require.Regexp(t, `^[\w/-]+@[a-f0-9]{40}$`, step.Uses)
			}
			if strings.HasPrefix(step.Uses, "actions/checkout@") {
				require.Equal(t, "false", step.With["persist-credentials"])
			}
		}
	}
	return w
}
func stepUsing(t *testing.T, job workflowJob, action string) (int, workflowStep) {
	t.Helper()
	for i, step := range job.Steps {
		if strings.HasPrefix(step.Uses, action+"@") {
			return i, step
		}
	}
	t.Fatalf("missing action %s", action)
	return -1, workflowStep{}
}
func stepRunning(t *testing.T, job workflowJob, command string) int {
	t.Helper()
	for i, step := range job.Steps {
		if step.Run == command {
			require.Empty(t, step.If)
			return i
		}
	}
	t.Fatalf("missing unconditional command %s", command)
	return -1
}
func TestReleasePullRequestChecks(t *testing.T) {
	w := readWorkflow(t, "test")
	require.Contains(t, w.On, "pull_request")
	require.Contains(t, w.On, "workflow_call")
	job := w.Jobs["verify"]
	require.Equal(t, map[string]string{"contents": "read"}, job.Permissions)
	checks := stepRunning(t, job, "bash scripts/verify.sh source")
	buildIndex, build := stepUsing(t, job, "docker/build-push-action")
	require.Greater(t, buildIndex, checks)
	require.Equal(t, "true", build.With["load"])
	require.Equal(t, "false", build.With["push"])
	require.Equal(t, "linux/amd64", build.With["platforms"])
	require.Equal(t, "portal:ci", build.With["tags"])
	require.Greater(t, stepRunning(t, job, "bash scripts/verify.sh image"), buildIndex)
	_, artifact := stepUsing(t, job, "actions/upload-artifact")
	require.Equal(t, "always()", artifact.If)
	require.Equal(t, "reports/", artifact.With["path"])
	_, setupGo := stepUsing(t, job, "actions/setup-go")
	require.Equal(t, "false", setupGo.With["cache"])
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/cache@") {
			require.Equal(t, "~/go/pkg/mod", step.With["path"])
		}
	}
}

func TestReleaseImmutableSupplyChain(t *testing.T) {
	w := readWorkflow(t, "release")
	require.Len(t, w.On, 1)
	push := w.On["push"].(map[string]any)
	require.Equal(t, []any{"v*"}, push["tags"])
	require.Equal(t, "./.github/workflows/test.yaml", w.Jobs["test"].Uses)
	job := w.Jobs["release"]
	require.Equal(t, []string{"test"}, job.Needs)
	require.Equal(t, map[string]string{"contents": "read", "actions": "read", "packages": "write", "id-token": "write", "attestations": "write"}, job.Permissions)
	preflight := stepRunning(t, job, "bash scripts/release.sh prepare")
	_, login := stepUsing(t, job, "docker/login-action")
	require.Equal(t, "${{ secrets.GITHUB_TOKEN }}", login.With["password"])
	require.Equal(t, "ghcr.io", login.With["registry"])
	buildIndex, build := stepUsing(t, job, "docker/build-push-action")
	require.Greater(t, buildIndex, preflight)
	require.Equal(t, "linux/amd64,linux/arm64", build.With["platforms"])
	require.Equal(t, "true", build.With["push"])
	require.Equal(t, "${{ steps.prepare.outputs.tag }}", build.With["tags"])
	evidence := stepRunning(t, job, "bash scripts/release.sh seal")
	require.Greater(t, evidence, buildIndex)
	require.Equal(t, "${{ steps.build.outputs.digest }}", job.Steps[evidence].Env["DIGEST"])
	provenanceIndex, provenance := stepUsing(t, job, "actions/attest-build-provenance")
	require.Greater(t, provenanceIndex, evidence)
	require.Equal(t, "${{ steps.build.outputs.digest }}", provenance.With["subject-digest"])
	require.Equal(t, "true", provenance.With["push-to-registry"])
	require.Equal(t, "${{ steps.build.outputs.digest }}", job.Outputs["digest"])
	approval := w.Jobs["promotion"]
	require.Equal(t, []string{"release"}, approval.Needs)
	require.Equal(t, "production", approval.Environment)
	require.Equal(t, map[string]string{"contents": "read", "actions": "read"}, approval.Permissions)
	i := stepRunning(t, approval, "bash scripts/release.sh instructions")
	require.Equal(t, "${{ needs.release.outputs.digest }}", approval.Steps[i].Env["DIGEST"])
	_, artifact := stepUsing(t, job, "actions/upload-artifact")
	require.Equal(t, "always()", artifact.If)
	require.Equal(t, "reports/", artifact.With["path"])
}

// External tool doubles exercise the executable scripts without a registry,
// OIDC token, Docker daemon or mutating a developer's checkout.
func runReleaseScript(t *testing.T, script, mode, failure string, extra ...string) (string, string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if runtime.GOOS == "windows" {
		bash = `C:\Program Files\Git\bin\bash.exe`
		_, err = os.Stat(bash)
	}
	require.NoError(t, err)
	dir := t.TempDir()
	for _, sub := range []string{"scripts", "web", "fake"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, sub), 0755))
	}
	data, err := os.ReadFile(filepath.Join(releaseRoot(), "scripts", script))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", script), data, 0755))
	stub := `#!/usr/bin/env bash
set -eu
name=${0##*/}
printf '%s' "$name" >> "$TRACE"
printf '|%s' "$@" >> "$TRACE"
printf '\n' >> "$TRACE"
if [[ "$name" == "$FAIL_TOOL" ]]; then exit 23; fi
if [[ "$name" == syft ]]; then
  for arg in "$@"; do
    if [[ "$arg" == *-json=* ]]; then printf '{}\n' > "${arg#*=}"; fi
  done
fi
if [[ "$name" == kustomize ]]; then printf 'kind: Deployment\n'; fi
if [[ "$name" == git && "${1:-}" == rev-parse ]]; then printf '%s\n' "$GITHUB_SHA"; fi
if [[ "$name" == gh ]]; then printf '%s\n' "$ENVIRONMENT_JSON"; fi
`
	for _, tool := range []string{"syft", "trivy", "cosign", "npm", "npx", "go", "git", "kustomize", "kubeconform", "actionlint", "shellcheck", "gh"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "fake", tool), []byte(stub), 0755))
	}
	trace := filepath.Join(dir, "trace")
	// Git Bash needs POSIX PATH syntax; convert inside Bash rather than assuming
	// the host path separator is also the shell separator.
	cmd := exec.Command(bash, "-c", `export PATH="$(cd "$FAKE_BIN" && pwd):$PATH"; bash "$SCRIPT" "$MODE"`)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FAKE_BIN="+filepath.ToSlash(filepath.Join(dir, "fake")), "SCRIPT=scripts/"+script, "MODE="+mode,
		"TRACE="+filepath.ToSlash(trace), "FAIL_TOOL="+failure,
		"IMAGE=ghcr.io/example/portal", "DIGEST=sha256:"+strings.Repeat("a", 64),
		"GITHUB_REPOSITORY=example/portal", "GITHUB_REF=refs/tags/v1.2.3", "GITHUB_SHA="+strings.Repeat("b", 40),
		"GITHUB_RUN_ID=123", "GITHUB_RUN_ATTEMPT=1")
	cmd.Env = append(cmd.Env, extra...)
	out, runErr := cmd.CombinedOutput()
	log, _ := os.ReadFile(trace)
	return string(log), string(out), runErr
}

func TestReleaseSourceVerificationExecutesRequiredChecks(t *testing.T) {
	log, output, err := runReleaseScript(t, "verify.sh", "source", "")
	require.NoError(t, err, output)
	previous := -1
	for _, command := range []string{"npm|ci", "npx|--no-install|playwright|install|--with-deps|chrome", "npm|run|build", "npm|run|lint", "npm|test", "go|mod|verify", "go|test|./...|-count=1|-json", "go|test|-race|./...|-count=1|-json", "go|vet|./...", "kustomize|build|deploy/overlays/homelab", "kubeconform|-strict|-summary|-kubernetes-version|1.36.4", "actionlint", "shellcheck|scripts/verify.sh", "git|diff|--exit-code|HEAD|--|go.mod|go.sum|web/package.json|web/package-lock.json"} {
		require.Contains(t, log, command)
		index := strings.Index(log, command)
		require.Greater(t, index, previous, "verification commands must keep their dependency order")
		previous = index
	}
	require.NotContains(t, log, "--ignore-missing-schemas")
	require.Contains(t, log, "git|archive|--format=tar|--output=reports/tested-source.tar|HEAD")
	for _, tool := range []string{"npm", "go", "kustomize", "kubeconform", "actionlint", "shellcheck", "git"} {
		t.Run(tool+" failure blocks success", func(t *testing.T) {
			_, _, err := runReleaseScript(t, "verify.sh", "source", tool)
			require.Error(t, err)
		})
	}
}

func TestReleaseCIScanFailsOnVulnerabilities(t *testing.T) {
	log, output, err := runReleaseScript(t, "verify.sh", "image", "")
	require.NoError(t, err, output)
	require.Equal(t, "trivy|image|--image-src|docker|--scanners|vuln|--severity|HIGH,CRITICAL|--ignorefile|/dev/null|--exit-code|1|--format|json|--output|reports/trivy-ci.json|portal:ci\n", log)
	_, _, err = runReleaseScript(t, "verify.sh", "image", "trivy")
	require.Error(t, err)
}

func TestReleaseSealScansBothPlatformsBeforeSigningDigest(t *testing.T) {
	log, output, err := runReleaseScript(t, "release.sh", "seal", "")
	require.NoError(t, err, output)
	ref := "ghcr.io/example/portal@sha256:" + strings.Repeat("a", 64)
	for _, arch := range []string{"amd64", "arm64"} {
		require.Contains(t, log, "syft|registry:"+ref+"|--platform|linux/"+arch)
		require.Contains(t, log, "cyclonedx-json=reports/sbom-"+arch+".cdx.json")
		require.Contains(t, log, "spdx-json=reports/sbom-"+arch+".spdx.json")
		require.Contains(t, log, "trivy|image|--image-src|remote|--platform|linux/"+arch+"|--scanners|vuln|--severity|HIGH,CRITICAL|--ignorefile|/dev/null|--exit-code|1")
		for _, format := range []string{"cyclonedx", "spdxjson"} {
			require.Contains(t, log, "cosign|attest|--yes|--type|"+format)
		}
		require.Contains(t, log, "cosign|attest|--yes|--type|cyclonedx|--predicate|reports/sbom-"+arch+".cdx.json|"+ref)
		require.Contains(t, log, "cosign|attest|--yes|--type|spdxjson|--predicate|reports/sbom-"+arch+".spdx.json|"+ref)
	}
	require.Contains(t, log, "cosign|sign|--yes|"+ref)
	require.Less(t, strings.LastIndex(log, "trivy|"), strings.Index(log, "cosign|sign|"))
	require.Contains(t, log, "cosign|verify|"+ref)
	require.Contains(t, log, "--certificate-identity|https://github.com/example/portal/.github/workflows/release.yaml@refs/tags/v1.2.3")
	require.Contains(t, log, "--certificate-oidc-issuer|https://token.actions.githubusercontent.com")
	require.NotContains(t, output, "GitOps")
	for _, tool := range []string{"syft", "trivy", "cosign"} {
		t.Run(tool+" failure blocks release", func(t *testing.T) {
			log, out, err := runReleaseScript(t, "release.sh", "seal", tool)
			require.Error(t, err)
			require.NotContains(t, out, "GitOps")
			if tool != "cosign" {
				require.NotContains(t, log, "cosign|")
			}
		})
	}
	for _, badDigest := range []string{"latest", "sha256:123", ""} {
		log, _, err := runReleaseScript(t, "release.sh", "seal", "", "DIGEST="+badDigest)
		require.Error(t, err)
		require.Empty(t, log, "invalid inputs must fail before external calls")
	}
}

func TestReleaseRequiresRealReviewerProtection(t *testing.T) {
	protected := `{"protection_rules":[{"type":"required_reviewers","prevent_self_review":true,"reviewers":[{"type":"User","reviewer":{"login":"operator"}}]}]}`
	for _, mode := range []string{"prepare", "instructions"} {
		t.Run(mode, func(t *testing.T) {
			outputFile := filepath.ToSlash(filepath.Join(t.TempDir(), "output"))
			extra := []string{"ENVIRONMENT_JSON=" + protected, "GITHUB_OUTPUT=" + outputFile, "GITHUB_STEP_SUMMARY=" + outputFile}
			log, output, err := runReleaseScript(t, "release.sh", mode, "", extra...)
			require.NoError(t, err, output)
			require.Contains(t, log, "gh|api|repos/example/portal/environments/production")
			published, err := os.ReadFile(outputFile)
			require.NoError(t, err)
			if mode == "prepare" {
				require.Contains(t, string(published), "tag=ghcr.io/example/portal:sha-"+strings.Repeat("b", 40)+"-123-1")
			} else {
				require.Contains(t, string(published), "ghcr.io/example/portal@sha256:"+strings.Repeat("a", 64))
				require.NotContains(t, string(published), ":latest")
			}
			for _, unprotected := range []string{`{}`, `{"protection_rules":[{"type":"required_reviewers","prevent_self_review":false,"reviewers":[{}]}]}`, `{"protection_rules":[{"type":"required_reviewers","prevent_self_review":true,"reviewers":[]}]}`} {
				_, out, err := runReleaseScript(t, "release.sh", mode, "", append(extra, "ENVIRONMENT_JSON="+unprotected)...)
				require.Error(t, err)
				require.NotContains(t, out, "Approved GitOps")
			}
			_, out, err := runReleaseScript(t, "release.sh", mode, "gh", extra...)
			require.Error(t, err)
			require.NotContains(t, out, "Approved GitOps")
		})
	}
}
