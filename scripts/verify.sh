#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p reports

case "${1:-}" in
  source)
    # Chrome matches the channel in web/tests/playwright.config.mjs.
    (
      cd web
      npm ci
      npx --no-install playwright install --with-deps chrome
      npm run build
      npm run lint
      npm test
    ) 2>&1 | tee reports/web.log
    go mod verify 2>&1 | tee reports/go-mod.log
    go test ./... -count=1 -json > reports/go-test.json
    go test -race ./... -count=1 -json > reports/go-race.json
    go vet ./... 2>&1 | tee reports/go-vet.log
    # Strict validation includes all CRDs; missing schemas are errors.
    kustomize build deploy/overlays/homelab > reports/manifests.yaml
    test -s reports/manifests.yaml
    kubeconform -strict -summary -kubernetes-version 1.36.4 \
      -schema-location 'https://raw.githubusercontent.com/yannh/kubernetes-json-schema/c9452fcf5ef03628ab8b07e5b3a6b6f989e543bf/{{.NormalizedKubernetesVersion}}-standalone{{.StrictSuffix}}/{{.ResourceKind}}{{.KindSuffix}}.json' \
      -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/ad3b08c5045129d7bb1eeffd8e61719b2c8dd1e2/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
      reports/manifests.yaml 2>&1 | tee reports/schemas.log
    actionlint 2>&1 | tee reports/actionlint.log
    shellcheck scripts/*.sh 2>&1 | tee reports/shellcheck.log
    git diff --exit-code HEAD -- go.mod go.sum web/package.json web/package-lock.json
    git rev-parse HEAD > reports/source-revision.txt
    git archive --format=tar --output=reports/tested-source.tar HEAD
    ;;
  image)
    trivy image --image-src docker --scanners vuln --severity HIGH,CRITICAL \
      --ignorefile /dev/null --exit-code 1 --format json --output reports/trivy-ci.json portal:ci
    ;;
  *) printf 'Usage: bash scripts/verify.sh source|image\n' >&2; exit 2 ;;
esac
