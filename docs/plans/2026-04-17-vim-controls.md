# Vim-Style Controls

## Overview
Add vim-style keyboard affordances to revdiff for users with vim muscle memory:
numeric count prefixes on motions (`5j`, `10k`, `3]`), `Ctrl-W` pane-switching
chord, `gg`/`G` jumps, `zz`/`zt`/`zb` viewport alignment, and `{`/`}` as
aliases for `[`/`]` (prev/next hunk). Pending count/chord state is surfaced
in the status bar so the user sees what they've typed before completing the
motion.

## Context
- Files involved: `app/keymap/keymap.go`, `app/ui/model.go`, `app/ui/diffnav.go`, `app/ui/handlers.go`, `app/ui/view.go`, `app/ui/sidepane/filetree.go`, `app/ui/sidepane/toc.go`
- Related patterns:
  - `keymap.Action` + `keymap.Keymap.Resolve()` is the existing single-point of key → action mapping. Chord/count state must live in `ui`, not `keymap`, because they cross pane contexts and mutate model state.
  - `navigationState` in `model.go:262` already groups cursor/nav fields; vim-input state fits alongside.
  - `FileTree.Move(m Motion, count ...int)` and `TOC.Move(...)` already accept variadic count (`app/ui/sidepane/filetree.go:119`, `toc.go:124`) — only page motions use it today. Extend to step motions.
  - Status bar right-side rendering is in `statusBarText()` at `app/ui/view.go:132-144`.
- Dependencies: none (all target bindings are currently unbound except `L`, which is out of scope after scope trim).

## Goals
1. `N` + motion key repeats the motion `N` times (motions listed in Action Table below). Max cap `10000` to prevent accidental runaway.
2. `Ctrl-W` followed by `h` / `l` / `w` / `j` / `k` manipulates pane focus.
3. `gg` jumps to file top; `G` jumps to file bottom. Count on `G` is ignored in this iteration (documented).
4. `zz` / `zt` / `zb` align the viewport so the cursor sits in the middle / top / bottom of the pane.
5. `{` / `}` act as additional bindings for `prev_hunk` / `next_hunk`.
6. Pending count (e.g. `5`) and pending chord (e.g. `g`, `z`, `^W`) display in the status bar, right-side, leftmost segment.
7. Existing non-vim bindings are untouched. Users who never type a digit or chord prefix see zero behavior change.

## Non-Goals (out of scope)
- **No word motion.** `w`, `b`, `e` will not be added. The `w`/`W` keys stay bound to their current toggles (wrap / word-diff). All count-aware motion in this design is **line-based**, not word-based.
- Viewport-relative cursor moves (`H` / `M` / `L`).
- Line-char search (`f`, `F`, `t`, `T`).
- Marks (`m` / `'`).
- Command mode (`:`).
- `NG` (absolute line jump with count on `G`).

## Design

### State

Add a new grouped struct `inputState` in `app/ui/model.go` alongside `searchState`, `annotationState`, etc.:

```go
// inputState holds transient keyboard-input state for vim-style prefixes.
// Both fields clear on completion, Esc, or any non-participating key.
type inputState struct {
    pendingCount int    // accumulated count prefix, 0 = none
    pendingChord string // "" | "g" | "z" | "ctrl+w"
}
```

Added to `Model` as field `vim inputState` (name avoids confusion with existing `annot.input` / `search.input` textinputs).

### Key-handling flow

A new file `app/ui/viminput.go` owns the prefix logic. The entry point is
called from `Model.handleKey` (`model.go:581`) immediately after
`handleModalKey` and before `handleOverlayOpen`:

```go
if handled, model, cmd := m.consumeVimPrefix(msg); handled {
    return model, cmd
}
```

`consumeVimPrefix` returns `handled=true` only when:
- The key was absorbed into `pendingCount` or `pendingChord` without dispatching an action (e.g. user typed `5`, `g`, or `ctrl+w`).
- The key completed a chord and the resulting action was dispatched here (e.g. `gg` → dispatched `ActionHome`, `ctrl+w`+`l` → dispatched `ActionFocusDiff`).

Otherwise it returns `handled=false` and leaves the existing dispatch path to
run. When `pendingCount > 0` and the normal dispatch resolves a
count-supporting action, the dispatch uses the count; `consumeVimPrefix`
clears `pendingCount` on exit regardless of whether an action consumed it
(so a non-motion key after a count silently discards the count, matching
vim).

**Order of checks inside `consumeVimPrefix`** (applied to a single key press):

1. If any modal is active (annotation, search, overlay, discard prompt): return `handled=false` without mutating state.
2. If `Esc`: clear both `pendingCount` and `pendingChord`; return `handled=false` (let existing `handleEscKey` run).
3. If `pendingChord` is non-empty:
   - If the key completes the chord: clear both fields, dispatch the chord's action, return `handled=true`.
   - Else: clear `pendingChord` only (count is preserved — it might still apply to this key), and fall through to step 4.
4. If the key is a digit and `(digit != "0" || pendingCount > 0)`: absorb into `pendingCount` (capped at `10000`), return `handled=true`.
5. If the key is a chord starter (`g`, `z`, `ctrl+w`): set `pendingChord`, return `handled=true`. If count was non-zero and the chord doesn't support count, count stays pending but will be cleared on chord completion.
6. Otherwise: return `handled=false`. Caller dispatches normally. After dispatch, `pendingCount` is cleared.

Overlay / annotation / search modal states short-circuit `consumeVimPrefix`
(it returns `handled=false` with no state change) so digits inside a search
query or annotation text are unaffected.

### Count prefix semantics

Digit absorption rules:
- Keys `1`…`9`: always absorbed into `pendingCount` (multiply-by-10 + digit).
- Key `0`: absorbed only when `pendingCount > 0`; when `pendingCount == 0`, falls through to normal dispatch (nothing is bound to `0` today; harmless).
- Max cap: `pendingCount` saturates at `10000`. Further digits are silently dropped.

A `pendingCount` value of `N` repeats the dispatched action `N` times for
the actions in the table below. Repetition is implemented by the action
handler (e.g. `moveDiffCursorDown` called in a loop, or passing count
through to `tree.Move(MotionDown, count)`). Centering / viewport-sync
happens once at the end, not per iteration, for performance.

### Chord prefix semantics

| First key | pendingChord set to | Completion keys | Result |
|-----------|---------------------|-----------------|--------|
| `g`       | `"g"`               | `g` → `ActionHome`; else → clear chord and re-dispatch second key | jump to top |
| `z`       | `"z"`               | `z` → center; `t` → top-align; `b` → bottom-align; else → clear chord and re-dispatch second key | viewport alignment |
| `ctrl+w`  | `"ctrl+w"`          | `h`/`k` → focus tree; `l`/`j` → focus diff; `w` → toggle pane; else → clear chord (swallow, do NOT re-dispatch) | pane focus |

Rationale for the asymmetry: `g` and `z` are single-letter keys in active
use (e.g. `g` might later be bound to something, `zj`/`zk` could have
semantics in future). Re-dispatching the second key preserves flexibility.
`Ctrl-W` is a dedicated modifier; there is no reason to re-dispatch a
mis-chorded second key — swallowing is cleaner and matches vim (`<C-W>q`
closes the window; there is no passthrough).

`Esc` always clears `pendingCount` and `pendingChord`; falls through to the
existing `handleEscKey` only if both were already empty.

### Action Table

| Action | Count-aware | Chord binding | Notes |
|--------|-------------|---------------|-------|
| `ActionDown` / `ActionUp` | yes | — | Repeats `moveDiffCursorDown/Up` `N` times; single `syncViewportToCursor` at end. |
| `ActionPageDown` / `ActionPageUp` | yes | — | Repeats page motion `N` times. |
| `ActionHalfPageDown` / `ActionHalfPageUp` | yes | — | Repeats half-page motion `N` times. |
| `ActionScrollLeft` / `ActionScrollRight` | yes | — | Scroll step × N. |
| `ActionNextHunk` / `ActionPrevHunk` | yes | `}` / `{` (new keys) | Hunk jump × N. Cross-file behavior unchanged (triggered once per jump). |
| `ActionNextItem` / `ActionPrevItem` | yes | — | N files in tree mode; N search matches in search mode. |
| `ActionHome` | — | `gg` | Single target, count ignored. |
| `ActionEnd` | — | `G` | Single target, count ignored (documented non-goal). |
| `ActionFocusTree` | — | `ctrl+w` + `h`/`k` | Additional bindings. |
| `ActionFocusDiff` | — | `ctrl+w` + `l`/`j` | Additional bindings. |
| `ActionTogglePane` | — | `ctrl+w` + `w` | Additional binding. |
| *New*: viewport-center | — | `zz` | No `Action` constant — implemented as direct call to `centerViewportOnCursor()`. |
| *New*: viewport-top-align | — | `zt` | Direct call to `topAlignViewportOnCursor()`. |
| *New*: viewport-bottom-align | — | `zb` | Direct call to new `bottomAlignViewportOnCursor()`. |

Viewport-alignment chords do **not** get `keymap.Action` constants because
they are unconfigurable and chord-only. They are dispatched directly from
`consumeVimPrefix`. Rationale: introducing an `Action` for every chord
completion would bloat the keymap surface without gain — users can't
sensibly rebind `zz` to a different chord through the existing keymap file
format.

Hunk aliases `{` / `}` DO get keymap entries in `defaultBindings()` so
users can unmap them.

### Keymap additions (`defaultBindings`)

```go
"G":      ActionEnd,
"{":      ActionPrevHunk,
"}":      ActionNextHunk,
```

No entry for `gg`, `zz`, `zt`, `zb`, `ctrl+w h`, etc. — those are chord-only
and live in `viminput.go`.

### Sidepane `Move` count extension

`FileTree.Move(MotionUp | MotionDown, count...)` currently ignores count
for step motions. Extend both `FileTree.Move` and `TOC.Move` to multiply
the step motion by `count[0]` when supplied.

Update `FileTreeComponent` and `TOCComponent` interface docs in
`app/ui/model.go` accordingly; no signature change (variadic already
present).

### Viewport bottom-align

Add `bottomAlignViewportOnCursor()` in `app/ui/diffnav.go` next to
`topAlignViewportOnCursor`. Implementation mirrors the top-align helper but
positions the cursor on the last viewport row:

```go
func (m *Model) bottomAlignViewportOnCursor() {
    cursorY := m.cursorViewportY()
    m.layout.viewport.SetYOffset(max(0, cursorY-m.layout.viewport.Height+1))
    m.layout.viewport.SetContent(m.renderDiff())
}
```

### Status bar pending indicator

In `statusBarText()` (`view.go:132-144`), prepend a new segment to
`rightParts` when `m.vim.pendingCount > 0 || m.vim.pendingChord != ""`:

```go
if seg := m.vimPendingSegment(); seg != "" {
    rightParts = append([]string{seg}, rightParts...)
}
```

Format:
- count only: `"5"` (plain digits)
- chord only: `"g"`, `"z"`, or `"^W"` (literal caret-W for `ctrl+w`)

Count clears when a chord starts in scope B (none of `gg`, `zz`, `zt`,
`zb`, or `ctrl+w` combinations consume count), so the combined `"5g"`
state is not reachable. If a future chord gains count semantics, the
combined format becomes `"<count><chord>"`.

Rendered with the status-bar background intact and an accent foreground
(`style.ColorKeyAccentFg`) using raw ANSI (via `style.AnsiFg` on the
resolved color) — matching the pattern already used for inline status-bar
elements to avoid the lipgloss full reset that would break the bar's
background.

### Modal / overlay interactions

`consumeVimPrefix` is a no-op (returns `handled=false`, no state mutation)
when any of these are true:
- `m.annot.annotating`
- `m.search.active`
- `m.overlay.Active()`
- `m.inConfirmDiscard`

Digits typed in search/annotation text inputs therefore reach those
textinputs untouched. Chord prefixes are also ignored in those modes.

When `pendingCount` or `pendingChord` is non-empty and a modal state begins
(e.g. user presses `/` to start search), the prefix state clears so
returning from the modal doesn't surprise with stale pending input.

### Backward compatibility

- `G`, `{`, `}` are newly bound. Users with custom keybindings that already map these keys will see their overrides win (parse order: defaults first, then file).
- No existing binding changes.
- `Ctrl-W` was unbound; using it as a chord prefix does not conflict.
- Digit keys `0`-`9` were unbound; adding count absorption is a pure extension.
- Help overlay (`?`) gains a new `"Vim"` section (added to `defaultDescriptions` in `app/keymap/keymap.go`) documenting: count prefix (as a one-line note, since it has no `Action`), `gg`/`G`, `zz`/`zt`/`zb`, `Ctrl-W h/l/w/j/k`, and `{`/`}`. Chord-only entries (e.g. `gg`, `zz`) have no `Action` constant so they appear as static rows in the help overlay, rendered by a small extension to `buildHelpSpec()` that appends a fixed list of chord descriptions.

### Testing

**Backward-compatibility guarantee.** The full pre-existing test suite
(`make test`) must pass unchanged. No existing test case may be modified
to accommodate the new behavior — if a test breaks, the change has
altered user-visible behavior outside the vim-prefix path, which is a
regression. Any required adjustments are limited to *additive* assertions
in new tests, not edits to existing ones.

New / extended coverage:
- `app/ui/viminput_test.go`: count accumulation, chord transitions, chord re-dispatch, `Esc` clearing, modal short-circuit, max-count cap.
- Extend `app/ui/diffnav_test.go`: verify `5j`/`10k`/`3]`/`{` move cursor N steps.
- Extend `app/ui/handlers_test.go` or similar: `gg`/`G` jump targets, `zz`/`zt`/`zb` viewport offsets, `Ctrl-W` pane focus transitions.
- Extend `app/ui/sidepane/filetree_test.go` and `toc_test.go`: `Move(MotionDown, 5)` steps 5 entries (additive; existing `MotionDown` single-step tests remain).
- `app/ui/view_test.go`: pending-indicator segment renders for count-only and chord-only states; absent when both empty.
- **Regression checks** (new tests against the existing behavior contract):
  - Pressing `j` alone (no prefix) moves exactly one line — same as today.
  - Pressing a currently-bound key that is *not* a chord starter, count digit, or modifier (e.g. `a`, `t`, `?`) dispatches immediately with no state change.
  - Typing digits while the search input is active or annotation input is active passes the digits through to those textinputs untouched.

### Documentation

- `README.md`: new "Vim-style controls" subsection under the key bindings section.
- `site/docs.html`: mirror the README additions.
- `app/keymap/keymap.go` help section text: add descriptions for the new actions so `--dump-keys` and the help overlay list them.
- `.claude-plugin/skills/revdiff/references/usage.md`: document the vim keys.

## Risks / Open Questions
- **None at this stage.** Scope and key assignments are settled. The only runtime decision is the muted-color choice for the status-bar indicator, which is an implementation detail and will be resolved during build.

## Development Approach
- **Testing approach**: TDD — write failing tests first, then implement.
- Run `make test` after each task; every task must be green before starting the next.
- Commit after each task.
- Run `make lint` before the final task.
- Do not modify existing test cases. All backward-compat guarantees are enforced by keeping the existing suite green without edits.

## Implementation Steps

### Task 1: Foundational state — `inputState` + `clearVimPrefix`

**Files:** Modify `app/ui/model.go`

- [ ] Add `inputState` struct (after `annotationState` at `model.go:277`):

```go
// inputState holds transient keyboard-input state for vim-style prefixes.
// Both fields clear on completion, Esc, or on entering a modal.
type inputState struct {
    pendingCount int    // accumulated count prefix, 0 = none
    pendingChord string // "" | "g" | "z" | "ctrl+w"
}
```

- [ ] Add `vim inputState` field to `Model` (alongside `annot annotationState`).
- [ ] Add `clearVimPrefix()` method to `Model` (pointer receiver) that zeroes both fields.
- [ ] Add test `TestModel_ClearVimPrefix` in a new file `app/ui/viminput_test.go` verifying both fields zero.
- [ ] Run `make test` — must pass.
- [ ] Commit: `feat(ui): add inputState struct for vim-style prefix state`.

### Task 2: `consumeVimPrefix` — digit absorption + modal short-circuit + Esc clear

**Files:** Create `app/ui/viminput.go`, create/extend `app/ui/viminput_test.go`, modify `app/ui/model.go`.

- [ ] Write failing tests in `app/ui/viminput_test.go`:
  - `TestConsumeVimPrefix_Digits`: typing `5` sets `pendingCount=5`, returns `handled=true`.
  - `TestConsumeVimPrefix_MultiDigit`: `1`,`0` → `pendingCount=10`.
  - `TestConsumeVimPrefix_LeadingZero`: `0` when count=0 → `handled=false` (fall through), count unchanged.
  - `TestConsumeVimPrefix_CapAt10000`: `1` eight times → `pendingCount=10000` (saturating).
  - `TestConsumeVimPrefix_ModalShortCircuit`: sets `m.search.active=true`, typing `5` → returns `handled=false`, count unchanged.
  - `TestConsumeVimPrefix_EscClearsPending`: with `pendingCount=5`, pressing Esc → both fields zero, `handled=false`.
- [ ] Run: `go test ./app/ui/ -run ConsumeVimPrefix -v`. Expected: compile error, then FAIL.
- [ ] Create `app/ui/viminput.go` with `consumeVimPrefix(msg tea.KeyMsg) (handled bool, model Model, cmd tea.Cmd)`. Implement only: modal short-circuit (annotating, search.active, overlay.Active, inConfirmDiscard); Esc clear; digit absorption with `1-9` always, `0` only when count>0; cap at 10000.
- [ ] Run tests — must PASS.
- [ ] Integrate call in `Model.handleKey` (`model.go:581`) immediately after `handleModalKey` and before `handleOverlayOpen`:

```go
if handled, model, cmd := m.consumeVimPrefix(msg); handled {
    return model, cmd
}
```

- [ ] Add `clearVimPrefix()` call at the bottom of `handleKey` (after all dispatch paths return). Simplest: wrap the existing dispatch body and `defer m.clearVimPrefix()` is not right because Model is value-typed. Instead, before each `return` in the dispatch switch, copy count locally and clear on the model. Cleanest: read count at the top of each count-aware handler and clear within. **Decision:** Task 3+ handlers will own both the read and the clear. This task leaves count intact after digit absorption so the next keystroke can consume it.
- [ ] Run full `make test` — must pass.
- [ ] Commit: `feat(ui): add consumeVimPrefix digit absorption and modal guard`.

### Task 3: Count-aware line motion (j/k in diff pane)

**Files:** Modify `app/ui/diffnav.go`, extend `app/ui/diffnav_test.go`.

- [ ] Write failing tests:
  - `TestModel_CountDown_5j`: 20-line file, press `5`,`j` → `diffCursor==5`.
  - `TestModel_CountUp_3k`: cursor at 10, press `3`,`k` → `diffCursor==7`.
  - `TestModel_CountClearsAfterMotion`: `5`,`j`,`j` → final cursor 6 (5+1), not 10.
  - `TestModel_CountCapped`: `1`,`0`,`0`,`0`,`0`,`j` in a 20-line file → cursor at last visible line, no hang.
- [ ] Run tests — FAIL.
- [ ] In `handleDiffNav` (`diffnav.go:420`), extract count before switch:

```go
count := max(1, m.vim.pendingCount)
m.vim.pendingCount = 0
```

- [ ] For `ActionDown`: loop `for i := 0; i < count; i++ { m.moveDiffCursorDown() }` then single `m.syncViewportToCursor()`. Bail if cursor doesn't advance (already at end).
- [ ] Mirror for `ActionUp`.
- [ ] Run tests — PASS.
- [ ] Commit: `feat(ui): support count prefix for j/k line motion`.

### Task 4: Count-aware page / half-page / scroll

**Files:** Modify `app/ui/diffnav.go`, extend `app/ui/diffnav_test.go`.

- [ ] Write failing tests:
  - `TestModel_CountPageDown_3`: in a 200-line file, `3`,`pgdown` → cursor moves ≈ 3 viewport heights.
  - `TestModel_CountHalfPageDown_2`: same setup, `2`,`ctrl+d` → cursor moves ≈ 1 viewport height.
  - `TestModel_CountScrollRight_5`: `5`,`right` → `scrollX == 5*scrollStep`.
- [ ] Run tests — FAIL.
- [ ] In `handleDiffNav`, apply count loop to: `ActionPageDown`, `ActionPageUp`, `ActionHalfPageDown`, `ActionHalfPageUp`, `ActionScrollLeft`, `ActionScrollRight`.
- [ ] Run tests — PASS.
- [ ] Commit: `feat(ui): support count prefix for page/scroll motions`.

### Task 5: Extend `FileTree.Move` and `TOC.Move` for step motions with count

**Files:** Modify `app/ui/sidepane/filetree.go`, `app/ui/sidepane/toc.go`; extend `app/ui/sidepane/filetree_test.go`, `app/ui/sidepane/toc_test.go`.

- [ ] Write failing tests:
  - `TestFileTree_Move_MotionDown_Count5`: build a tree with 10 files, `Move(MotionDown, 5)` from first file → cursor on sixth file (delta 5 in file order).
  - `TestFileTree_Move_MotionUp_Count3`: similar in reverse.
  - `TestTOC_Move_MotionDown_Count2`.
  - Existing `Move(MotionDown)` single-step tests must remain and pass.
- [ ] Run tests — FAIL.
- [ ] In `filetree.go:119` `Move`: add `count[0]`-loop handling for `MotionUp`/`MotionDown`:

```go
case MotionUp:
    n := 1
    if len(count) > 0 && count[0] > 1 {
        n = count[0]
    }
    for i := 0; i < n; i++ {
        prev := ft.cursor
        ft.moveUp()
        if ft.cursor == prev {
            break
        }
    }
case MotionDown:
    // mirror
```

- [ ] Mirror in `toc.go:124`.
- [ ] Update interface doc comments in `app/ui/model.go` on `FileTreeComponent.Move` and `TOCComponent.Move` to note that step motions now use count.
- [ ] Run tests — PASS.
- [ ] Commit: `feat(sidepane): support count on Move step motions`.

### Task 6: Count-aware tree / TOC navigation

**Files:** Modify `app/ui/diffnav.go` (`handleTreeNav`, `handleTOCNav`), extend `app/ui/diffnav_test.go`.

- [ ] Write failing tests:
  - `TestModel_TreeNav_CountDown_3j`: tree pane focused, 10 files, `3`,`j` → tree cursor 3 files down.
- [ ] Run tests — FAIL.
- [ ] In `handleTreeNav` and `handleTOCNav`: extract count, pass to `m.tree.Move(sidepane.MotionDown, count)` / `...Up, count)`. Clear `m.vim.pendingCount` at function top.
- [ ] Run tests — PASS.
- [ ] Commit: `feat(ui): support count prefix in tree/TOC navigation`.

### Task 7: Count-aware hunk navigation (`[` / `]`)

**Files:** Modify `app/ui/diffnav.go` (`handleHunkNav`), modify `app/ui/model.go` (dispatch), extend `app/ui/diffnav_test.go`.

- [ ] Write failing test `TestModel_CountHunkNav_3`: file with 5 hunks, press `3`,`]` → cursor at first line of 4th hunk (3 jumps forward).
- [ ] Run — FAIL.
- [ ] Extract count in `handleHunkNav(forward bool)`; loop N times calling `moveToNextHunk`/`moveToPrevHunk`, break on no-move (boundary). Cross-file boundary triggers only on the final iteration (when local hunks exhausted). For simplicity, count hunk jumps within the current file only — if boundary reached mid-count, do the single cross-file jump and stop.
- [ ] Run — PASS.
- [ ] Commit: `feat(ui): support count prefix for hunk navigation`.

### Task 8: Count-aware next/prev item (`n` / `p` / `N`)

**Files:** Modify `app/ui/handlers.go` (`handleFileOrSearchNav`), extend `app/ui/handlers_test.go`.

- [ ] Write failing test `TestModel_CountFileNav_2n`: 5 files, `2`,`n` → second file loaded.
- [ ] Run — FAIL.
- [ ] In `handleFileOrSearchNav`: extract count, loop over `tree.StepFile` (or `nextSearchMatch`/`prevSearchMatch` in search mode). For file-step, break if `HasFile(dir)` is false.
- [ ] Run — PASS.
- [ ] Commit: `feat(ui): support count prefix for next/prev item`.

### Task 9: `g` chord (`gg` → top) + `G` → end binding

**Files:** Modify `app/keymap/keymap.go` (add `G` to defaults), extend `app/ui/viminput.go` (chord state), extend `app/ui/viminput_test.go`, extend `app/keymap/keymap_test.go`.

- [ ] Write failing tests:
  - `TestConsumeVimPrefix_GChordStart`: press `g` → `pendingChord=="g"`, handled=true.
  - `TestConsumeVimPrefix_GGDispatch`: with `pendingChord=="g"`, press `g` → dispatches ActionHome, clears chord.
  - `TestConsumeVimPrefix_GChordReDispatch`: with `pendingChord=="g"`, press `j` → clears chord, handled=false (caller dispatches j).
  - `TestKeymap_G_EndBinding`: after `Default()`, `Resolve("G") == ActionEnd`.
- [ ] Run — FAIL.
- [ ] In `keymap.go` `defaultBindings()`: add `"G": ActionEnd,`.
- [ ] Extend `consumeVimPrefix` to handle chord state:
  - If `pendingChord == "g"`: second key `"g"` → clear chord and count, invoke a new helper `dispatchVimAction(keymap.ActionHome)` that calls the same code path as pressing the existing Home key (extract shared helper `m.dispatchAction(action keymap.Action)` if needed).
  - If `pendingChord == "g"` and key is neither `g` nor a digit/chord-starter: clear `pendingChord`, return handled=false (fall through).
  - If key is `"g"` and `pendingChord == ""`: set `pendingChord="g"`, handled=true.
- [ ] Run — PASS.
- [ ] Commit: `feat(ui): add gg chord and G key binding for top/bottom jumps`.

### Task 10: `z` chord (`zz`/`zt`/`zb`) + `bottomAlignViewportOnCursor`

**Files:** Modify `app/ui/diffnav.go` (new helper), extend `app/ui/viminput.go`, extend `app/ui/viminput_test.go`, extend `app/ui/diffnav_test.go`.

- [ ] Write failing tests:
  - `TestBottomAlignViewportOnCursor`: cursor at line 20, viewport height 10 → `YOffset==11`.
  - `TestConsumeVimPrefix_ZZDispatch`: press `z`,`z` → viewport centers on cursor.
  - `TestConsumeVimPrefix_ZTDispatch`: press `z`,`t` → `topAlignViewportOnCursor` applied.
  - `TestConsumeVimPrefix_ZBDispatch`: press `z`,`b` → bottom-align applied.
  - `TestConsumeVimPrefix_ZChordReDispatch`: `z` then `j` → clears chord, handled=false.
- [ ] Run — FAIL.
- [ ] Add `bottomAlignViewportOnCursor` to `diffnav.go` (per spec, at §Viewport bottom-align).
- [ ] Extend `consumeVimPrefix` for `z` chord (z/t/b completers call respective viewport helpers; anything else clears chord and falls through).
- [ ] Run — PASS.
- [ ] Commit: `feat(ui): add z chord for viewport alignment (zz/zt/zb)`.

### Task 11: `ctrl+w` chord (pane nav)

**Files:** Extend `app/ui/viminput.go`, extend `app/ui/viminput_test.go`.

- [ ] Write failing tests:
  - `TestConsumeVimPrefix_CtrlW_H_FocusTree`: press `ctrl+w`,`h` → `focus==paneTree`.
  - `TestConsumeVimPrefix_CtrlW_L_FocusDiff`: `ctrl+w`,`l` → `focus==paneDiff`.
  - `TestConsumeVimPrefix_CtrlW_W_TogglePane`: `ctrl+w`,`w` → pane toggled.
  - `TestConsumeVimPrefix_CtrlW_J_FocusDiff`: `ctrl+w`,`j` → `focus==paneDiff`.
  - `TestConsumeVimPrefix_CtrlW_K_FocusTree`: `ctrl+w`,`k` → `focus==paneTree`.
  - `TestConsumeVimPrefix_CtrlW_Invalid_Swallowed`: `ctrl+w`,`x` → chord clears, handled=true (swallowed — per spec, Ctrl-W does NOT re-dispatch).
- [ ] Run — FAIL.
- [ ] Extend `consumeVimPrefix`: if key is `ctrl+w` and no pending chord → set `pendingChord="ctrl+w"`, handled=true. If pending chord is `ctrl+w`: dispatch based on second key; any unmatched second key still returns handled=true (swallow).
- [ ] Run — PASS.
- [ ] Commit: `feat(ui): add Ctrl-W chord for pane navigation`.

### Task 12: Hunk aliases `{` / `}`

**Files:** Modify `app/keymap/keymap.go`, extend `app/keymap/keymap_test.go`.

- [ ] Write failing tests:
  - `TestKeymap_CurlyBraceHunks`: `Resolve("{") == ActionPrevHunk`, `Resolve("}") == ActionNextHunk`.
- [ ] Run — FAIL.
- [ ] Add `"{": ActionPrevHunk,` and `"}": ActionNextHunk,` to `defaultBindings()`.
- [ ] Run — PASS.
- [ ] Commit: `feat(keymap): add { and } as hunk navigation aliases`.

### Task 13: Status-bar pending indicator

**Files:** Modify `app/ui/view.go`, extend `app/ui/view_test.go`.

- [ ] Write failing tests:
  - `TestStatusBar_PendingCount`: `pendingCount=5` → status-bar string contains `"5"` in the right-parts section.
  - `TestStatusBar_PendingChord_G`: `pendingChord="g"` → contains `"g"`.
  - `TestStatusBar_PendingChord_CtrlW`: `pendingChord="ctrl+w"` → contains `"^W"`.
  - `TestStatusBar_NoPending`: both zero → no pending segment emitted.
- [ ] Run — FAIL.
- [ ] Add `vimPendingSegment()` method on Model returning the formatted string (or empty), wrapped with `style.AnsiFg(resolver.Color(style.ColorKeyAccentFg))`.
- [ ] In `statusBarText()`, prepend the segment to `rightParts` when non-empty.
- [ ] Run — PASS.
- [ ] Commit: `feat(ui): display pending vim count/chord in status bar`.

### Task 14: Help overlay — Vim section

**Files:** Modify `app/ui/handlers.go` (`buildHelpSpec`) OR `app/keymap/keymap.go` (prefer extending buildHelpSpec to avoid forcing chord-only entries into `keymap.Action`). Extend `app/ui/handlers_test.go`.

- [ ] Write failing test `TestBuildHelpSpec_VimSection`: the help spec contains a section titled `"Vim"` with entries for count prefix, `gg`, `G`, `zz`/`zt`/`zb`, `{`/`}`, `Ctrl-W h/l/w/j/k`.
- [ ] Run — FAIL.
- [ ] Extend `buildHelpSpec()` to append a fixed `overlay.HelpSection{Title: "Vim", Entries: [...]}` at the end:

```go
vim := overlay.HelpSection{Title: "Vim", Entries: []overlay.HelpEntry{
    {Keys: "N", Description: "count prefix: N × next motion (e.g. 5j, 3])"},
    {Keys: "gg", Description: "jump to file top"},
    {Keys: "G", Description: "jump to file bottom"},
    {Keys: "zz / zt / zb", Description: "center / top-align / bottom-align cursor in viewport"},
    {Keys: "{ / }", Description: "previous / next hunk (aliases for [ / ])"},
    {Keys: "Ctrl+W h/l/w/j/k", Description: "focus tree / diff / toggle / diff / tree"},
}}
result = append(result, vim)
```

- [ ] Run — PASS.
- [ ] Commit: `feat(ui): add Vim section to help overlay`.

### Task 15: Regression tests for backward compat

**Files:** Create `app/ui/viminput_regression_test.go`.

- [ ] Write tests:
  - `TestRegression_UnprefixedJ_OneLine`: press `j` with no prefix → cursor moves exactly 1 line (baseline).
  - `TestRegression_NonChordKey_Dispatches`: press `?` → help overlay opens, no state mutation on `m.vim`.
  - `TestRegression_DigitsInSearch`: activate search, press `5` → search query has `"5"`, `pendingCount==0`.
  - `TestRegression_DigitsInAnnotation`: start annotation, press `5` → annotation input has `"5"`, `pendingCount==0`.
- [ ] Run — PASS (should pass on first run; if any fail, the earlier tasks introduced a regression).
- [ ] Run full `make test` — all existing tests plus new ones must pass.
- [ ] Run `make lint` — no new warnings.
- [ ] Commit: `test(ui): backward-compat regression coverage for vim prefix`.

### Task 16: Documentation

**Files:** Modify `README.md`, `site/docs.html`, `.claude-plugin/skills/revdiff/references/usage.md`.

- [ ] In `README.md`: add a "Vim-style controls" subsection under the key bindings section covering count prefix, `gg`/`G`, `zz`/`zt`/`zb`, `{`/`}`, `Ctrl-W h/l/w/j/k`.
- [ ] Mirror the content in `site/docs.html` (find the key bindings docs block).
- [ ] Mirror in `.claude-plugin/skills/revdiff/references/usage.md`.
- [ ] Run `make test` (sanity) and view the README rendered.
- [ ] Commit: `docs: document vim-style controls`.

### Task 17: Final verification

- [ ] Run `make test` — must be fully green.
- [ ] Run `make lint` — no issues.
- [ ] Run `make install` and manually sanity-check `revdiff --help` (not required to exercise keybindings, just verify the binary builds and runs).
- [ ] Move this plan to `docs/plans/completed/2026-04-17-vim-controls.md` in the same commit as the doc updates, or in a dedicated `chore: move vim-controls plan to completed` commit.
