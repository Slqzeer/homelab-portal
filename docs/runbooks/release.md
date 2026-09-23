# Release and rollback

The release workflow produces one image index for `linux/amd64,linux/arm64` and publishes its verified digest and application Git revision for a later, manually reviewed GitOps change. It never edits manifests, pushes Git changes, or invokes Argo CD sync.

## Configure once before releasing

1. Protect `main` with review and the `Test / verify` check. Protect version tags (`v*`) against update/deletion and restrict creation to release maintainers. A release tag must identify a reviewed full commit SHA; never move or reuse it.
2. Create the GitHub environment `production`, select custom deployment branches and tags, and add a tag policy named exactly `v*` (`type: tag`). This private repository plan does not support required reviewers, so the environment does not pause for GitHub approval. The workflow checks the environment settings and tag-policy list before building and again before the handoff; a missing or wrong policy or API error blocks release. Repository administrators remain a trust boundary.
3. Enable Actions publishing to the repository's GHCR package. Only the built-in `GITHUB_TOKEN` is passed to the registry login action; signing uses GitHub OIDC. Do not create credential files, pass credentials in CLI arguments/build arguments, or enable shell tracing. The environment inspection needs `actions: read`; the publishing job alone receives `packages: write` and `id-token: write`. GitHub-native artifact attestation persistence is unavailable for this user-owned private repository; the workflow must not invoke it.
4. Preserve the GHCR digest, its BuildKit SLSA provenance, Cosign signature/SBOM attestations, and downloaded release evidence for every deployed/rollback revision. Never retag or overwrite published images. Every workflow attempt uses a unique `sha-<40-character-commit>-<run-id>-<attempt>` tag. Only the content-addressed `@sha256:...` index is a deployment input; a registry administrator can still mutate tags.

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
3. The build-push action publishes BuildKit SLSA provenance in `mode=max` as an attestation manifest linked to each platform in the immutable index. After both scans pass, the job fetches that index by digest and fails unless both platform manifests have linked attestation manifests. Only then does Cosign sign the index digest and attach the four SBOM attestations using OIDC; the job verifies signature/attestations against the exact repository, release workflow, version tag and source SHA. No deployment instructions are emitted if any check fails. GitHub-native artifact attestations are not produced on this plan.
4. After publication, download `release-evidence-<run-id>-<attempt>` and the verification artifact. The `promotion` job does not wait for GitHub approval on this plan: the owner must review this evidence before any GitOps promotion. Review the source revision, `image-index.json`, both SBOM pairs and both Trivy reports. Verify each report's source/platform and timestamp, the index's two platform manifests and their linked BuildKit attestation manifests. Check the workflow run is from the expected tag/commit and that Buildx reported `mode=max` provenance for both platforms. A scan is time-sensitive: rescan both platforms immediately before a delayed GitOps promotion with the same severity policy.
5. Independently verify the digest using Cosign 3.1.3 and Docker Buildx 0.37.1. Set `IMAGE_REF` to the **complete value from `image-digest.txt`**, `RELEASE_TAG` to the tag, and `SOURCE_SHA` to the tested commit; these are public identifiers, not credentials:

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
docker buildx imagetools inspect "$IMAGE_REF" --format '{{json .Manifest}}' | jq -e --arg digest "${IMAGE_REF##*@}" '
  . as $index |
  $index.digest == $digest and
  all(["amd64", "arm64"][]; . as $arch |
    [$index.manifests[]? | select(.platform.os == "linux" and .platform.architecture == $arch) | .digest] as $images |
    ($images | length) == 1 and
    any($index.manifests[]?;
      .annotations."vnd.docker.reference.type" == "attestation-manifest" and
      .annotations."vnd.docker.reference.digest" == $images[0]))'
docker buildx imagetools inspect "$IMAGE_REF" --format '{{json .Provenance.SLSA}}' | jq -e '.buildType == "https://mobyproject.org/buildkit@v1"'
```

Stop on any nonzero exit. Compare the freshly fetched index with `image-index.json`; verify each `linux/amd64` and `linux/arm64` descriptor has an `attestation-manifest` whose `vnd.docker.reference.digest` matches that platform digest. Inspect BuildKit's SLSA provenance and build log for the expected source repository, commit, run identity, and both platforms; an attestation-manifest link alone does not prove predicate contents. Inspect the verified Cosign attestation subjects/predicates and match both platform SBOMs to the downloaded files; do not accept a different workflow identity or arbitrary issuer. The signed multi-platform index digest commits to its child platform and BuildKit attestation manifests.

6. The `promotion` job prints the exact digest and application commit SHA without an approval pause. The owner completes the manual evidence and identity checks in steps 4 and 5, then opens a separate reviewed change in the GitOps repository pinning that digest and immutable application revision. Record the release run, evidence, and owner review. Task 10 owns the actual external repository paths, Argo Application and centralized Tailscale Ingress. Do not deploy a tag, `latest`, a branch name, or the sample all-zero digest in this repository's overlay.
7. After that GitOps change is reviewed/merged, check Argo CD reports the intended Git revision, `Synced` and `Healthy`, and Kubernetes runs the corresponding platform image from the reviewed index. Confirm rollout readiness, catalogue/auth behavior and alerts. This pipeline does **not** establish cluster acceptance: Task 7's API-server dry run/admission, VSO synchronization, real RBAC negative checks, CNI enforcement, Tailscale-only routing, and live alert evaluation remain mandatory Task 9/10 gates.

The listener-separation release requires a coordinated application/GitOps change:
ConfigMap `PORT=8080` and `OPERATIONS_PORT=8081`, named container/Service ports
`public` and `operations`, kubelet probes and ServiceMonitor on `operations`, and
separate proxy-to-public/scraper-to-operations NetworkPolicy rules. Every external
Tailscale Ingress backend must select Service port `public` only. Its `/` prefix
is safe because operational routes are absent from that listener. Retain the
[isolated acceptance report](acceptance.md) proving the exact public Pod route,
public operational 404s, and internal health/readiness/metrics observations.
Non-default ports require matching configuration and manifest changes.

## Rollback

Locate the previous reviewed GitOps commit and its retained digest/evidence. Revert the promotion commit in the GitOps repository through review, restoring the image, application revision, and matching listener manifests together. A single-listener image cannot satisfy the internal-only operations acceptance gate behind a Tailscale `/` prefix; do not expose that image without a separately reviewed isolation mechanism. Do not rebuild an old tag, mutate a tag, edit live Kubernetes resources, or use an unreviewed Argo override. Once reconciled, verify Argo revision/health, readiness, authentication/catalogue behavior and alerts again. Record the reverted revision, restored digest and outcome in the release record.
