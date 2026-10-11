# Shadow-Pager Design System: Night Shift

## 1. Visual Theme and Atmosphere

Shadow-Pager is an on-call instrument panel, not a marketing dashboard. Picture an
engineer paged at 3am: the room is dark, the screen is quiet, and the one thing that
is broken is the only thing lit.

- Calm by default. Healthy things are grey, small and still.
- Loud only for meaning. Colour, size and motion are spent exclusively on breaches,
  warnings and the user's current action.
- Instrument, not decoration. Every element reads like a gauge, a tape, a readout or
  a switch. No illustrations, no gradients on surfaces, no glass, no emoji.
- Data first. Numbers are large, monospaced and steady; labels are small and quiet.
- Two faces: the Dashboard and Incidents pages are the dark control room. The public
  Status page is the calm public notice: airier, larger, light by default.

Signature elements that make it ours:
1. Signal rail: a 3px vertical bar on the left edge of every service and incident
   card, coloured by state. It is the only coloured border in the product.
2. Shape glyphs: each failure mode has a tiny 40x16 line drawing of its curve
   (spike, gradual, leak, cliff) used in the inject form, cards and lists.
3. Flat-line idle: with nothing running, the empty state shows a slow, flat
   heartbeat line, like a monitor with nothing to report.
4. Flight recorder tape: incident timelines are a vertical tape with mono
   timestamps on the left and events on the right.
5. SLO lines are labelled in place ("SLO 500 ms") and charts only colour the part
   of the line that is above the threshold.

## 2. Color Palette and Roles

Expose every token as a CSS custom property and as a Tailwind theme colour. Never
use a raw hex in components.

Dark (default for Dashboard and Incidents)

| Token            | Hex       | Role                                                  |
|------------------|-----------|-------------------------------------------------------|
| bg.canvas        | #0A0C0F   | Page background                                       |
| bg.surface       | #111418   | Cards, panels                                         |
| bg.raised        | #171B21   | Popovers, menus, hovered rows                         |
| bg.inset         | #0D1013   | Inputs, chart wells, code                             |
| border.subtle    | #1F242B   | Card edges, dividers                                  |
| border.strong    | #2B323B   | Input edges, focused containers                       |
| text.primary     | #E6E9ED   | Values, headings (never pure white)                   |
| text.secondary   | #9AA3AE   | Body copy, labels                                     |
| text.tertiary    | #7A838F   | Meta, timestamps, helper text                         |
| signal.breach    | #F2555A   | SLO breach, critical, destructive                     |
| signal.warn      | #F5A524   | Degraded, major, approaching threshold                |
| signal.ok        | #3FB984   | Operational, resolved (small marks only)              |
| signal.monitor   | #8B9CF4   | Mitigated, monitoring                                 |
| accent           | #C9D4FF   | Focus ring, selected state, links                     |
| chart.line       | #6B7480   | Default series colour: healthy data is grey           |

Light (default for Status, available everywhere)

| Token            | Hex       |
|------------------|-----------|
| bg.canvas        | #F7F7F5   |
| bg.surface       | #FFFFFF   |
| bg.raised        | #FFFFFF   |
| bg.inset         | #F1F1EE   |
| border.subtle    | #E6E5E1   |
| border.strong    | #D3D2CD   |
| text.primary     | #15171A   |
| text.secondary   | #50555D   |
| text.tertiary    | #6E737B   |
| signal.breach    | #D12F3A   |
| signal.warn      | #A86400   |
| signal.ok        | #1E8E5A   |
| signal.monitor   | #4A5FD0   |
| accent           | #2F4FC9   |
| chart.line       | #9A9EA5   |

Rules
- Signal tints for backgrounds are the signal colour at 10% opacity, borders at 30%.
- Green is never used large. Operational is a 6px dot, not a green banner.
- Only one red thing per card: the rail plus the breaching value, nothing else.
- All text meets WCAG AA against its background; text.tertiary is only for meta
  text at 12px or larger.

## 3. Typography

Families
- IBM Plex Sans (400, 500, 600) for interface text.
- IBM Plex Mono (400, 500) for anything that is data: metric values, IDs, service
  names, durations, timestamps, axis ticks, code, keyboard keys.
- Rule of thumb: if a machine produced it, it is mono. If a human reads it as
  prose, it is sans. Inputs follow the data they hold: the service and duration
  inputs are mono, everything else sans.

Scale (px / line height / weight)
| Name      | Size | LH  | Weight | Use                                        |
|-----------|------|-----|--------|--------------------------------------------|
| overline  | 11   | 16  | 500    | Mono, uppercase, 0.08em tracking, section labels only |
| meta      | 12   | 16  | 400    | Timestamps, helper text                    |
| body      | 14   | 20  | 400    | Default                                    |
| label     | 13   | 18  | 500    | Form labels, button text                   |
| title     | 16   | 24  | 600    | Card titles                                |
| heading   | 20   | 28  | 600    | Page titles                                |
| readout   | 28   | 32  | 500    | Mono, primary metric values                |
| hero      | 40   | 44  | 600    | Status page overall state only             |

- Tabular numerals on every number (font-variant-numeric: tabular-nums).
- Units sit next to values in text.tertiary at a smaller size: "824 ms".
- Sentence case everywhere. Failure mode names are lowercase inside sentences
  ("Started an error rate surge on payments-api") and sentence case as labels.

## 4. Component Stylings

Header
- 48px tall, bg.canvas, bottom border.subtle. Logo mark plus "Shadow-Pager" left;
  nav tabs as text with a 2px accent underline for the active tab; right side holds
  the connection indicator (dot plus "Live", "Reconnecting" or "Waking server") and
  a Settings icon button that opens a popover with theme and admin token.

Cards
- bg.surface, 1px border.subtle, radius 8px, padding 16px, no shadow in dark mode.
- Left signal rail 3px, full height, inside the radius, coloured by state; healthy
  cards have no rail.
- Breaching cards: rail in signal.breach with a slow 2.4s opacity pulse between
  100% and 55%. Static under prefers-reduced-motion.

Buttons
- Primary: text.primary background, bg.canvas text, radius 6px, 36px tall.
- Secondary: transparent, 1px border.strong, text.primary.
- Destructive (Halt): secondary style; on hover the border and text turn
  signal.breach. Never a solid red button.
- All buttons show a 2px accent focus ring offset by 2px on keyboard focus only.

Inputs
- 36px tall, bg.inset, 1px border.strong, radius 6px, 12px horizontal padding.
- Labels above, 13px label style. Helper text below in meta style, and every field
  in a row reserves the same helper height so fields always line up.
- Errors replace helper text in signal.breach with a leading 6px dot.

Drill composer (the inject form)
- Title "Run a drill". Fields in this order: service, failure mode, severity,
  intensity, duration.
- Failure mode is a 2x2 grid of option cards, each with its shape glyph, name and a
  one-line description. Selected card gets border.strong plus accent ring.
- Severity is a 3-segment control: Critical, Major, Minor.
- Intensity is a custom slider: 4px bg.inset track, filled part text.secondary,
  14px thumb in text.primary, value shown as a mono readout on the right.
- Duration is chips (30s, 2m, 5m, 10m) plus a "Custom" chip that reveals a mono
  input.
- Submission feedback appears as a transient line under the button for 4 seconds,
  then the new simulation card appears in the list. Feedback never outlives the
  state it describes.

Service and simulation cards
- Group by service. Card header: service name (mono, title size), severity tag,
  shape glyph, mode name, and time remaining as a 2px progress bar under the header.
- Breaching metrics: full-width chart, 120px tall, with the readout above it.
- Healthy metrics: compact row of mono value plus a 64x20 sparkline, text.tertiary.
- Halt button top right.

Charts
- No chart borders or backgrounds; hairline gridlines in border.subtle, max 3.
- Line 1.5px in chart.line. Only segments above the SLO turn signal.breach, with a
  10% breach tint filled between the line and the SLO.
- SLO threshold is a 1px dashed line in signal.breach at 60% opacity with an
  inline label "SLO 500 ms" in mono meta at the right end.
- Y ticks: max 3, rounded to human values (0, 250, 500, 750), with units only on
  the top tick. X axis shows relative time ("-60s", "now").

Tags
- 20px tall, radius 4px, mono 11px uppercase, signal tint background and signal
  text. Healthy and finished states use a neutral tag: border.subtle outline,
  text.tertiary.

Lists and tables (Recently finished, Incidents)
- Dense rows, 44px, separated by border.subtle hairlines, no cards.
- Columns carry information, never decoration: service, mode with glyph, outcome
  ("Breached SLO, incident INC-12" or "Stayed healthy"), peak value, duration,
  relative time. Row hover is bg.raised.

Incident detail
- Header: incident ID, status tag, severity tag, service, live duration.
- Two readouts: time to acknowledge and time to resolve, in readout style.
- Flight recorder tape: vertical 1px border.strong line, 8px dots per event, mono
  timestamps on the left column, actor and text on the right.

Empty states
- One line of text.secondary, one short instruction, and the flat-line heartbeat
  drawn in chart.line. No boxes with dashed borders.

Status page
- Light by default. Max width 760px, centred. Large hero line with the overall state
  ("All systems operational", "Degraded performance", "Major outage") with a 10px
  signal dot.
- One row per service: name, state word, current p99 latency and error rate in
  mono meta. Active incidents listed below as cards with the signal rail and their
  latest timeline entry.
- Footer: "Last updated" relative time, ticking.

## 5. Layout Principles

- 4px spacing base. Common steps: 4, 8, 12, 16, 24, 32, 48.
- Container max width 1440px with 24px side gutters (16px on phones).
- Dashboard: 12 column grid. Composer spans 4 columns and is sticky below the
  header on large screens; simulations span 8.
- Section labels use the overline style with 12px gap to their content.
- Vertical rhythm: 32px between page sections, 16px inside cards.
- Alignment: every row of fields shares a baseline; never let helper text push
  fields out of line.

## 6. Depth and Elevation

- Dark mode uses borders and surface steps, never shadows, for elevation.
- Only popovers and menus get a shadow: 0 8px 24px at 40% black in dark mode,
  0 8px 24px at 8% black in light mode.
- Nothing floats over content except popovers, menus and the toast region.

## 7. Do's and Don'ts

Do
- Spend colour only on meaning.
- Make the breaching metric the largest, brightest element on screen.
- Use mono for every machine-generated value.
- Show relative times ("2m ago") with the absolute time in a title tooltip.
- Keep motion under 200ms with ease-out, except the breach pulse.

Don't
- Use gradients, glassmorphism, glows, emoji or illustrations.
- Use pure black or pure white.
- Repeat the same tag on every row; if everything is "Finished", say nothing.
- Use blue as a default chart colour.
- Show more than three gridlines or more than three ticks per axis.
- Animate numbers counting up; values change instantly.
- Put success messages next to empty states that contradict them.

## 8. Responsive Behavior

- 1280px and up: 4 and 8 column split, composer sticky.
- 768px to 1279px: composer collapses into a full-width panel above the list.
- Below 768px: single column, header nav moves into a bottom tab bar, charts keep
  their aspect, compact metric rows wrap to two per line.
- Touch targets at least 40px. No horizontal page scroll at 360px width.

## 9. Agent Prompt Guide

When building or changing any screen:
1. Ask: what is broken right now, and is it the most visible thing? If not, fix that
   first.
2. Use only tokens from section 2 and type styles from section 3.
3. Mono for data, sans for prose. Tabular numerals always.
4. Use the signal rail and shape glyph instead of extra badges or icons.
5. Every state is designed: loading, empty, waking server, reconnecting, error.
6. Check both themes and 360px width before calling anything done.