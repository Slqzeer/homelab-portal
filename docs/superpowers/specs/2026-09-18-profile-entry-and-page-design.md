# Profile entry and page design

**Status:** Approved design

## Purpose

Give authenticated visitors a compact top-right profile entry with a minimal
preview and an authenticated, read-only `/profile` destination. Preserve the
portal's privacy boundary by retaining only the verified display name needed by
this interface.

## Goals

- Put the profile entry at the far top right.
- Show display name and signed-in state on hover and keyboard focus.
- Give account actions a dedicated profile page rather than crowding the
  catalogue.
- Keep identity storage and display intentionally minimal.

## Non-goals

- Profile editing, avatar upload, account recovery, or Keycloak administration.
- Displaying email, group membership, subject identifiers, or token/session
  details.
- Fetching remote avatars or identity information from the browser.

## Anonymous and authenticated controls

Anonymous visitors see a clearly labelled sign-in control. They do not see an
empty avatar or a link to `/profile`.

Authenticated visitors see a local circular profile icon derived from the
display-name initials. The icon is the single link to `/profile`, has an
accessible name containing the display label, and remains at least 44 by 44 CSS
pixels. It is released only when `/profile` is available; no build may ship a
link to a missing route.

## Preview behavior

Hovering the profile entry or focusing it with the keyboard reveals a
non-interactive preview containing only:

- the verified display name, or “Signed-in user” fallback;
- “Signed in.”

The preview contains no buttons or links and therefore does not trap hover or
focus. Its information is represented in the accessible name/description so it
does not require pointer hover. Touch activation follows the `/profile` link
directly. Escape or moving focus/pointer away dismisses any visually persistent
preview state.

The preview never exposes email, groups, administrator state, subject ID,
session timestamps, provider data, or tokens.

## Verified identity and session contract

The existing OIDC `profile` scope permits a `name` claim, but the current
implementation deliberately discards all profile identifiers. This design
changes that boundary only for display name:

1. Decode `name` from the already verified ID token during the callback.
2. Accept only a JSON string after trimming surrounding whitespace.
3. Reject control characters and values longer than 120 Unicode code points.
4. If absent or invalid, use no stored display name and render the neutral
   fallback; login itself does not fail.
5. Add only this normalized display name to the encrypted local session.
6. Keep authorization groups as the independent input to visibility decisions.

The session and application must continue to omit subject ID, email, raw claims,
access tokens, ID tokens, and refresh tokens. Display name is presentation data
and must never participate in authorization or logs.

## `/profile` route

`GET /profile` is a server-rendered route on the public listener. A valid portal
session renders:

- display name or neutral fallback;
- signed-in state;
- sign-out action using the existing CSRF-protected POST contract;
- an admin-diagnostics link only when existing authorization already grants it;
- navigation back to the catalogue.

An anonymous or expired session starts the normal login flow with a
server-defined return destination of exactly `/profile`; it must not accept an
arbitrary return URL from the browser. After successful login, the one-time
transaction may return to `/profile`. Authentication failures retain the
existing safe failure behavior.

The route remains read-only. Identity changes are made in Keycloak, not through
the portal.

## Security and privacy

- Escape display names in every HTML and accessibility context.
- Never use display name as a key, URL, metric label, or log field.
- Generate initials locally from the normalized value with a neutral fallback;
  do not request an avatar.
- `/profile` receives the same security headers and session refresh rules as
  other public BFF routes.
- The operations listener must return 404 for `/profile`.

## Acceptance criteria

- Anonymous users see sign-in and no profile link or profile data.
- Authenticated users see a top-right profile entry whose hover/focus preview
  contains only display name and signed-in state.
- Keyboard, pointer, and touch users can reach `/profile` without an
  interaction trap.
- Valid names survive encrypted-session rotation and idle refresh; invalid or
  absent names use the neutral fallback.
- Display name never changes catalogue authorization.
- `/profile` exposes only the approved fields/actions and preserves admin-link
  authorization.
- No email, group, subject, token, or raw claim appears in HTML, logs, metrics,
  cookies outside the encrypted payload, or client scripts.

## Test seams

- OIDC callback tests for valid, missing, malformed, control-character, and
  over-limit `name` claims.
- Session round-trip/refresh/rotation tests proving only normalized display name
  and groups survive.
- HTTP tests for anonymous/authenticated `/`, `/profile`, operations-listener
  404, safe return destination, and admin-link visibility.
- Real-browser tests for hover, focus, touch-sized activation, accessible name,
  sign-out, and absence of forbidden identity fields.

## Dependency and release boundary

Theme and compact-layout work may ship independently. The top-right profile
entry, new session field, login return behavior, and `/profile` route form one
release unit and must be implemented and tested together.
