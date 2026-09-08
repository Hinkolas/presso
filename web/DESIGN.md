# presso UI design notes

The web UI is a control plane for WordPress sites on one host. Its job is to
show at a glance what is up and healthy and to let an operator act on it.
The direction is called "Atelier": light and editorial, with the site
previews as the hero on soft white plates, a serif headline voice, and a
sidebar that has room to grow.

## Shell

- A 260px sidebar: Sites at the top; pinned to the bottom, System and the
  account card (avatar, name, email; flat, sunken on hover; opens the
  account menu upward). The
  System row carries a dot when a component is bad, the event stream
  dropped, or a job is running; the health verdict itself is the System
  page subtitle. Nothing else is padded in: new sections (domains,
  backups, hosts) get a row when they get a page.
- System has two tabs (`?tab=`): Overview (components, Docker, edge, disk)
  and Activity, the job history. `/jobs` redirects to the Activity tab;
  job detail pages stay at `/jobs/{id}` and read as System › Activity › Job.
- A 64px topbar carries the breadcrumb and the global search (⌘K opens the
  command palette).
- Pages live in a 1400px column with 48px side padding and 36px above.

## Tokens

All colors, radii and shadows are semantic tokens declared in
`src/routes/layout.css` (Tailwind `@theme`) and redefined for dark mode. Use
the token classes (`bg-surface`, `text-ink-2`, `border-rule`, ...), never raw
palette values.

- Surfaces: `ground` (page), `surface` (plates, sidebar, topbar), `pop`
  (menus and dropdowns floating over a surface; white in light, a step
  lighter than `surface` in dark so they separate without a border),
  `sunken` (inset areas), `rule` and `rule-strong` (hairlines and control
  borders).
- Text: `ink`, `ink-2` (secondary), `ink-3` (muted).
- Meaning: `brand` (forest green, primary actions and the "running" state),
  `ok`, `busy` (brass, anything in motion), `danger` (failure only), `info`.
  Each has a `-soft` tint and an `-ink` text color that reads on that tint.
  `brand-deep` stays dark in both schemes for large fills.
- Console panes are always dark (`console` utility) with `console-ink`,
  `console-dim`, `console-warn`, `console-err`.
- Radii encode hierarchy: `control` (12px) for 44px buttons and inputs,
  `control-sm` (10px) for 36px ones and nav items, `chip` (8px), `key`
  (6px, keycaps and checkboxes), `plate` (18px), `dialog` (20px). No
  arbitrary `rounded-[…]` values outside the Wordmark.
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
- `SiteRow` is a plate laid out sideways, not a table row: 120px thumb,
  name and domain, a fixed-width status column, then the hour's numbers as
  value-over-caption cells (`w-18`, right-aligned) so they read as columns
  down the list without a header. A site that is not serving puts what it
  is doing where the numbers would be: the failure in `text-danger-ink`,
  "Stopped 3 h ago · Start", or the setup step and its bar. Rows stack with
  `gap-3`. The list is a `@container`; p95 appears from `@4xl` (56rem) and
  the runtime cell from `@5xl` (64rem). Under 44rem of content width the
  page measures itself, hides the view toggle and shows the grid card in
  one column instead: the row never folds.
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
- No bare browser controls. `Input` is every text-like field (text, email,
  password, search, url): one 44px frame with optional leading `icon`,
  literal `prefix`/`suffix` ("wp", a unit), `mono` for code-shaped values,
  `invalid`, and a built-in show/hide toggle on passwords (`bind:shown` to
  drive it from a "Generate" button). `NumberInput` holds a real number,
  shows its unit and a two-chevron stepper, steps on arrow keys and snaps to
  `min`/`max` on blur. `Checkbox` and `Toggle` draw their own box and track
  over an sr-only native input. `Skeleton` is the loading placeholder
  (`plate` for whole-panel stand-ins). `BackLink` is the "‹ Sites" link above
  a detail title (PageHeader's `back` uses it).
- Focus: buttons and links get the global 2px outline; text-like controls
  draw a brand hairline plus a 3px `brand/20` halo through the `field-focus`
  / `field-focus-within` utilities. Never both on one element.
- `Menu` takes `children` to render a custom trigger (the topbar account
  button) and `header` for a non-interactive block above the items; arrow
  keys move through items. Every icon-only action in a table is
  `Button variant="ghost" size="sm" icon=…` with an `aria-label`.
- `Sheet` is the side panel: a plate that slides in at the right edge on
  the native `<dialog>`, for detail that belongs beside the page rather than
  on top of it (a job's timeline). `title`, `subtitle`, an `aside` snippet
  next to the title (a StatusPill), scrolling body, fixed `footer`.
- `ProgressBar` is the one track (1.5px `md`, 1px `sm`): brass while in
  motion, green complete, red stopped short; `value` null sweeps.

## Following a job

The engine declares each job kind's steps (`plan` on the job payload) and
`lib/job-progress.ts` folds the plan and the event log into done / active /
todo / skipped / failed steps, a position and a fraction. Everything that
shows a job reads that model, never the raw events:

- On the Sites page a site being set up or torn down keeps its card; the
  preview area becomes the setup face: the step under way, a `ProgressBar`,
  "Step 5 of 10 · 12 s". The list row puts the same bar and line under the
  status pill. When the job finishes the card turns into an ordinary one.
- The site page stays the site page. A job under way is followed by
  `JobToast`, a small plate at the bottom right (title, current step, bar,
  counter, elapsed). Clicking it opens the timeline in a `Sheet`; the ×
  hides it. A success fades out after six seconds; a failure stays until
  dismissed. The toast only appears for a job seen live on that page: after
  a reload a failed site shows its message in the header with a "Show what
  happened" link that opens the same Sheet.
- `JobTimeline` is the spine of steps (ticked, breathing, hollow, dashed
  for skipped), each with the handler's wording and its log lines, the
  failure under the step that hit it, and per-step durations. The job
  detail page under Activity uses it too. There is no "Last operation" panel on Overview.
- Creating a site closes the wizard and shows the new card (scrolled into
  view) rather than opening the site; deleting one returns to the Sites
  page, where the card shows the teardown and then disappears.

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

## Settings

The site Settings tab is four panels (General, PHP, Container, Edge cache)
and a save bar that appears only while the form is dirty, sticky at the
bottom of the content pane. The name and the cache warmer toggle apply in
place; anything else recreates the container (mirrors `internal/site/ops.go`), so
those saves go through a confirmation dialog that names the changed fields
and the short outage. There is no explanatory side column and no standing
help text.

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
