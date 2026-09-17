# Single-replica in-memory session and catalog model

V1 runs one portal replica with encrypted, signed cookie sessions and an in-memory Kubernetes list/watch catalog; it has no database, Redis, or PVC. Sessions last at most four hours with a 30-minute inactivity timeout and no refresh token, while the cache is marked degraded after two minutes and expired after fifteen; this minimizes operational state at the accepted cost of session loss on restart and no high availability.

## Consequences

The last valid catalog remains visible after cache expiry, but readiness fails and the UI reports that it is stale. Multi-replica availability requires a future shared-session and cache design rather than simply increasing replicas.
