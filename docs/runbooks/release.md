# Release and rollback

The release workflow produces one image index for `linux/amd64,linux/arm64` and hands an approved digest and application Git revision to the separate GitOps repository. It never edits manifests, pushes Git changes, or invokes Argo CD sync.

## Configure once before releasing

1. Protect `main` with review and the `Test / verify` check. Protect version tags (`v*`) against update/deletion and restrict creation to release maintainers. A release tag must identify a reviewed full commit SHA; never move or reuse it.
2. Create the GitHub environment `production`, configure at least one required human reviewer, enable **Prevent self-review**, disable administrator bypass, and limit deployment tags to `v*`. Confirm the repository's GitHub plan supports required reviewers. The workflow reads environment protection before building and again before handoff, and fails if reviewers/self-review protection are absent or the API is unavailable. Administrator-bypass settings must be checked in the UI; the REST response does not expose them. Repository administrators remain a trust boundary.
3. Enable Actions publishing to the repository's GHCR package. Only the built-in `GITHUB_TOKEN` is passed to the registry login action; signing uses GitHub OIDC. Do not create credential files, pass credentials in CLI arguments/build arguments, or enable shell tracing. The environment inspection needs `actions: read`; the publishing job alone receives `packages: write`, `id-token: write`, and `attestations: write`. For private repositories, verify the GitHub plan also supports artifact attestations.
4. Preserve the GHCR digest, its Cosign signatures/attestations and GitHub provenance for every deployed/rollback revision. Never retag or overwrite published images. Every workflow attempt uses a unique `sha-<40-character-commit>-<run-id>-<attempt>` tag. Only the content-addressed `@sha256:...` index is a deployment input; a registry administrator can still mutate tags.

## Validate a candidate

PR CI performs `npm ci`, installs Chrome for Playwright's configured channel, builds/checks/tests the web UI, verifies Go modules, runs all Go tests including manifest contracts, race tests and vet, renders the overlay and validates all 12 built-in/CRD resources with strict pinned schemas, lints workflows/scripts, builds the CI image and scans it. Lockfiles must match `HEAD` after checks. Caches contain only npm's package store and Go modules; build outputs and credentials are not cached. Reports are uploaded even on failure.

For local Linux verification, install Node 24.21.0, the Go version from `go.mod`, a C compiler for Go's race detector, Bash, jq, Chrome/Playwright system dependencies, Kustomize 5.7.1, kubeconform 0.7.0, actionlint 1.7.12, ShellCheck, Docker Buildx and Trivy 0.74.0. Run:

```bash
bash scripts/verify.sh source
docker buildx build --platform linux/amd64 --load --tag portal:ci .
bash scripts/verify.sh image
```

`reports/` is ignored local output. CI archives reports for 14 days; release evidence for 90 days. Archive release evidence longer with the operational release record before expiration. Pinned tooling/action updates require review; Dependabot proposes grouped weekly Actions, Go, npm and Docker updates, without automatic merging. Review script-installed tool versions and pinned schema commits manually as well.

Both workflows pin Buildx 0.37.1 and the BuildKit 0.33.0 image by digest through the `docker-container` driver. Release also pins the QEMU binfmt 10.2.3-68 image by digest. Review updates to these action inputs manually together with the release contracts; pinning the setup action's commit alone does not pin the tools it installs.

## Release and review evidence

1. Select the reviewed commit, create a new annotated semantic version tag (`vMAJOR.MINOR.PATCH`, optionally a prerelease suffix), and push that tag. The workflow checks out the event's immutable commit and reruns CI before publishing.
2. The publishing job builds/pushes the two-platform image once. Syft produces both CycloneDX JSON and SPDX JSON for **each platform of that pushed index digest**. Trivy scans both platforms against its current vulnerability database and rejects every HIGH/CRITICAL vulnerability, including unfixed findings, with no ignore file. Network/database/tool failures block release. A rejected candidate can exist in GHCR but must never be promoted.
3. Only after both scans pass, Cosign signs the index digest and attaches the four SBOM attestations using OIDC; the job verifies signature/attestations against the exact repository, release workflow, version tag and source SHA. GitHub build provenance is then attached to the same digest. No deployment instructions are emitted if any step fails.
4. While `promotion` awaits environment review, download `release-evidence-<run-id>-<attempt>` and the verification artifact. Review the source revision, both SBOM pairs and both Trivy reports. Verify each report's source/platform and timestamp, and the immutable index's two platform manifests. Check the workflow run is from the expected tag/commit. A scan is time-sensitive: rescan both platforms immediately before a delayed promotion with the same severity policy.
5. Independently verify the digest using Cosign 3.1.3 and GitHub CLI. Set `IMAGE_REF` to the **complete value from `image-digest.txt`**, `RELEASE_TAG` to the tag, and `SOURCE_SHA` to the tested commit; these are public identifiers, not credentials:

```bash
set -euo pipefail
[[ "$IMAGE_REF" =~ ^ghcr.io/slqzeer/homelab-portal@sha256:[a-f0-9]{64}$ ]]
IDENTITY="https://github.com/Slqzeer/homelab-portal/.github/workflows/release.yaml@refs/tags/$RELEASE_TAG"
cosign verify "$IMAGE_REF" --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-sha "$SOURCE_SHA"
cosign verify-attestation "$IMAGE_REF" --type cyclonedx --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-sha "$SOURCE_SHA"
cosign verify-attestation "$IMAGE_REF" --type spdxjson --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-sha "$SOURCE_SHA"
gh attestation verify "oci://$IMAGE_REF" --repo Slqzeer/homelab-portal \
  --signer-workflow Slqzeer/homelab-portal/.github/workflows/release.yaml \
  --source-digest "$SOURCE_SHA"
```

Stop on any nonzero exit. Inspect the verified attestation subjects/predicates and match both platform SBOMs to the downloaded files; do not accept a different workflow identity or an arbitrary issuer. The multi-platform index digest commits to its child platform digests.

6. The configured human reviewer approves `production` after these checks. The promotion job prints the exact digest and application commit SHA. Open a separate reviewed change in the GitOps repository pinning that digest and immutable application revision; record the release run/evidence and approval. Task 10 owns the actual external repository paths, Argo Application and centralized Tailscale Ingress. Do not deploy a tag, `latest`, a branch name, or the sample all-zero digest in this repository's overlay.
7. After that GitOps change is reviewed/merged, check Argo CD reports the intended Git revision, `Synced` and `Healthy`, and Kubernetes runs the corresponding platform image from the approved index. Confirm rollout readiness, catalogue/auth behavior and alerts. This pipeline does **not** establish cluster acceptance: Task 7's API-server dry run/admission, VSO synchronization, real RBAC negative checks, CNI enforcement, Tailscale-only routing, and live alert evaluation remain mandatory Task 9/10 gates.

## Rollback

Locate the previous reviewed GitOps commit and its retained digest/evidence. Revert the promotion commit in the GitOps repository through review, restoring both the prior image digest and application revision. Do not rebuild an old tag, mutate a tag, edit live Kubernetes resources, or use an unreviewed Argo override. Once reconciled, verify Argo revision/health, readiness, authentication/catalogue behavior and alerts again. Record the reverted revision, restored digest and outcome in the release record.
