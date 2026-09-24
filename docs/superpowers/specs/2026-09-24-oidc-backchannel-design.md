# OIDC internal backchannel — design specification

## 1. Purpose

The portal must authenticate against the Keycloak realm whose canonical issuer
is `https://keycloak.taildf6cd4.ts.net/realms/homelab`. Browser redirects and
ID-token `iss` validation must continue to use that exact HTTPS identity.

Cluster pods cannot resolve the Tailscale MagicDNS hostname. Keycloak is
reachable inside the cluster at
`http://keycloak.keycloak.svc.cluster.local:8080`, where TLS has already been
terminated by the browser-facing Tailscale proxy. The portal therefore needs a
separate, narrowly scoped server-side route without changing the issuer seen by
users or tokens.

Success means that the portal can discover Keycloak, exchange an authorization
code, refresh signing keys, and validate an ID token from inside the cluster,
while the browser still visits the public HTTPS authorization endpoint and no
general HTTP or Internet egress is introduced.

## 2. Scope

Included:

- a non-secret `OIDC_BACKCHANNEL_URL` process setting;
- strict routing of OIDC server-to-server requests through that URL;
- preservation of the canonical HTTPS issuer and authorization URL;
- fail-closed URL, discovery, endpoint, redirect, and issuer checks;
- the exact Keycloak pod selector and TCP 8080 NetworkPolicy egress;
- unit, contract, manifest, and integration coverage;
- a new reviewed immutable portal release and GitOps pin.

Excluded:

- adding TLS to the in-cluster Keycloak Service;
- changing Keycloak's issuer, hostname, client, realm, or certificates;
- changing CoreDNS, Tailscale MagicDNS, or proxy topology;
- accepting arbitrary discovery documents or endpoint hosts;
- weakening signature, audience, nonce, state, or PKCE validation;
- adding a general-purpose proxy, custom DNS resolver, or broad HTTP egress.

## 3. Configuration contract

`OIDC_ISSUER_URL` remains required and must be an absolute HTTPS URL without
credentials, query, or fragment. It is the sole identity used for discovery
issuer comparison and ID-token issuer validation.

`OIDC_BACKCHANNEL_URL` is required for the homelab deployment. It may use HTTP
or HTTPS, must be an absolute URL without credentials, query, or fragment, and
must include the same realm path as the issuer. The production value is:

```text
http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab
```

The application treats both values as normalized URL prefixes with no trailing
slash. It permits equal origins so local and test deployments may use one
provider. The homelab manifest contract requires distinct public and internal
origins and the exact production values above.

No credential, token, session key, or certificate material is added to this
setting. Existing file-backed secret boundaries remain unchanged.

## 4. Architecture and request flow

The portal retains the pinned `coreos/go-oidc` provider and its normal issuer
validation. A dedicated HTTP client uses a small backchannel transport:

1. The OIDC library constructs requests below the canonical issuer prefix.
2. The transport rejects any request whose scheme, host, or escaped path falls
   outside that exact prefix.
3. It clones the request and replaces only the approved issuer prefix with the
   configured backchannel prefix, preserving the remaining path and query.
4. It sends the cloned request with the internal Service authority. It never
   mutates the caller's request and never rewrites browser-facing URLs.
5. Redirect following is disabled. A redirect, off-prefix endpoint, malformed
   response, unavailable Service, or issuer mismatch fails initialization or
   login without exposing upstream details to the browser.

Provider discovery is invoked with the canonical issuer and the client-bearing
context. The discovery fetch is therefore transported internally, while the
document's `issuer` must still exactly equal `OIDC_ISSUER_URL` through the
library's standard check. The discovered authorization, token, JWKS, and
userinfo endpoints must all be absolute URLs under the same canonical issuer
prefix; the portal validates these claims before accepting the provider.

The OAuth configuration keeps the discovered public HTTPS authorization URL.
Consequently `LoginURL` redirects the browser to Tailscale HTTPS. Callback code
exchange uses the same dedicated client in its context, so the public token URL
is transported internally. The provider verifier uses that client for JWKS
refreshes and continues to validate signatures, the exact canonical issuer,
the portal client audience, nonce, state, and PKCE as before. Any future
userinfo request must use this same client and endpoint validation.

The client has a finite request timeout and a cloned default transport. It does
not log request bodies, authorization headers, codes, tokens, or discovery
responses.

## 5. Deployment and network boundary

The base ConfigMap declares `OIDC_BACKCHANNEL_URL` with a non-deployable
example value. The homelab overlay patches both URLs to the observed values:

- issuer: `https://keycloak.taildf6cd4.ts.net/realms/homelab`;
- backchannel: `http://keycloak.keycloak.svc.cluster.local:8080/realms/homelab`.

The portal NetworkPolicy allows this traffic only to namespace `keycloak`, pods
with the observed stable label `app: keycloak`, and TCP port 8080. The obsolete
example selector and port 8443 are removed. DNS access remains limited to
CoreDNS. No IP block, whole-namespace peer, port 80 wildcard, or general
Internet egress is added.

Task 4's GitOps Application uses a reviewed immutable portal commit containing
the new release pin. Its VSO secret schema is unchanged.

## 6. Failure behavior

- Invalid issuer or backchannel configuration prevents startup.
- Discovery failure, redirect, issuer mismatch, or an endpoint that is missing,
  non-absolute, malformed, or outside the canonical issuer prefix prevents OIDC
  initialization and readiness.
- Token/JWKS backchannel failure makes new login fail with the existing generic
  `portal login failed` response; upstream details and tokens remain private.
- Existing local sessions retain their current bounded behavior and never gain
  extra authorization during a Keycloak outage.
- The public catalogue behavior is unchanged; restricted entries remain hidden
  without a valid session.

## 7. Verification strategy

Test-driven implementation adds:

- configuration tests for required, normalized, malformed, credential-bearing,
  queried, fragmented, and HTTP/HTTPS backchannel URLs;
- transport tests proving exact-prefix rewriting, request immutability,
  internal authority use, query preservation, redirect rejection, and denial
  of sibling paths, alternate hosts, schemes, and prefix-confusion strings;
- OIDC tests with distinct public and internal test servers proving discovery
  issuer equality, public browser authorization, internal code exchange and
  JWKS refresh, plus issuer/off-origin/redirect failures;
- existing state, nonce, PKCE, audience, signature, and generic-error tests;
- manifest tests for the exact ConfigMap values and Keycloak `app: keycloak`
  TCP 8080 NetworkPolicy peer, with negative checks against broad egress;
- the complete Go, race, vet, web, manifest, workflow, container, browser, and
  release gates already required by the portal repository.

## 8. Release and migration

The implementation is reviewed before creating a new immutable patch tag.
The protected workflow must pass and emit a signed, scanned, two-platform image
with BuildKit provenance and SBOM attestations. The evidence is reviewed against
the tagged source and exact digest. A manifest-only follow-up pins the unique
workflow tag plus digest, and Task 4 consumes that follow-up commit.
