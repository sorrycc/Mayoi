# Design

How Mayoi should feel, and the tests that hold it there. A rule here that no test checks is a rule for review; add
the test when you can. The website has its own, in `website/DESIGN.md`.

The order: it works, then it is easy, then it looks good. Polish never covers a broken interaction.

## Motion

- Every animation takes its length from a token in `cmd/mayoi/motion.go`: `motionTap` (100 ms), `motionLeave`
  (110), `motionHover` (120), `motionShow` (150), `motionMove` (200) and `motionEnter` (240). `TestMotionUsesTokens` fails on a length
  written out anywhere else.
- What a person does a hundred times a day does not move. A dialog opened by a key (the command palette, Quick Open,
  a picker) shows at once (`keyedOpen`); opened with the pointer, it fades in over `motionShow`. The chosen row of a
  list moves with the keys: where it glides, as in Sessions, it starts at once and a key that repeats puts it there.
- Motion says where something came from, or that a state changed. Looking nice is not a reason.
- Coming in and going out ease out; moving across the screen eases in and out. Nothing eases in.
- Going out is shorter and smaller than coming in.
- With Reduce Motion on, nothing moves: colors fade at most (`shake` and `Keyframes` already stop).
- Something visible changes within a frame of the key or click, even when the work takes longer.

## Stability

- A state that changes moves nothing else. A tab's title, its neighbours and its close button stay put when the
  pointer comes over it, its file gets unsaved changes, or it becomes the chosen tab (`TestTabTitlesHoldStill`). The
  chosen tab is marked by color, not weight, as a heavier weight widens it.
- Numbers that change in place use tabular digits (`FontFeatures("tnum")`), as the caret's position in the status
  bar and find's count (`TestStatusPositionHoldsItsWidth`).
- Hover never moves what is clicked.

## States

- Each surface shows each of its states: empty, loading, error, offline or reconnecting, and with no permission.
- The state gallery renders surfaces in their states across every theme, light and dark, and every language, into
  one page: `GALLERY=<dir> go test -run TestGallery ./cmd/mayoi`, then open `<dir>/index.html`. Add a state to
  `galleryStates` when you add a surface.

## Speed

- `TestBudgets` (`cmd/mayoi/budget_test.go`) holds the cost of a frame to a budget: idle, scrolling, typing, typing
  with find open, Quick Open, the palette, 2,000 sessions scrolled, hovered and filtered, and a terminal printing as
  fast as it can. CI runs it on macOS.
  A change that needs more raises a budget in the same commit and says why.
- The figures on the website and in `docs/ARCHITECTURE.md` come from these tests.

## Failure

- A failure says what happened, keeps what the person needs to understand it, and offers the next step.
- Nothing that failed vanishes: a terminal whose shell exits as it starts stays open with its output and offers
  **Report…** (`TestTerminalExitedEarlyStays`).
- **Help → Report a Problem…** opens a new issue with the version and system filled in (`reportURL`). Offer it
  wherever a failure has no next step of its own.

## Words

`TestCopyRules` checks the catalogs in `locales/`:

- Every language has every message, with the same arguments.
- A count before a noun uses `{n, plural, …}`, never `{n} files`.
- An ellipsis is `…`, one character.
- No "please", "successfully", "click here", "are you sure" or "(s)": say what happens.

And, for review:

- Let the layout explain the screen. Drop a subtitle or helper text that restates a label; if a screen needs one,
  change the layout. Keep labels of fields and the accessible names of icon buttons.
- One verb for one action across the app.
- Confirm only what cannot be undone.
- A time within a week reads as how long ago ("3 days ago"); older, as a date ("Mar 4", "Mar 4, 2024").

## Look

- The page is flat: shadows only for what floats (`floating`), no box in a box.
- An inner radius is the outer one less the padding between them.
- The accent is for the one thing that matters on a screen.
