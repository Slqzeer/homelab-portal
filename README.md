# Homelab Portal

Homelab Portal is an in-cluster, read-only service catalog for users on the local
network or Tailnet. It discovers explicitly published Tailscale Ingresses and
does not grant access to the target applications.

## Start Here

- [Documentation index](docs/README.md)
- [Canonical language](CONTEXT.md)
- [Technical architecture](docs/technical-stack.md)
- [Local development](docs/development.md)
- [Release and rollback](docs/runbooks/release.md)
- [Acceptance runbook](docs/runbooks/acceptance.md)

## Delivery status

The source implementation described by [Issue #1](https://github.com/Slqzeer/homelab-portal/issues/1)
is present in this repository: the Go BFF and embedded interface, read-only Ingress
catalog, Keycloak session boundary, hardened Kubernetes manifests, release
pipeline, and disposable-cluster acceptance harness.

Delivery evidence is intentionally split by boundary:

- `bash scripts/verify.sh source` verifies source, browser behavior, Go behavior,
  manifests, and workflow/script policy.
- The [release runbook](docs/runbooks/release.md) defines immutable image, SBOM,
  vulnerability, signature, provenance, promotion, and rollback evidence.
- The [acceptance runbook](docs/runbooks/acceptance.md) defines the live Tailnet,
  Keycloak, VSO, Argo CD, RBAC, CNI, observability, and stale-cache gate. Offline
  checks do not establish that live acceptance passed.

## Verification

```bash
bash scripts/verify.sh source
```

The Homelab repository owns the Argo CD Application and shared cluster services;
this repository owns the application, image, and application manifests.
