# Cluster acceptance

`portal_e2e_test.go` is deliberately behind `//go:build e2e`. Ordinary `go test ./...` runs only offline harness configuration, HTTP, and launcher contracts. Enabling the tag and invoking `TestPortalAcceptance` without its complete environment fails; it never silently skips acceptance.

Use [the acceptance runbook](../../docs/runbooks/acceptance.md) for the complete input contract, isolated fixture setup, credentials, commands, expected observations, and cleanup. `fixtures.yaml` is a non-secret bootstrap template for the disposable backend; the suite does not apply it. The suite creates six uniquely named Tailscale publication Ingresses, three short-lived curl probe Pods, and two direction-control NetworkPolicies. Every created object receives the run and disposable labels. Only those successfully created objects enter cleanup, which verifies labels and UID and deletes with UID/resourceVersion preconditions.

The public seam is real HTTP through the supplied Tailscale Ingress, whose Service/EndpointSlice route must bind to the exact digest-checked Pod UID. CI supplies session cookie **values** obtained through real Keycloak Authorization Code + PKCE login for disposable users; the suite neither fabricates sessions nor decodes their contents. Kubernetes checks use explicit kubeconfig/context and inspect public resource metadata/status. Secret metadata reads send one PartialObjectMetadata-only `Accept` value with no unrestricted fallback; the API server's normal plain `application/json` response is bounded and accepted only when it has the exact PartialObjectMetadata top-level shape and identity. This relies on the trusted API server honoring metadata negotiation; the harness never requests, retains, logs, or reports Secret data. `kubectl auth can-i --as` tests actual ServiceAccount authorization, including namespace-specific negative grants. Two additional run-owned NetworkPolicies permit only the opposite directions of the CNI denial tests, eliminating source-egress/backend-ingress false positives; these policies receive the same UID-checked cleanup as the Pods and Ingresses.

Offline commands:

```powershell
go test ./tests/e2e -count=1
go test -tags=e2e ./tests/e2e -list TestPortalAcceptance
go vet -tags=e2e ./tests/e2e
```

The compile/list command does not execute live tests. A missing-environment run is expected to exit nonzero and retain `reports/acceptance.json` with `passed=false` and `liveAttempted=false`. Use the PowerShell launcher for CI, which also leaves a failed report if compilation or startup fails. Upload that JSON with an always-run artifact step. Never upload kubeconfig, environment dumps, cookies, raw HTTP bodies, or raw tool output.
