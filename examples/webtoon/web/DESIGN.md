# DESIGN.md — Webtoon (Linear-inspired)

> Feed this file to your AI coding agent. Every UI decision should cite this document.
> Inspired by Linear.app's design language: **ultra-minimal, precise, purple accent**.

## Vibe

Dark-first reading platform. Every pixel serves the content (webtoon art). No decorative
flourishes. Type-driven hierarchy. Motion is subtle — 120–200ms ease-out, never bouncy.
The UI should feel like a premium tool, not a consumer app.

## Color tokens (HSL, dark default)

```css
--bg:              222 15% 7%;     /* #0f1014 — deep neutral, not black */
--bg-elevated:     223 15% 10%;    /* #151720 — cards, menus */
--bg-hover:        223 14% 14%;    /* #1d1f2a */
--border:          225 10% 18%;    /* #292c38 — 1px hairlines only */
--border-strong:   225 12% 26%;    /* #3a3e4e — focus rings */
--fg:              220 15% 96%;    /* #f2f3f7 — primary text */
--fg-muted:        220 10% 70%;    /* #a7abb8 — secondary text */
--fg-subtle:       220 8% 50%;     /* #787d8b — tertiary / placeholders */
--accent:          235 85% 65%;    /* #5E6AD2 — Linear signature purple */
--accent-hover:    235 85% 70%;
--accent-subtle:   235 50% 22%;    /* for backgrounds of accent chips */
--success:         145 60% 50%;
--warning:         38 90% 55%;
--danger:          0 72% 60%;
```

Contrast: AAA on `--fg` over `--bg`. Never use pure white or pure black.

## Typography

- Primary: **Inter** (variable), 400 / 500 / 600 only. No 700+.
- Mono: **JetBrains Mono** for IDs, counts, code.
- Scale (line-height in parentheses):
  - `--text-xs`: 12px (16) — captions, meta
  - `--text-sm`: 13px (20) — body small, buttons
  - `--text-base`: 14px (22) — default body
  - `--text-md`: 15px (24) — card titles
  - `--text-lg`: 18px (26) — section headers
  - `--text-xl`: 22px (30) — page headers
  - `--text-2xl`: 32px (38) — hero
  - `--text-3xl`: 48px (56) — landing hero only
- Letter-spacing: `-0.01em` on sizes ≥ `--text-lg`; `0` otherwise.
- All-caps labels: 11px, `+0.08em` tracking, `--fg-muted`.

## Spacing

4px base grid. Use only: 4, 8, 12, 16, 24, 32, 48, 64, 96. Never 10/14/20.

Page gutter: 24px mobile, 32px tablet, 64px desktop. Max content width: **1200px**
(reader width: 720px).

## Radius & borders

- Radius: 6px (buttons, inputs), 8px (cards), 12px (modals). Never pill or fully rounded
  except avatars (50%).
- Borders: 1px, `--border`. Use shadow for elevation, not double borders.

## Shadows

```css
--shadow-sm: 0 1px 2px 0 rgb(0 0 0 / 0.25);
--shadow-md: 0 4px 12px -2px rgb(0 0 0 / 0.35);
--shadow-lg: 0 16px 48px -12px rgb(0 0 0 / 0.5);
--shadow-glow: 0 0 0 1px hsl(235 85% 65% / 0.35), 0 0 24px -4px hsl(235 85% 65% / 0.25);
```

## Motion

- Duration: 120ms (micro), 180ms (default), 240ms (modal/page).
- Easing: `cubic-bezier(0.16, 1, 0.3, 1)` (ease-out-expo-lite). No spring.
- Reduced-motion: disable all transforms; keep opacity only.

## Components

### Button
- Primary: bg `--accent`, text white, hover `--accent-hover`, shadow-sm.
- Secondary: bg `--bg-elevated`, border `--border`, hover `--bg-hover`.
- Ghost: no bg, text `--fg-muted`, hover bg `--bg-hover`.
- Sizes: sm (28px), md (32px), lg (40px). Padding-x: 12 / 16 / 20.
- Icon-only: square, icon 16px, centered.

### Card (series)
- Aspect ratio: 3/4 poster.
- Border-radius 8px, overflow hidden.
- Image fills; on hover: scale 1.03 over 240ms, shadow-md, border `--border-strong`.
- Title below image, `--text-md` weight 500, 1 line clamped.
- Meta row: genre chip + author, `--text-xs`, `--fg-muted`.

### Genre chip
- Inline, bg `--accent-subtle`, text `--accent`, padding 2/8, radius 4px, `--text-xs`
  weight 500, uppercase letter-spacing.

### Navbar
- Height 56px, sticky top, bg `--bg` w/ 80% blur-backdrop on scroll.
- Left: wordmark (monospace `webtoon`) + active link underline 2px `--accent`.
- Right: search (cmd+k), login / avatar.
- Border-bottom 1px `--border`, hairline only.

### Hero (Home)
- Full-bleed 560px tall, gradient radial from `--accent-subtle` top-left fading to
  `--bg`. Content left-aligned, max-width 560px.
- Badge above title: "New this week" in accent chip.
- Title: `--text-3xl`, weight 600.
- Subtitle: `--text-lg`, `--fg-muted`, max 2 lines.
- CTAs: primary "Start reading" + secondary "Browse catalog".

### Row / Carousel
- Horizontal scroll (CSS scroll-snap). Gap 16px. Show peek of next card.
- Arrow controls appear on hover (desktop). Mobile: free swipe.

### Reader
- Vertical single column, max-width 720px, centered.
- Image gap: 0 (webtoons are continuous strips) or 8px (episodic comics).
- Floating top bar: title + episode N, back button, reading progress (1px accent bar
  at top of viewport).
- Tap/scroll to hide chrome after 2s idle.

### Empty state
- Illustration = monochrome line icon 48px `--fg-subtle`.
- Headline `--text-lg`, subline `--fg-muted`, CTA ghost button.

## Iconography

- **Lucide** icons only. Weight 1.5px. Size 16 (inline) / 20 (buttons) / 24 (nav).
- No filled icons except active states.

## Layout patterns

- Lists use 1px borders between rows, never stripes.
- Tables: col headers in uppercase 11px, hairline border-bottom `--border-strong`.
- Pagination: "Load more" button, never numbered. Infinite scroll only for reader.

## Accessibility

- All interactive elements have 2px focus ring `--border-strong` offset 2px.
- Minimum tap target 32px (mobile 44px).
- Respect `prefers-reduced-motion`.
- `aria-current="page"` on active nav; skip-to-content link at top.

## What NOT to do

- ❌ No rounded-full pill buttons.
- ❌ No gradients inside buttons or cards.
- ❌ No drop shadows on text.
- ❌ No more than 2 font weights in a single view.
- ❌ No emoji in UI copy (only in marketing).
- ❌ No skeuomorphism, no glassmorphism beyond navbar blur.
