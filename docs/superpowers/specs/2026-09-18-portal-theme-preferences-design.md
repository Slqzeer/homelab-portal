# Portal theme preferences design

**Status:** Approved design

## Purpose

Give the portal complete light and dark themes. A first-time visitor follows the
operating-system preference; a visitor who selects a theme keeps that explicit
choice on later visits. Light and dark are presentation preferences only and do
not affect authentication, authorization, or catalogue contents.

## Goals

- Make light mode a fully designed first-class theme rather than an inversion.
- Retain a complete dark theme for users who prefer it.
- Apply the correct theme before the page becomes visible.
- Make theme selection keyboard-, touch-, and screen-reader-accessible.
- Keep the preference local to the browser and independent of the portal
  session.

## Non-goals

- User-account synchronization or server-side preference storage.
- Additional themes, custom colour editors, or branding controls.
- Changing catalogue visibility or OIDC/session behavior.

## Theme model

Components consume semantic CSS custom properties instead of literal palette
values. At minimum, the token set distinguishes page and elevated surfaces,
primary and muted text, borders, accents, focus rings, warnings, inputs, active
controls, and shadows. Each token has an intentional light and dark value.

The root element exposes the resolved theme as `data-theme="light"` or
`data-theme="dark"`. CSS also declares the matching `color-scheme` so native
controls use the correct palette.

## Preference resolution

The local-storage key is `portal.theme`; its only valid values are `light` and
`dark`.

1. If the key contains a valid explicit choice, apply it.
2. Otherwise, resolve from `prefers-color-scheme: dark`.
3. While no explicit choice exists, react to operating-system theme changes.
4. Activating the theme control switches to the opposite resolved theme and
   persists that choice.
5. Missing, blocked, malformed, or unavailable storage falls back to the
   current operating-system preference without preventing page rendering.

The first implementation does not add a third “system” control state. Clearing
site storage restores operating-system following.

## Loading and control behavior

A small same-origin external initializer runs in the document head before the
theme stylesheet is applied. It resolves and sets `data-theme` without requiring
inline script, preserving the existing content-security policy and preventing a
visible wrong-theme flash.

The theme control appears in the top-right action group beside the profile
control. It is a real button with an accessible name such as “Switch to dark
theme.” Its icon is accompanied by an accessible label, and its label updates
after activation. Theme changes do not navigate, reload, or request network
data.

## Accessibility and motion

- Text, controls, links, warnings, and focus indicators meet WCAG 2.2 AA
  contrast in both themes.
- Theme is never communicated by colour alone.
- Focus remains clearly visible on every surface.
- Theme transitions are disabled under `prefers-reduced-motion: reduce` and are
  subtle otherwise.
- The control remains at least 44 by 44 CSS pixels.

## Security and privacy

Theme code reads and writes only `portal.theme`. It does not inspect cookies,
identity, catalogue data, or URLs. No preference is sent to the server. The
initializer and icons are same-origin embedded assets; no third-party resource
is introduced.

## Acceptance criteria

- With no saved value, light/dark follows the emulated OS preference.
- A manual selection survives reload and overrides a later OS-theme change.
- Invalid or unavailable local storage falls back without an uncaught error.
- The resolved theme is set before the first meaningful paint.
- The toggle is operable and understandable with keyboard and assistive
  technology.
- Automated contrast checks pass for representative catalogue, stale, empty,
  and profile-preview states in both themes.
- No request, cookie, or session write occurs when changing theme.

## Test seams

- Real-browser tests at `/` for OS resolution, persistence, toggle semantics,
  storage failure, and reduced motion.
- Rendered-HTML contract verifying the early same-origin initializer and theme
  control are present without inline script.
- Visual/accessibility checks for representative states in both themes.
