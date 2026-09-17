# Homelab Portal

The Homelab Portal is an in-cluster, read-only service catalogue for people connected to the local network or Tailnet. It discovers intended services from Kubernetes Ingress metadata and does not grant access to those services.

## Language

**Portal**:
The in-cluster application that presents service links and read-only diagnostics to authorized network users.
_Avoid_: dashboard, Kubernetes console

**Catalog item**:
A user-visible link derived from an eligible Kubernetes Ingress and its portal publication metadata.
_Avoid_: application, workload

**Target application**:
The service reached by a catalog item; it independently enforces its own authorization policy.
_Avoid_: protected portal service

**Eligible Ingress**:
A Tailscale-class Kubernetes Ingress with valid portal metadata and exactly one usable LoadBalancer hostname; it can supply a catalog item.
_Avoid_: discovered service, exposed workload

**Portal session**:
A server-controlled, time-bounded authenticated state used to determine catalog visibility; it never gives the browser an OAuth token.
_Avoid_: browser token, permanent login

**Publication metadata**:
The version-controlled portal annotations that explicitly allow an Eligible Ingress to become a catalog item.
_Avoid_: automatic discovery rule, inferred publication

**Admin diagnostics**:
The restricted portal view that explains catalog validation and watch failures using allowlisted resource metadata and remediation advice.
_Avoid_: cluster console, debug dump

**Available catalog item**:
A catalog item derived from an Eligible Ingress whose current status contains its validated Tailscale hostname. It does not assert that the target application responds to HTTP requests.
_Avoid_: healthy application, probed endpoint
