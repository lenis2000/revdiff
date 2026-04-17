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
- Word-motion (`w`, `b`, `e`) — conflicts with `w`/`W` toggles and narrow payoff in a diff viewer.
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

- `app/ui/viminput_test.go`: count accumulation, chord transitions, chord re-dispatch, `Esc` clearing, modal short-circuit, max-count cap.
- Extend `app/ui/diffnav_test.go`: verify `5j`/`10k`/`3]`/`{` move cursor N steps.
- Extend `app/ui/handlers_test.go` or similar: `gg`/`G` jump targets, `zz`/`zt`/`zb` viewport offsets, `Ctrl-W` pane focus transitions.
- Extend `app/ui/sidepane/filetree_test.go` and `toc_test.go`: `Move(MotionDown, 5)` steps 5 entries.
- `app/ui/view_test.go`: pending-indicator segment renders for count-only, chord-only, and combined states; absent when both empty.

### Documentation

- `README.md`: new "Vim-style controls" subsection under the key bindings section.
- `site/docs.html`: mirror the README additions.
- `app/keymap/keymap.go` help section text: add descriptions for the new actions so `--dump-keys` and the help overlay list them.
- `.claude-plugin/skills/revdiff/references/usage.md`: document the vim keys.

## Risks / Open Questions
- **None at this stage.** Scope and key assignments are settled. The only runtime decision is the muted-color choice for the status-bar indicator, which is an implementation detail and will be resolved during build.
