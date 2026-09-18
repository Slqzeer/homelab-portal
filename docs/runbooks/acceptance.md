# Cluster acceptance

This gate verifies an explicitly provisioned **disposable cluster and Tailnet test deployment**. It does not promote a release, edit existing Ingresses, create resources through the portal, or operate production identities. A successful local compile is not live acceptance. Task 10 must retain a successful live report alongside the reviewed release digest and GitOps revision before promotion.

## Prerequisites

1. Provision an isolated cluster with the target Kubernetes/CNI, Tailscale operator, Keycloak test realm/client, VSO, Argo CD, and network policies. Use the deployment candidate's immutable registry digest and the reviewed immutable GitOps commit. Verify release signature/SBOM/scan evidence using [release.md](release.md). The operator must admit the exact names and proxy resources required by the central Ingress configuration and cap each proxy container at **128Mi or less**.
2. Deploy one healthy portal and a separate stale fixture of the same image. Their existing centralized Tailscale Ingresses must have exactly one `.ts.net` LoadBalancer hostname. Configure external `/healthz`, `/readyz`, and `/metrics` to return **404**, including to admin users. Internal paths remain reachable on pod TCP 8080. Route exclusions require the GitOps routing layer; the application serves these paths internally and the harness will fail if an unfiltered `/` route publishes them.

   Every Ingress backend must resolve through its exact ClusterIP Service and Service-owned EndpointSlices to only the respective digest-checked portal Pod UID, with matching Pod addresses and target TCP 8080. The harness rechecks this binding before acceptance cases. Missing targetRefs, selectorless Services, replaced UIDs/images, extra unverified endpoints, mismatched ports, and routing intermediaries that cannot be verified by this direct topology fail closed. Path exclusions must therefore live at the Ingress routing boundary for this supported topology; a reverse-proxy intermediary requires a separately reviewed verifiable route contract before it can be accepted.
3. Use a healthy confidential Keycloak client with exact callback, PKCE, `openid profile groups`, and the JSON groups mapper. Create three disposable identities: member with exactly `ACCEPT_GROUP`, nonmember without that group or `portal-admin`, and admin with `portal-admin` but without either test group. None has the uppercased test group. Run a real browser/CI OIDC login against the **healthy** portal for each and inject its `__Host-portal_session` value into CI secret environment variables. Do not supply the cookie name, attributes, password, bearer token, or an invented session. Sessions must remain valid for the run; do not restart the portal between login and acceptance. Credential acquisition is an external prerequisite, not an offline success claim.
4. VSO must reconcile `VaultStaticSecret` at its current generation with `SecretSynced=True` and own its destination Secret. A deployed VSO version without these conditions fails this gate and requires an explicitly reviewed compatibility update. The suite sends only `Accept: application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1` for the destination Secret, with no full-object fallback. It rejects HTTP errors or a different response media type **before reading the body**, then accepts only a correctly identified PartialObjectMetadata object. Metadata negotiation failure blocks acceptance. Argo Application must be labeled for this test run, target the portal namespace, and report `Synced`, `Healthy`, and the expected full revision. Keep the deliberately unready stale fixture outside that healthy Application.
5. Provision dedicated fixture, portal, and allowed-probe namespaces with **both** `acceptance.homelab.io/disposable: "true"` and `acceptance.homelab.io/run: <unique run ID>`. Apply those labels to both portal Pods and Ingresses, the fixture backend Service/Pod, the Argo Application, and the VaultStaticSecret. Use the template in `tests/e2e/fixtures.yaml` for the fixture namespace/backend, replacing `RUN_ID`, `FIXTURE_NAMESPACE`, and `BACKEND_IMAGE` with a reviewed BusyBox digest. The backend Pod also needs `acceptance.homelab.io/backend: <run ID>`; its Service must resolve to that single run-owned Pod through Service-owned EndpointSlices. It must return HTTP 200 at `/` and expose Service TCP 80. Bootstrap resources remain owned by the provisioning workflow; this harness never deletes them.
6. The allowed-probe namespace must match the deployed scraper NetworkPolicy namespace selector (the base uses `monitoring`). The suite's allowed Pod uses `app.kubernetes.io/name=prometheus` and `operator.prometheus.io/name=homelab`; these must match the reviewed policy. The fixture namespace must differ from portal and allowed-probe namespaces and must have no allow rule to portal TCP 8080. Both probe source namespaces need working egress to their intended positive controls. `ACCEPT_PROBE_IMAGE` must be a reviewed immutable image containing `curl` and `sleep`, executable as UID 65532. All namespaces need the standard `kube-root-ca.crt` ConfigMap.
7. The runner needs Go from `go.mod`, PowerShell 5.1+ or 7+, compatible `kubectl`, verified cluster TLS, and Tailnet HTTPS connectivity. Supply a dedicated CI kubeconfig from the secret store, with the named context and direct short-lived credentials; executable credential plugins, auth-provider plugins, and insecure TLS are rejected. Grant the runner read access to prerequisite Pods, Namespaces, Services, EndpointSlices, Ingresses, Argo/VSO status, and destination Secret metadata; scoped create/get/delete for fixture Ingresses, probe Pods, and the two run-owned NetworkPolicies in the fixture namespace; `pods/exec` for its probes; and impersonation of the test portal ServiceAccount **and its ServiceAccount/authenticated groups** for authorization reviews. Kubernetes RBAC does not distinguish Secret metadata reads from Secret reads: restrict the runner's Secret `get` grant to the exact VSO destination resourceName and rely on the strict negotiated reader to avoid requesting data. The portal itself retains its read-only Ingress permissions. Do not grant or use a production identity.

## Stale fixture preparation

Create a separate, run-labeled portal fixture with the same image, config contract, and ServiceAccount in the isolated portal namespace. Publish an independently known public sentinel and allow an initial successful list; observe its exact name/link through the fixture's Tailnet URL. Then the bootstrap/fault workflow blocks **only this fixture's** Kubernetes API connectivity while allowing Tailscale and scraper ingress. Wait over 15 minutes after its last successful list/watch revalidation. The page must retain the sentinel and show `stale-banner`; internal `/healthz` remains 200 and `/readyz` becomes 503. Keep the API block in place for acceptance.

Use a separately selected fixture Service with `publishNotReadyAddresses: true` (or equivalent isolated routing) so the stale page is still externally observable after readiness fails. Do not disable readiness or alter the healthy portal's published Ingress. A NetworkPolicy deny alone cannot override existing allows: prepare the fixture with distinct policy selectors and a removable fixture-only API allow rule in the bootstrap workflow. Existing broad `app.kubernetes.io/name=homelab-portal` egress allows must not also select that stale fixture. Fault injection, the >15-minute aging wait, and later fault-fixture teardown belong to the disposable provisioning workflow; this suite only observes the resulting public behavior and readiness. Startup-with-no-cache does not satisfy the sentinel assertion.

## Required environment

Every value is mandatory. Values below are names/contracts, not real credentials. Inject secrets from the CI secret store with tracing/transcripts disabled; never echo the environment or paste secret values into commands or URLs.

| Variables | Required value |
| --- | --- |
| `ACCEPT_DISPOSABLE`, `ACCEPT_TEST_IDENTITIES` | Exact attestations `isolated-test-cluster` and `disposable-only` |
| `ACCEPT_RUN_ID` | New `accept-` ID with 8–35 lower-case alphanumeric/hyphen suffix characters; same as prerequisite labels; never reuse |
| `KUBECONFIG`, `ACCEPT_CONTEXT`, `ACCEPT_CLUSTER_UID` | CI kubeconfig file, explicit context, and independently recorded `kube-system` namespace UID for the disposable cluster |
| `ACCEPT_PORTAL_URL`, `ACCEPT_STALE_URL` | Distinct exact HTTPS `.ts.net` origins; no path/trailing slash, port, userinfo, query, or fragment; match respective Ingress status |
| `ACCEPT_PORTAL_NAMESPACE`, `ACCEPT_FIXTURE_NAMESPACE`, `ACCEPT_PROBE_NAMESPACE` | Run-labeled active namespaces described above |
| `ACCEPT_PORTAL_INGRESS`, `ACCEPT_STALE_INGRESS` | Distinct existing isolated Ingress names in portal namespace |
| `ACCEPT_PORTAL_POD`, `ACCEPT_STALE_POD` | Distinct exact running Pod names, run-labeled; each has a `portal` container using `ACCEPT_IMAGE` |
| `ACCEPT_SERVICE_ACCOUNT` | Both portal Pods' read-only ServiceAccount |
| `ACCEPT_FIXTURE_SERVICE` | Run-labeled ClusterIP backend Service with a single TCP 80 port, normally `acceptance-backend` |
| `ACCEPT_ARGO_NAMESPACE`, `ACCEPT_ARGO_APPLICATION`, `ACCEPT_ARGO_REVISION` | Exact Application identity and expected 40-character lower-case Git SHA |
| `ACCEPT_VSO_NAME`, `ACCEPT_PROXY_NAMESPACE` | VaultStaticSecret in portal namespace and operator proxy namespace, normally `tailscale` |
| `ACCEPT_IMAGE`, `ACCEPT_PROBE_IMAGE` | Reviewed full image references ending `@sha256:<64 lower-case hex>` |
| `ACCEPT_GROUP` | Lower-case exact disposable member group, 3–51 characters; no user belongs to its upper-case counterpart |
| `ACCEPT_STALE_ITEM_NAME`, `ACCEPT_STALE_ITEM_URL` | Known sentinel name and exact `.ts.net` HTTPS origin retained in the preconditioned stale fixture |
| `ACCEPT_MEMBER_COOKIE`, `ACCEPT_NONMEMBER_COOKIE`, `ACCEPT_ADMIN_COOKIE` | **Secret** distinct real portal session cookie values from the CI store/environment; never retained in artifacts |

The isolation attestations are intentional operator inputs, not proof by themselves: the harness also checks explicit cluster UID, namespace labels, resource run labels, Ingress hostnames, Pod identity, image digest, backend Service, and named Argo/VSO resources before creating anything. It never infers a context from the workstation's default kubeconfig.

## Commands and observations

Offline checks (no cluster required):

```powershell
go test ./tests/e2e -count=1
go test -tags=e2e ./tests/e2e -list TestPortalAcceptance
go vet -tags=e2e ./tests/e2e
go test ./...
$parseTokens = $null
$parseErrors = $null
[System.Management.Automation.Language.Parser]::ParseFile((Join-Path (Get-Location) 'scripts/verify-cluster.ps1'), [ref]$parseTokens, [ref]$parseErrors) | Out-Null
if ($parseErrors.Count -ne 0) { throw 'PowerShell parse failed' }
```

After isolated provisioning and CI environment/secret injection, run in a separate process:

```powershell
powershell -NoProfile -NonInteractive -File scripts/verify-cluster.ps1 -ReportPath reports/acceptance.json
```

Use `pwsh` instead of `powershell` on runners with PowerShell 7. Exit 0 plus report `passed=true` and `liveAttempted=true` is required. The launcher suppresses raw process output and starts with a failed report, so startup/build failure cannot reuse an old passing report. Archive `reports/acceptance.json` with the CI equivalent of `if: always()`. The JSON contains only schema/time, validated non-secret run/image/revision identifiers, fixed case names, and booleans. Never archive kubeconfig, traces, cookies, HTTP pages, redirect URLs, or command output. Reports are overwritten at the specified path; CI should use a unique artifact/run directory.

The matrix requires anonymous public-only links; authenticated links for both signed-in non-admins; group links only for the exact member; upper-case-group links hidden from everyone; admin links/diagnostics only for the admin; and invalid publication visible only as an admin diagnostic. Each visible name must be paired with `https://` plus its current Ingress status hostname. Both unauthorized names and URLs must be absent from returned HTML. It also checks the stale sentinel/banner, healthy 200/stale 503 readiness, internal metrics, operational-route 404s, VSO sync, Argo revision/health, and proxy caps.

`kubectl auth can-i` impersonates the test portal ServiceAccount plus its actual Kubernetes groups. `get/list/watch ingresses.networking.k8s.io --all-namespaces` must answer `yes`; Secret reads and Ingress writes must answer `no`, also in every existing namespace to catch local RoleBinding grants. Errors are never interpreted as a valid denial unless stdout is exactly `no`.

CNI checks create disposable curl Pods and two direction-control NetworkPolicies. The scraper-labeled allowed peer must reach healthy readiness/metrics and stale liveness/readiness. A policy selecting only the unauthorized run-labeled peer explicitly permits **all its egress**, so its three failed portal connections cannot be attributed to source egress policy. A second policy selects only the run-owned fixture backend and explicitly permits ingress from the denied peer and the exact labels/namespace of the egress probe, so the egress probe's three failed backend connections cannot be attributed to backend ingress policy. Neither policy changes portal ingress or egress. The known backend is resolved to its unique verified Pod/port and probed directly, avoiding Service DNAT ambiguity. Positive controls run before and after the denials. These tests assume standard additive Kubernetes NetworkPolicy semantics; clusterwide/CNI-specific deny policies must be absent from this disposable topology or independently excluded by the provisioning workflow.

The egress probe copies **exactly** the healthy portal's policy labels, carries a controller owner reference preventing ReplicaSet adoption, and remains unready as defense in depth. Readiness alone is insufficient: before creation, the harness enumerates every Service in its namespace whose selector matches the probe and rejects **every numeric/default targetPort** and every named targetPort declared by the probe, regardless of `publishNotReadyAddresses`. Only unresolved named ports are safe. Thus matching portal Services must use named targetPorts (the base uses `http`), and the curl probe declares no ports. The admitted Pod is checked again after creation. Freeze Service/route configuration for the duration of acceptance. The probe must resolve and reach the Kubernetes API using its CA (HTTP 200/401/403 with no token), then fail connections to the explicitly ingress-permitted backend. Real Keycloak connectivity is required by the external OIDC login prerequisite.

## Failure and cleanup

Any missing/invalid input or prerequisite stops the run. Existing Ingresses are read-only; publication fixtures use Kubernetes **Create**, never apply/update/adopt, with names `<run>-public`, `-authenticated`, `-groups`, `-case-mismatch`, `-admin`, and `-invalid`. Probes are `<run>-allowed`, `-denied`, and `-egress`; direction-control policies in the fixture namespace are `<run>-source-egress-control` and `<run>-backend-ingress-control`. All receive run/disposable labels and enter exact UID-precondition cleanup immediately after successful creation. A collision fails without touching the existing object. Readiness/hostname convergence has bounded waits. HTTP bodies, tool stderr, redirect locations, and credentials never appear in assertions or the report.

Normal failure runs cleanup in reverse creation order. Cleanup re-reads each exact created object, verifies its run/disposable labels and original UID, and supplies both UID and current resourceVersion delete preconditions. Any mismatch or API error refuses deletion and marks acceptance failed. It never deletes a namespace, uses a label-selector delete, or removes bootstrap resources. Operator-generated proxy children are left to the Ingress owner's normal garbage collection/finalizers.

The CI process must allow the launcher's 18-minute deadline and a cleanup grace period. Hard termination, a server-side successful create followed by client timeout, or unavailable API can leave fixtures. Retain the failed report and have the bootstrap workflow inspect **the exact names above** in their explicit namespaces, verify the run/disposable labels and UID against that run's API audit/create record, then delete each individually with UID preconditions through the Kubernetes API. Do not issue a broad selector delete or delete the namespace. If ownership cannot be established, leave the object and investigate. Provisioning owns the stale fault fixture and bootstrap backend lifecycle.

Successful acceptance does not replace admission/server dry-run, image provenance, human promotion approval, or live alert-rule evaluation. Those remain explicit release/GitOps gates. No local test result establishes real cluster, Tailnet, Keycloak, VSO, Argo, or CNI health.
