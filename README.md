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

## Verification

```bash
bash scripts/verify.sh source
```

The Homelab repository owns the Argo CD Application and shared cluster services;
this repository owns the application, image, and application manifests.
