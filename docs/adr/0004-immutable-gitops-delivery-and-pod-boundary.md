# Immutable GitOps delivery with a restricted Kubernetes boundary

The portal is packaged with Kustomize and reconciled by Argo CD from immutable Git revisions and image digests, with VSO supplying its OIDC and session secrets. Its pod has read-only, least-privilege Kubernetes access and default-deny network policy because the service is intentionally an internal diagnostic catalogue, not a cluster administration plane.

## Consequences

Promotion requires human approval after SBOM, vulnerability scan, image signature, and automated tests; rollback is a Git revert. The NetworkPolicy must be verified against the deployed CNI because declaring it alone does not guarantee enforcement.
