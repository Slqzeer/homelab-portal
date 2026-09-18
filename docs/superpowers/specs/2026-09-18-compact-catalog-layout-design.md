# Compact catalogue layout design

**Status:** Approved design

## Purpose

Replace the narrow, vertically spacious catalogue with a denser application
shell that uses the viewport effectively while preserving readability,
responsive behavior, and the portal's server-rendered authorization boundary.

## Goals

- Use desktop width efficiently and display more catalogue items at once.
- Separate descriptive portal information from actionable service links.
- Preserve a clear reading and keyboard order at every viewport size.
- Keep long valid catalogue metadata usable without horizontal overflow.

## Non-goals

- Changing catalogue metadata, sorting, visibility, or target navigation.
- Adding dashboards, target health, favorites, or user-customizable layouts.
- Moving admin diagnostics into the catalogue page.

## Desktop shell

The page uses the available viewport with a bounded comfortable maximum rather
than the current narrow centered column. It consists of:

1. a compact top interaction area owned by the search/information-hierarchy
   specification;
2. a fixed-width left information rail, approximately 15–18rem;
3. a flexible main catalogue region occupying the remaining width.

The left rail contains the current non-destination information:

- “Tailnet catalogue”;
- “Homelab Portal”;
- “Available to you” and the catalogue explanation;
- current visible-item count;
- stale-catalogue information when applicable.

It contains no target-application links. The main region contains search,
category filtering, empty results, and catalogue cards.

## Catalogue grid and cards

The card grid uses responsive tracks such as `repeat(auto-fit,
minmax(...))`, with a practical minimum card width around 16–18rem. It expands
from two to four columns as space permits without leaving a large unused side
area.

Cards use compact spacing and no arbitrary fixed height. The service name is
the primary target link. Icon, category, and description are supporting
content and do not create competing destinations. Long valid names,
descriptions, and categories wrap within the card. Dense presentation must not
reduce interactive targets below 44 by 44 CSS pixels.

## Responsive behavior

Below the width needed for a useful rail and catalogue, the information rail
moves above search and results. The source and focus order remains:

1. skip link;
2. portal information, catalogue count, and stale state;
3. search and categories;
4. empty-result feedback when applicable;
5. catalogue cards.

Cards reduce to one column when their minimum readable width cannot be
maintained. The page must not scroll horizontally at 320 CSS pixels or wider.
Theme and profile actions remain available in a compact top-right group without
covering the search input.

## Spacing and hierarchy

Use a small documented spacing scale. Reduce current header padding, large
section gaps, and repeated heading space. Retain enough separation to identify
the rail, controls, and results without wrapping every section in a card.
Headings use a restrained scale; service names remain more visually prominent
than categories and descriptions.

## Accessibility

- Preserve semantic header, main, navigation, section, heading, and article
  structure.
- The skip link lands on the principal catalogue content.
- DOM order matches visual order; CSS reordering must not create a different
  keyboard sequence.
- Zoom to 200% remains usable without clipped controls or text.
- Long content and localized text do not overlap icons or actions.

## Acceptance criteria

- Common desktop widths show multiple useful card columns with no large unused
  outer region.
- The named informational content appears in the left rail and not duplicated
  above the cards.
- Valid maximum-length catalogue fields wrap without overflow.
- At 320px width the rail stacks above the controls, cards use one column, and
  there is no horizontal page scrolling.
- All interactive controls retain at least a 44px target.
- Anonymous, authenticated, stale, empty, and admin-linked states retain a
  stable layout.

## Test seams

- Production-rendered browser tests at narrow, tablet, and wide desktop
  viewports.
- Overflow tests using maximum valid name, description, and category lengths.
- Accessibility checks for landmarks, headings, focus order, zoom, and target
  size.
- Assertions use visible behavior and geometry, not CSS implementation details.
