# Design

## Theme

cold-tool — picked 2026-11-09, retuned to Mayoi's icon. Why: a developer-tool landing for technical buyers; the stock grey/blue was swapped for the icon's pixel gold on indigo night so the page belongs to Mayoi.

## Changed tokens

- `--bg/--surface/--surface-2/--border` — light: lavender paper `#f5f4f9 / #ffffff / #ecebf4 / #d9d6e7`; dark: indigo night `#0f0c24 / #16122f / #201a42 / #2f2856`
- `--accent: #f5a614` (light) / `#ffb52e` (dark) — icon gold; `--accent-contrast` is indigo ink on gold buttons
- `--link/--info/--focus` — indigo `#4b3bb0` in light (gold fails small-text contrast on paper), lavender `#c3b8ff` in dark
- `--fs-*` scale bumped one step (15px base) and `--fs-5xl` added for the hero, with phone overrides
- `--font-jp: "Noto Serif JP"` added — only for the 迷い wordmark and the closing line
- Brand tokens added: `--stage`, `--stage-2`, `--stage-dot`, `--on-stage`, `--on-stage-muted`, `--gold` — the dotted indigo band behind product shots, the theme section and the shell sample, in both modes
- `--th-<palette>-<role>` × 15 palettes (Mayoi, Claude, Qoder, Grok Bot, Cursor in light and dark; The Matrix, Purple Gold and the three Halloween themes dark only) — dumped from the Go palettes by a throwaway test, translucent roles flattened over the editor color; `.mock[data-pal]` maps them onto `--m-*`
- `--shadow-2` deepened for screenshots sitting on the indigo stage

## Rejected

- Nothing rejected by the user yet.
- Self-rejected during build: a hard-stop `linear-gradient` to split the hero stage (gate R04 reads it as a blue-purple gradient) — a flat `::before` in `--bg` does the split instead.

## Accepted

- `.gate/1440.png`, `.gate/1440-dark.png`

## Gate

Passes with 0 errors / 0 warnings in light and dark at 1440/768/390, with one scoped ignore.

```gate
ignore: R09 span.m-ln  # why: the live theme preview paints Mayoi's real palettes from cmd/mayoi/theme.go, whose gutter and
                       # comment colors are deliberately dim; the mock is role="img" with an aria-label, not text to read
```

## Notes

- Section order: "Show the product" — hero + big screenshot → measured speed table → four alternating feature rows → a spec list (not cards) → shell sample → themes on the stage → install tabs → final call.
- Hero shot is the user's own window capture (`assets/hero.webp`, from `Mayoi 2026-11-09 16.58.22.png`): project rail, Git view and two Claude Code terminal tabs. It carries its own frame and shadow, so `.shot-hero img` drops the border and radius; the transparent margin is trimmed to ~48px and the image saved as WebP (1.2 MB PNG → 248 KB).
- Themes section is a live HTML mock, not a screenshot (user asked): chips pick the theme, Dark/Light the appearance (off for dark-only themes); colors fade over `--dur-slow`, as the app fades between themes. The Matrix and Claude switch the mock's UI font to mono, as their `mono` flag does. Code shown is `paletteFor` from `theme.go`; the terminal shows the repo's real last four commits. Regenerate the palette tokens if theme.go changes.
- Every speed figure comes from `docs/ARCHITECTURE.md` (Performance); CLI examples from `internal/cli/request.go`; shortcuts from the README and `docs/FEATURES.md`.
- Motion is limited to hover/press color and transform transitions on tokens; no load or scroll animation.
- Install tabs follow the ARIA tabs pattern (arrows, Home/End) and preselect Windows or Linux from the user agent.
- `.tabpanel[hidden]` needs `display: none` because `.stack` sets `display: flex`.
