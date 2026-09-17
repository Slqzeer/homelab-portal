# Read-only catalog from explicitly published Tailscale Ingresses

The portal observes only Tailscale-class Kubernetes Ingresses and exposes a card only when Git-versioned portal metadata is valid and the status has exactly one usable LoadBalancer hostname. It never writes Kubernetes or Git resources, constructs target URLs, or probes target applications; this avoids accidental disclosure, GitOps drift, and SSRF while preserving Kubernetes as the current source of service availability.

## Consequences

`available` means the catalog item is validly published with a current hostname, not that its target application is healthy. Applications remain the sole owners of target authorization and health.
