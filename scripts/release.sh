#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { printf '%s\n' "$1" >&2; exit 1; }
validate_source() {
  [[ "${GITHUB_REPOSITORY:-}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail 'Invalid repository'
  [[ "${GITHUB_REF:-}" =~ ^refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]] || fail 'Release requires a version tag'
  [[ "${GITHUB_SHA:-}" =~ ^[a-f0-9]{40}$ ]] || fail 'Release requires a full commit SHA'
}
validate_digest() {
  validate_source
  [[ "${IMAGE:-}" == "ghcr.io/${GITHUB_REPOSITORY,,}" ]] || fail 'Unexpected image repository'
  [[ "${DIGEST:-}" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'Release requires an immutable sha256 digest'
  ref="$IMAGE@$DIGEST"
}
check_approval_rules() {
  # A named environment alone does not require approval. Fail closed on missing
  # settings, insufficient API permissions, API errors or unsupported plans.
  gh api "repos/$GITHUB_REPOSITORY/environments/production" |
    jq -e 'any(.protection_rules[]?;
      .type == "required_reviewers" and (.reviewers | length) > 0)' > /dev/null
}

case "${1:-}" in
  prepare)
    validate_source
    [[ "$(git rev-parse HEAD)" == "$GITHUB_SHA" ]] || fail 'Checkout does not match the release commit'
    [[ "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ && "${GITHUB_RUN_ATTEMPT:-}" =~ ^[0-9]+$ ]] || fail 'Missing unique run identity'
    check_approval_rules
    image="ghcr.io/${GITHUB_REPOSITORY,,}"
    # Every run attempt gets a new tag; tags are never promotion inputs.
    tag="$image:sha-$GITHUB_SHA-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"
    printf 'image=%s\ntag=%s\n' "$image" "$tag" >> "${GITHUB_OUTPUT:?}"
    mkdir -p reports
    printf '%s\n' "$GITHUB_SHA" > reports/source-revision.txt
    ;;
  seal)
    validate_digest
    mkdir -p reports
    for arch in amd64 arm64; do
      syft "registry:$ref" --platform "linux/$arch" \
        -o "cyclonedx-json=reports/sbom-$arch.cdx.json" \
        -o "spdx-json=reports/sbom-$arch.spdx.json"
      test -s "reports/sbom-$arch.cdx.json"
      test -s "reports/sbom-$arch.spdx.json"
      trivy image --image-src remote --platform "linux/$arch" --scanners vuln \
        --severity HIGH,CRITICAL --ignorefile /dev/null --exit-code 1 \
        --format json --output "reports/trivy-$arch.json" "$ref"
    done
    cosign sign --yes "$ref"
    for arch in amd64 arm64; do
      cosign attest --yes --type cyclonedx --predicate "reports/sbom-$arch.cdx.json" "$ref"
      cosign attest --yes --type spdxjson --predicate "reports/sbom-$arch.spdx.json" "$ref"
    done
    identity="https://github.com/$GITHUB_REPOSITORY/.github/workflows/release.yaml@$GITHUB_REF"
    verification=(--certificate-identity "$identity" --certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-github-workflow-sha "$GITHUB_SHA")
    cosign verify "$ref" "${verification[@]}" > reports/signature.json
    cosign verify-attestation "$ref" --type cyclonedx "${verification[@]}" > reports/attestation-cyclonedx.json
    cosign verify-attestation "$ref" --type spdxjson "${verification[@]}" > reports/attestation-spdx.json
    printf '%s\n' "$ref" > reports/image-digest.txt
    ;;
  instructions)
    validate_digest
    check_approval_rules
    # This mode is invoked only by the production environment-gated job.
    printf 'Approved GitOps handoff\nImage: %s\nApplication Git revision: %s\n' "$ref" "$GITHUB_SHA" |
      tee -a "${GITHUB_STEP_SUMMARY:?}"
    printf '%s\n' \
      'Open a reviewed GitOps change pinning exactly this image digest and application revision.' \
      'Ingress backend: Service port public (8080) only; operations (8081) stays internal. Apply the matching ConfigMap, Service, probes, ServiceMonitor and NetworkPolicy changes together.' \
      'Retain a passing isolated live report from docs/runbooks/acceptance.md before promotion.' \
      'Follow docs/runbooks/release.md. This workflow does not edit or sync deployment manifests.' \
      'Rollback: Git revert to the prior reviewed digest and revision.' |
      tee -a "$GITHUB_STEP_SUMMARY"
    ;;
  *) printf 'Usage: bash scripts/release.sh prepare|seal|instructions\n' >&2; exit 2 ;;
esac
