# Catalogue search and information hierarchy design

**Status:** Approved design

## Purpose

Make catalogue discovery the primary interaction: search belongs at the top,
category filters immediately below it, and descriptive/status information in
the left rail. Filtering remains a local operation over already authorized
server-rendered cards.

## Goals

- Make search immediately visible and usable.
- Keep category selection close to search without consuming excessive space.
- Move non-navigation information out of the result grid.
- Preserve the existing no-fetch, deny-by-default authorization model.

## Non-goals

- Server-side search, autocomplete, ranking, or fuzzy matching.
- Access-level filters or indications that hidden catalogue items exist.
- Persisting search terms or categories across visits.

## Top interaction area

The main region begins with a compact search row. The labelled search input
occupies the flexible width. Theme and profile controls sit at the far right of
the same top interaction area. On small screens the controls remain visible and
the search input receives its own full-width row if required.

A clear-search button appears only when text is present. It clears the text,
retains the selected category, returns focus to the input, and updates results
immediately.

Category filter chips appear directly below the search row. “All” is selected
initially. Chips wrap or use an accessible horizontal overflow treatment on
narrow screens; they never shrink labels into unreadable text.

For catalogues taller than the viewport, the interaction area remains sticky at
viewport widths where it does not cover the results. It returns to normal flow
on smaller screens. Its opaque themed surface and boundary must keep underlying
cards from reducing legibility, and focused elements must never be hidden
beneath it.

## Filtering behavior

Text matching remains case-insensitive across the visible name, description,
and category text. Text and category filters combine with AND semantics.
Clearing one filter retains the other. Selecting “All” clears only the category
constraint.

Filtering reads only `article[data-catalog-item]` elements already emitted by
the BFF. It does not call an API, inspect hidden metadata, or receive a serialized
catalogue. Removing a card from authorized HTML makes it unavailable to search
by construction.

## Information rail and result feedback

The left rail from the compact-layout specification owns:

- the total currently visible result count;
- the stale-catalogue notice;
- portal and catalogue explanatory copy.

The count is an atomic polite live region and updates after every filter
change. Stale state describes catalogue freshness only, never target health.

When no visible card matches, the main result region shows a concise empty
result with actions to clear search or return to “All.” It must not reveal or
imply the names or number of unauthorized items.

## Keyboard and assistive behavior

- Search has a persistent visible label; placeholder text is supplementary.
- Category chips are buttons with accurate `aria-pressed` state.
- Filtering does not move focus or automatically navigate.
- The clear control has a specific accessible name.
- Result-count announcements are concise and do not fire on unrelated page
  changes.
- Sticky positioning does not interfere with skip links or focused controls.

## Acceptance criteria

- Search is the first principal control in the main catalogue area.
- Category filters render immediately beneath it.
- Name, description, and category matching remains case-insensitive.
- Search and category filters combine and clear independently.
- Visible count and stale information appear in the left rail and are not
  duplicated above cards.
- Empty results provide safe recovery controls and disclose no hidden item.
- Browser observation records no catalogue-data network request during any
  filtering interaction.

## Test seams

- Real-browser behavior against production BFF-rendered authorized cards.
- Anonymous/member/admin fixtures proving filtering cannot reveal withheld
  names, URLs, categories, or counts.
- Keyboard and screen-reader semantics for search, clear, chips, count, and
  empty recovery.
- Narrow and wide viewport checks for sticky layout and category overflow.
