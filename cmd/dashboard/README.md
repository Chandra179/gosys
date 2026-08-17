# gosys dashboard — UI notes

Small HTMX+Tailwind web UI around `pipeline.Analyze`. This file documents the
handful of conventions that aren't obvious from reading a single template in
isolation — mainly the parts that have to be duplicated by hand because they
can't be shared via a build step.

## Stack

- **Tailwind via CDN** (`cdn.tailwindcss.com`) and **htmx via CDN**
  (`unpkg.com/htmx.org`) — no bundler, no `npm`, no build step. Templates are
  embedded directly with `go:embed`. Keep it this way unless the page
  outgrows what CDN Tailwind + htmx can do; the appeal of this dashboard is
  that `go build` is the only build step.
- Layout shell: `max-w-[1400px]` container, single `lg:` breakpoint
  (`results.html`'s treemap/findings grid collapses to one column below
  `lg`, sticky treemap panel above it). Don't introduce another breakpoint
  or container width without a reason — one split point keeps the page
  predictable.

## Color semantics

The amber ramp means "how much of this is flagged," from nothing to a lot:

| Meaning              | Tailwind class     | Hex (SVG)  |
|-----------------------|--------------------|------------|
| none flagged          | `slate-200`        | `#e2e8f0`  |
| flagged, small/partial| `amber-200`        | `#fde68a`  |
| flagged, large/mostly | `amber-500`        | `#f59e0b`  |
| flagged, dominant     | `amber-700`        | `#b45309`  |
| label text on a flagged tile | `amber-950` | `#451a03`  |
| label text, unflagged | `slate-700`        | `#334155`  |
| selection outline     | `slate-900`        | `#0f172a`  |

**Why hex strings exist at all:** the treemap in `results.html` is rendered
as inline SVG built by hand in JS (`colorForSite`, `colorForPackage`,
`textColorFor`), and SVG `fill`/`stroke` attributes can't consume Tailwind
utility classes — they need literal color values. Those hex values are
hand-picked to match the Tailwind shades above; nothing enforces that link
automatically. **If you change one side, update the other** — a Tailwind
class edit in the HTML that isn't mirrored in the JS (or vice versa) will
make the on-page legend describe colors that no longer appear on screen.

## The treemap script

`results.html` embeds a hand-rolled squarified treemap (Bruls et al.) in a
plain `<script>` block, not a library — this keeps the whole dashboard a
single Go binary with no external JS dependency to fetch or vendor. It
renders two levels: packages (root) and sites within a package (drilled-in).
Clicking a leaf site tile also scrolls the matching card into view in the
findings list (`findCard`, matched by `data-site="file:line"` rather than a
CSS selector, since file paths contain characters that would need escaping).

If you add a third drill-down level or a genuinely reusable interaction,
that's the point to reconsider pulling in a small charting dependency —
until then, the hand-rolled version is intentional, not a shortcut.
