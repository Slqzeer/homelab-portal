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
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Needs           []string          `yaml:"needs"`
	Uses            string            `yaml:"uses"`
	Environment     string            `yaml:"environment"`
	Permissions     map[string]string `yaml:"permissions"`
	Outputs         map[string]string `yaml:"outputs"`
	Steps           []workflowStep    `yaml:"steps"`
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
	// Only mutation-test child processes set this, to inspect an isolated fixture.
	if root := os.Getenv("PORTAL_RELEASE_CONTRACT_ROOT"); root != "" {
		return root
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// Reuse the actual contracts in a child test process; mutation tests must prove
// those assertions fail, rather than implementing a second, weaker validator.
func rejectReleaseMutation(t *testing.T, relativePath, targetTest string, mutate func(string) string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(releaseRoot(), relativePath))
	require.NoError(t, err)
	original := strings.ReplaceAll(string(data), "\r\n", "\n")
	mutated := mutate(original)
	require.NotEqual(t, original, mutated, "mutation must change the fixture")
	root := t.TempDir()
	fixture := filepath.Join(root, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fixture), 0755))
	require.NoError(t, os.WriteFile(fixture, []byte(mutated), 0644))
	cmd := exec.Command(os.Args[0], "-test.run=^"+targetTest+"$")
	cmd.Env = append(os.Environ(), "PORTAL_RELEASE_CONTRACT_ROOT="+root)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "contract accepted mutation:\n%s", out)
	require.Contains(t, string(out), "--- FAIL: "+targetTest, "must fail the contract, not test startup")
}

func TestReleaseRejectsJobGateMutations(t *testing.T) {
	for _, job := range []struct{ workflow, name, contract string }{
		{"test", "verify", "TestReleasePullRequestChecks"},
		{"release", "test", "TestReleaseImmutableSupplyChain"},
		{"release", "release", "TestReleaseImmutableSupplyChain"},
		{"release", "promotion", "TestReleaseImmutableSupplyChain"},
	} {
		for _, control := range []string{"if: always()", "if: false", "continue-on-error: true", "continue-on-error: '${{ true }}'"} {
			t.Run(job.name+"/"+control, func(t *testing.T) {
				rejectReleaseMutation(t, ".github/workflows/"+job.workflow+".yaml", job.contract, func(s string) string {
					return strings.Replace(s, "  "+job.name+":\n", "  "+job.name+":\n    "+control+"\n", 1)
				})
			})
		}
	}
}

func TestReleaseRejectsSealMutations(t *testing.T) {
	cdx := `    cosign verify-attestation "$ref" --type cyclonedx "${verification[@]}" > reports/attestation-cyclonedx.json` + "\n"
	spdx := `    cosign verify-attestation "$ref" --type spdxjson "${verification[@]}" > reports/attestation-spdx.json` + "\n"
	mutations := map[string]func(string) string{
		"scan tag instead of digest": func(s string) string {
			return strings.Replace(s, `--output "reports/trivy-$arch.json" "$ref"`, `--output "reports/trivy-$arch.json" "$IMAGE:latest"`, 1)
		},
		"omit CycloneDX verification": func(s string) string { return strings.Replace(s, cdx, "", 1) },
		"omit SPDX verification":      func(s string) string { return strings.Replace(s, spdx, "", 1) },
		"verify wrong digest": func(s string) string {
			return strings.Replace(s, cdx, strings.Replace(cdx, `"$ref"`, `"$IMAGE:latest"`, 1), 1)
		},
		"omit attestation issuer": func(s string) string {
			return strings.Replace(s, cdx, strings.Replace(cdx, `"${verification[@]}"`, `--certificate-identity "$identity" --certificate-github-workflow-sha "$GITHUB_SHA"`, 1), 1)
		},
		"omit attestation identity": func(s string) string {
			return strings.Replace(s, spdx, strings.Replace(spdx, `"${verification[@]}"`, `--certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-github-workflow-sha "$GITHUB_SHA"`, 1), 1)
		},
		"omit source SHA": func(s string) string {
			return strings.Replace(s, ` --certificate-github-workflow-sha "$GITHUB_SHA"`, "", 1)
		},
		"verify attestations before signature": func(s string) string {
			s = strings.Replace(s, cdx+spdx, "", 1)
			return strings.Replace(s, `    cosign verify "$ref"`, cdx+spdx+`    cosign verify "$ref"`, 1)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			rejectReleaseMutation(t, "scripts/release.sh", "TestReleaseSealScansBothPlatformsBeforeSigningDigest", mutate)
		})
	}
}
func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(releaseRoot(), ".github", "workflows", name+".yaml"))
	require.NoError(t, err)
	var w workflow
	require.NoError(t, yaml.Unmarshal(data, &w))
	require.Empty(t, w.Permissions, "grant permissions only per job")
	for _, job := range w.Jobs {
		require.Empty(t, job.If, "jobs must preserve GitHub's default success dependency gate")
		require.False(t, job.ContinueOnError, "a failed verification or release job must block dependents")
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
	require.Equal(t, map[string]string{"contents": "read", "actions": "read", "packages": "write", "id-token": "write"}, job.Permissions)
	preflight := stepRunning(t, job, "bash scripts/release.sh prepare")
	_, login := stepUsing(t, job, "docker/login-action")
	require.Equal(t, "${{ secrets.GITHUB_TOKEN }}", login.With["password"])
	require.Equal(t, "ghcr.io", login.With["registry"])
	buildIndex, build := stepUsing(t, job, "docker/build-push-action")
	require.Greater(t, buildIndex, preflight)
	require.Equal(t, "linux/amd64,linux/arm64", build.With["platforms"])
	require.Equal(t, "true", build.With["push"])
	require.Equal(t, "mode=max", build.With["provenance"])
	require.Equal(t, "${{ steps.prepare.outputs.tag }}", build.With["tags"])
	evidence := stepRunning(t, job, "bash scripts/release.sh seal")
	require.Greater(t, evidence, buildIndex)
	require.Equal(t, "${{ steps.build.outputs.digest }}", job.Steps[evidence].Env["DIGEST"])
	for _, step := range job.Steps {
		require.NotContains(t, step.Uses, "attest-build-provenance")
	}
	require.Equal(t, "${{ steps.build.outputs.digest }}", job.Outputs["digest"])
	promotion := w.Jobs["promotion"]
	require.Equal(t, []string{"release"}, promotion.Needs)
	require.Equal(t, "production", promotion.Environment)
	require.Equal(t, map[string]string{"contents": "read", "actions": "read"}, promotion.Permissions)
	i := stepRunning(t, promotion, "bash scripts/release.sh instructions")
	require.Equal(t, "${{ needs.release.outputs.digest }}", promotion.Steps[i].Env["DIGEST"])
	_, artifact := stepUsing(t, job, "actions/upload-artifact")
	require.Equal(t, "always()", artifact.If)
	require.Equal(t, "reports/", artifact.With["path"])
}

func TestReleaseBuildToolsArePinned(t *testing.T) {
	for _, name := range []string{"test", "release"} {
		t.Run(name, func(t *testing.T) {
			w := readWorkflow(t, name)
			jobName := "verify"
			if name == "release" {
				jobName = "release"
			}
			_, builder := stepUsing(t, w.Jobs[jobName], "docker/setup-buildx-action")
			require.Equal(t, "v0.37.1", builder.With["version"])
			require.Equal(t, "docker-container", builder.With["driver"])
			require.Equal(t, "image=moby/buildkit:v0.33.0@sha256:6c2fa84a6b61ccd72899dde4239f8d5717f05f9a8ca6f3cad185fb1a95a94de3", builder.With["driver-opts"])
			if name == "release" {
				_, qemu := stepUsing(t, w.Jobs[jobName], "docker/setup-qemu-action")
				require.Equal(t, "tonistiigi/binfmt:qemu-v10.2.3-68@sha256:400a4873b838d1b89194d982c45e5fb3cda4593fbfd7e08a02e76b03b21166f0", qemu.With["image"])
			}
		})
	}
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
if [[ -n "${FAIL_COMMAND:-}" && "$name $*" == "$FAIL_COMMAND"* ]]; then exit 23; fi
if [[ "$name" == syft ]]; then
  for arg in "$@"; do
    if [[ "$arg" == *-json=* ]]; then printf '{}\n' > "${arg#*=}"; fi
  done
fi
if [[ "$name" == kustomize ]]; then printf 'kind: Deployment\n'; fi
if [[ "$name" == docker && "${1:-}" == buildx && "${2:-}" == imagetools && "${3:-}" == inspect ]]; then printf '%s\n' "$INDEX_JSON"; fi
if [[ "$name" == git && "${1:-}" == rev-parse ]]; then printf '%s\n' "$GITHUB_SHA"; fi
if [[ "$name" == gh ]]; then
  if [[ "$*" == "api repos/$GITHUB_REPOSITORY/environments/production" ]]; then
    printf '%s\n' "$ENVIRONMENT_JSON"
  elif [[ "$*" == "api --paginate repos/$GITHUB_REPOSITORY/environments/production/deployment-branch-policies?per_page=100" ]]; then
    printf '%s\n' "$POLICIES_JSON"
  else
    exit 22
  fi
fi
`
	for _, tool := range []string{"syft", "trivy", "cosign", "npm", "npx", "go", "git", "kustomize", "kubeconform", "actionlint", "shellcheck", "gh", "docker"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "fake", tool), []byte(stub), 0755))
	}
	trace := filepath.Join(dir, "trace")
	// Git Bash needs POSIX PATH syntax; convert inside Bash rather than assuming
	// the host path separator is also the shell separator.
	cmd := exec.Command(bash, "-c", `export PATH="$(cd "$FAKE_BIN" && pwd):$PATH"; bash "$SCRIPT" "$MODE"`)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FAKE_BIN="+filepath.ToSlash(filepath.Join(dir, "fake")), "SCRIPT=scripts/"+script, "MODE="+mode,
		"TRACE="+filepath.ToSlash(trace), "FAIL_TOOL="+failure,
		"INDEX_JSON="+validReleaseIndex(),
		"IMAGE=ghcr.io/example/portal", "DIGEST=sha256:"+strings.Repeat("a", 64),
		"GITHUB_REPOSITORY=example/portal", "GITHUB_REF=refs/tags/v1.2.3", "GITHUB_SHA="+strings.Repeat("b", 40),
		"GITHUB_RUN_ID=123", "GITHUB_RUN_ATTEMPT=1")
	cmd.Env = append(cmd.Env, extra...)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		_, artifactErr := os.Stat(filepath.Join(dir, "reports", "image-digest.txt"))
		require.ErrorIs(t, artifactErr, os.ErrNotExist, "a failed release must not publish its digest artifact")
	}
	log, _ := os.ReadFile(trace)
	return string(log), string(out), runErr
}
func validReleaseIndex() string {
	amd64 := "sha256:" + strings.Repeat("c", 64)
	arm64 := "sha256:" + strings.Repeat("d", 64)
	return `{"digest":"sha256:` + strings.Repeat("a", 64) + `","manifests":[` +
		`{"digest":"` + amd64 + `","platform":{"os":"linux","architecture":"amd64"}},` +
		`{"digest":"` + arm64 + `","platform":{"os":"linux","architecture":"arm64"}},` +
		`{"digest":"sha256:` + strings.Repeat("e", 64) + `","platform":{"os":"unknown","architecture":"unknown"},"annotations":{"vnd.docker.reference.type":"attestation-manifest","vnd.docker.reference.digest":"` + amd64 + `"}},` +
		`{"digest":"sha256:` + strings.Repeat("f", 64) + `","platform":{"os":"unknown","architecture":"unknown"},"annotations":{"vnd.docker.reference.type":"attestation-manifest","vnd.docker.reference.digest":"` + arm64 + `"}}]}`
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
	verification := "|--certificate-identity|https://github.com/example/portal/.github/workflows/release.yaml@refs/tags/v1.2.3|--certificate-oidc-issuer|https://token.actions.githubusercontent.com|--certificate-github-workflow-sha|" + strings.Repeat("b", 40)
	// Full argv and order are the contract: a nearby correct identity or digest
	// in a different command cannot satisfy another command's verification.
	expected := []string{
		"syft|registry:" + ref + "|--platform|linux/amd64|-o|cyclonedx-json=reports/sbom-amd64.cdx.json|-o|spdx-json=reports/sbom-amd64.spdx.json",
		"trivy|image|--image-src|remote|--platform|linux/amd64|--scanners|vuln|--severity|HIGH,CRITICAL|--ignorefile|/dev/null|--exit-code|1|--format|json|--output|reports/trivy-amd64.json|" + ref,
		"syft|registry:" + ref + "|--platform|linux/arm64|-o|cyclonedx-json=reports/sbom-arm64.cdx.json|-o|spdx-json=reports/sbom-arm64.spdx.json",
		"trivy|image|--image-src|remote|--platform|linux/arm64|--scanners|vuln|--severity|HIGH,CRITICAL|--ignorefile|/dev/null|--exit-code|1|--format|json|--output|reports/trivy-arm64.json|" + ref,
		"docker|buildx|imagetools|inspect|" + ref + "|--format|{{json .Manifest}}",
		"cosign|sign|--yes|" + ref,
		"cosign|attest|--yes|--type|cyclonedx|--predicate|reports/sbom-amd64.cdx.json|" + ref,
		"cosign|attest|--yes|--type|spdxjson|--predicate|reports/sbom-amd64.spdx.json|" + ref,
		"cosign|attest|--yes|--type|cyclonedx|--predicate|reports/sbom-arm64.cdx.json|" + ref,
		"cosign|attest|--yes|--type|spdxjson|--predicate|reports/sbom-arm64.spdx.json|" + ref,
		"cosign|verify|" + ref + verification,
		"cosign|verify-attestation|" + ref + "|--type|cyclonedx" + verification,
		"cosign|verify-attestation|" + ref + "|--type|spdxjson" + verification,
	}
	require.Equal(t, expected, strings.Split(strings.TrimSpace(log), "\n"))
	require.NotContains(t, output, "GitOps")
	// Each late-stage failure must stop exactly there, including the second
	// architecture scan and each individual attestation verification.
	for index, command := range expected {
		t.Run("failure at "+command, func(t *testing.T) {
			failedLog, _, err := runReleaseScript(t, "release.sh", "seal", "", "FAIL_COMMAND="+strings.ReplaceAll(command, "|", " "))
			require.Error(t, err)
			require.Equal(t, expected[:index+1], strings.Split(strings.TrimSpace(failedLog), "\n"))
		})
	}
	for _, tool := range []string{"syft", "trivy", "cosign", "docker"} {
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
	for name, index := range map[string]string{
		"missing index":          `{}`,
		"wrong index digest":     strings.Replace(validReleaseIndex(), `"digest":"sha256:`+strings.Repeat("a", 64)+`"`, `"digest":"sha256:`+strings.Repeat("b", 64)+`"`, 1),
		"missing arm provenance": strings.Replace(validReleaseIndex(), `"vnd.docker.reference.digest":"sha256:`+strings.Repeat("d", 64)+`"`, `"vnd.docker.reference.digest":"sha256:`+strings.Repeat("c", 64)+`"`, 1),
		"wrong attestation type": strings.ReplaceAll(validReleaseIndex(), `"vnd.docker.reference.type":"attestation-manifest"`, `"vnd.docker.reference.type":"other"`),
	} {
		t.Run(name, func(t *testing.T) {
			log, _, err := runReleaseScript(t, "release.sh", "seal", "", "INDEX_JSON="+index)
			require.Error(t, err)
			require.NotContains(t, log, "cosign|sign")
		})
	}
}

func TestReleaseRequiresProductionTagPolicy(t *testing.T) {
	environment := `{"name":"production","protection_rules":[],"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}`
	policies := `{"total_count":1,"branch_policies":[{"id":1,"name":"v*","type":"tag"}]}`
	for _, mode := range []string{"prepare", "instructions"} {
		t.Run(mode, func(t *testing.T) {
			outputFile := filepath.ToSlash(filepath.Join(t.TempDir(), "output"))
			extra := []string{"ENVIRONMENT_JSON=" + environment, "POLICIES_JSON=" + policies, "GITHUB_OUTPUT=" + outputFile, "GITHUB_STEP_SUMMARY=" + outputFile}
			log, output, err := runReleaseScript(t, "release.sh", mode, "", extra...)
			require.NoError(t, err, output)
			require.Contains(t, log, "gh|api|repos/example/portal/environments/production\n")
			require.Contains(t, log, "gh|api|--paginate|repos/example/portal/environments/production/deployment-branch-policies?per_page=100\n")
			published, err := os.ReadFile(outputFile)
			require.NoError(t, err)
			if mode == "prepare" {
				require.Contains(t, string(published), "tag=ghcr.io/example/portal:sha-"+strings.Repeat("b", 40)+"-123-1")
			} else {
				require.Contains(t, string(published), "Release evidence ready for owner review")
				require.Contains(t, string(published), "Owner must review release evidence before GitOps promotion.")
				require.Contains(t, string(published), "ghcr.io/example/portal@sha256:"+strings.Repeat("a", 64))
				require.NotContains(t, string(published), ":latest")
				require.Contains(t, string(published), "Ingress backend: Service port public (8080) only")
				require.Contains(t, string(published), "operations (8081) stays internal")
				require.Contains(t, string(published), "docs/runbooks/acceptance.md")
			}
			for _, invalid := range []string{
				`{}`,
				`{"name":"production","deployment_branch_policy":null}`,
				`{"name":"production","deployment_branch_policy":{"protected_branches":true,"custom_branch_policies":false}}`,
				`{"name":"production","deployment_branch_policy":{"protected_branches":true,"custom_branch_policies":true}}`,
			} {
				log, out, err := runReleaseScript(t, "release.sh", mode, "", append(extra, "ENVIRONMENT_JSON="+invalid)...)
				require.Error(t, err)
				require.NotContains(t, log, "deployment-branch-policies")
				require.NotContains(t, out, "Release evidence ready")
			}
			for _, invalid := range []string{
				`{}`,
				`{"total_count":0,"branch_policies":[]}`,
				`{"total_count":1,"branch_policies":[{"name":"v*","type":"branch"}]}`,
				`{"total_count":1,"branch_policies":[{"name":"v1.*","type":"tag"}]}`,
				`{"total_count":1,"branch_policies":[{"name":"v*"}]}`,
			} {
				_, out, err := runReleaseScript(t, "release.sh", mode, "", append(extra, "POLICIES_JSON="+invalid)...)
				require.Error(t, err)
				require.NotContains(t, out, "Release evidence ready")
			}
			_, out, err := runReleaseScript(t, "release.sh", mode, "gh", extra...)
			require.Error(t, err)
			require.NotContains(t, out, "Release evidence ready")
			_, out, err = runReleaseScript(t, "release.sh", mode, "", append(extra, "FAIL_COMMAND=gh api --paginate repos/example/portal/environments/production/deployment-branch-policies?per_page=100")...)
			require.Error(t, err)
			require.NotContains(t, out, "Release evidence ready")
		})
	}
}
