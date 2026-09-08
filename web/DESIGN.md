# presso UI design notes

The web UI is a control plane for WordPress sites on one host. Its job is to
show at a glance what is up and healthy and to let an operator act on it.
The direction is called "Atelier": light and editorial, with the site
previews as the hero on soft white plates, a serif headline voice, and a
sidebar that has room to grow.

## Shell

- A 260px sidebar carries navigation only: Sites, Jobs, then a Platform
  section (Edge, Database, Cache, System). Edge, Database and Cache link to
  anchors on the System page until they earn pages of their own. The
  bottom holds the platform health chip and the connection state.
- A 64px topbar carries the breadcrumb, the global search (⌘K opens the
  command palette), the Live indicator, running jobs and the account menu.
- Pages live in a 1400px column with 48px side padding and 36px above.

## Tokens

All colors, radii and shadows are semantic tokens declared in
`src/routes/layout.css` (Tailwind `@theme`) and redefined for dark mode. Use
the token classes (`bg-surface`, `text-ink-2`, `border-rule`, ...), never raw
palette values.

- Surfaces: `ground` (page), `surface` (plates, sidebar, topbar), `sunken`
  (inset areas), `rule` and `rule-strong` (hairlines and control borders).
- Text: `ink`, `ink-2` (secondary), `ink-3` (muted).
- Meaning: `brand` (forest green, primary actions and the "running" state),
  `ok`, `busy` (brass, anything in motion), `danger` (failure only), `info`.
  Each has a `-soft` tint and an `-ink` text color that reads on that tint.
  `brand-deep` stays dark in both schemes for large fills.
- Console panes are always dark (`console` utility) with `console-ink`,
  `console-dim`, `console-warn`, `console-err`.
- Radii encode hierarchy: `control` (12px) for 44px buttons and inputs,
  `control-sm` (10px) for 36px ones, `chip` (8px), `plate` (18px), `dialog`
  (20px).
- Shadows: `shadow-plate` (the `frame` utility) is the one content shadow,
  `shadow-plate-hover` lifts a plate under the pointer, `shadow-primary`
  sits under the primary button, `shadow-pop` and `shadow-dialog` for menus
  and dialogs.

## Type

- Root is 16px. `text-sm` is 15px body copy, `text-xs` 13px labels,
  `text-2xs` 12px fine print. Headings: `text-4xl` (48px) page titles,
  `text-lg` (20px) panel titles, 26px plate names, 22px list names.
- Instrument Serif via the `display` utility for every headline: page
  titles, site names, panel titles, the wordmark. Regular weight only.
- Instrument Sans Variable for everything else. IBM Plex Mono is reserved
  for real code: commands, ids, log lines. Numbers use `num` (tabular).

## Components

- `Plate` (grid) and `SiteRow` (list) are the two faces of a site; the
  Sites page toggles between them with `Segmented` and remembers the choice
  (`?view=grid|list` overrides for a visit). `Thumb` renders the preview
  or a placeholder with the site's initial.
- `Panel` is a plate with an optional title row; `Table` is a plate around a
  table. `Menu` is the overflow menu. `CommandPalette` is the ⌘K search.
- Status wording is shared through `components/status.ts` (`toneOf`,
  `statusLabel`); use `StatusPill` (30px chip when `soft`) and `Lamp`.
- `Badge` is the small tinted label for a fact, not a state: tones `brand`,
  `info`, `neutral`, `busy`, `danger`; `sm` is the inline chip ("Latest",
  "This device"), `md` matches the 30px soft StatusPill so the two can sit
  side by side. `RuntimeBadge` is the one home for the runtime wording: a
  site on the legacy runtime (nginx + PHP-FPM, PHP 7.4 to 8.1) gets an
  `info` "Legacy" badge wherever it appears (plate footer, list row, site
  header, version picker); FrankenPHP renders nothing unless `always`.
  Runtime names come from `state/capabilities.svelte.ts` (`runtimeLabel`).
- `Modal` takes `size="lg"` (46rem) for forms, a `header` snippet under the
  title (the wizard's stepper) and `dismissable={false}` while a submit is in
  flight. `Field` renders `error` in place of the hint in `text-danger-ink`;
  the control points at it with `aria-describedby="{for}-error"`. `Select`
  options may be `disabled`: still listed with their hint saying why, skipped
  by the keys.

## Creating a site

`components/create-site/CreateSiteModal` is mounted once in the app shell
and opened through `state/create-site.svelte.ts` from the Sites page, ⌘K
and `/sites/new`. Four steps (Basics → WordPress → Administrator → Review)
with a `Stepper` under the title; Enter advances, done steps are clickable,
"Edit" links on the review jump back. Errors are inline per field; what only
the server knows (a taken slug, a disabled runtime) is mapped back onto its
step. The draft survives a close and reopen; "Start over" blanks it.

The WordPress step is a `VersionPicker`: an inline filterable listbox, one
row per branch pinned to its newest release, with the PHP version and
runtime the row lands on. Rows the server cannot run (legacy disabled) stay
listed, dimmed, with the reason. PHP is derived from the branch; "Advanced"
opens an override limited to compatible, offered versions and warns when the
choice moves the site to another runtime.

## Rules of thumb

- Plates, not cards: one shadow, one radius, generous padding (24px),
  hairlines inside for structure.
- Sentence case everywhere. No uppercase tracked labels, no eyebrows, no
  arrows in button text.
- Buttons say what happens: "Save changes", "Purge page cache", "Sign out".
- Motion only answers state: the lamp breathes while something is in
  progress, dialogs ease in, plates lift on hover, nothing animates on page
  load.
- Empty states invite an action; errors say what happened and what to do.
