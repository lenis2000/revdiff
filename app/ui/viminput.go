package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/umputun/revdiff/app/ui/sidepane"
)

// vimCountCap saturates pendingCount to prevent runaway repetition (e.g. user
// typing "999999j" should not loop millions of times).
const vimCountCap = 10000

// clearVimPrefix resets both fields of m.vim to their zero values.
// Called when entering a modal mode, on Esc, and after a chord completes.
func (m *Model) clearVimPrefix() {
	m.vim.pendingCount = 0
	m.vim.pendingChord = ""
}

// consumeVimPrefix processes a key through the vim-style prefix state machine.
// Returns handled=true when the key was absorbed (digit accumulation, chord
// start, or chord completion that dispatched an action). Returns handled=false
// when the caller should fall through to normal action dispatch.
//
// In modal modes (annotation input, search input, overlay open, discard
// confirm) this is a no-op — digits and other keys must reach the relevant
// modal handler untouched.
//
// On Esc, clears any pending prefix state and falls through so the existing
// Esc handler (search-clear, overlay-close) still runs.
func (m Model) consumeVimPrefix(msg tea.KeyMsg) (handled bool, model Model, cmd tea.Cmd) {
	if m.annot.annotating || m.search.active || m.inConfirmDiscard {
		return false, m, nil
	}
	if m.overlay != nil && m.overlay.Active() {
		return false, m, nil
	}

	if msg.Type == tea.KeyEsc {
		m.clearVimPrefix()
		return false, m, nil
	}

	if m.vim.pendingChord != "" {
		return m.handleVimChordSecondKey(msg)
	}

	if d, ok := digitFromKey(msg); ok {
		if d == 0 && m.vim.pendingCount == 0 {
			return false, m, nil
		}
		next := m.vim.pendingCount*10 + d
		if next > vimCountCap {
			next = vimCountCap
		}
		m.vim.pendingCount = next
		return true, m, nil
	}

	if isChordStarter(msg) {
		m.vim.pendingChord = chordKeyName(msg)
		return true, m, nil
	}

	return false, m, nil
}

// isChordStarter reports whether msg starts a vim-style chord (g, z, ctrl+w).
func isChordStarter(msg tea.KeyMsg) bool {
	return chordKeyName(msg) != ""
}

// chordKeyName returns the canonical chord identifier ("g", "z", "ctrl+w") for
// a starter key, or "" if not a chord starter.
func chordKeyName(msg tea.KeyMsg) string {
	if msg.Type == tea.KeyCtrlW {
		return "ctrl+w"
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'g':
			return "g"
		case 'z':
			return "z"
		}
	}
	return ""
}

// handleVimChordSecondKey processes the second key of a chord. If the key
// completes a known chord, the chord's action is dispatched and state
// cleared. Otherwise the chord is cleared and the caller falls through
// (handled=false) so the second key dispatches normally.
func (m Model) handleVimChordSecondKey(msg tea.KeyMsg) (handled bool, model Model, cmd tea.Cmd) {
	chord := m.vim.pendingChord
	keyRune := chordSecondRune(msg)

	switch chord {
	case "g":
		if keyRune == 'g' {
			m.clearVimPrefix()
			return m.dispatchVimGotoStart()
		}
	case "z":
		// viewport-alignment chords are diff-pane only; no-op outside.
		if m.layout.focus == paneDiff {
			switch keyRune {
			case 'z':
				m.clearVimPrefix()
				m.centerViewportOnCursor()
				return true, m, nil
			case 't':
				m.clearVimPrefix()
				m.topAlignViewportOnCursor()
				return true, m, nil
			case 'b':
				m.clearVimPrefix()
				m.bottomAlignViewportOnCursor()
				return true, m, nil
			}
		}
	}

	// chord did not complete: clear chord (count preserved) and fall through.
	m.vim.pendingChord = ""
	return false, m, nil
}

// chordSecondRune returns the rune of a single-rune key, or 0 otherwise.
func chordSecondRune(msg tea.KeyMsg) rune {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0
	}
	return msg.Runes[0]
}

// dispatchVimGotoStart implements the gg chord: go to top of file in the diff
// pane, or first file/TOC entry in the tree pane. Mirrors the Home action's
// per-pane semantics so the chord behaves identically to pressing Home.
func (m Model) dispatchVimGotoStart() (handled bool, model Model, cmd tea.Cmd) {
	if m.layout.focus == paneDiff {
		m.moveDiffCursorToStart()
		return true, m, nil
	}
	if m.file.mdTOC != nil {
		m.file.mdTOC.Move(sidepane.MotionFirst)
		m.file.mdTOC.EnsureVisible(m.treePageSize())
		m.syncDiffToTOCCursor()
		return true, m, nil
	}
	m.tree.Move(sidepane.MotionFirst)
	m.tree.EnsureVisible(m.treePageSize())
	result, cmd := m.loadSelectedIfChanged()
	if rm, ok := result.(Model); ok {
		return true, rm, cmd
	}
	return true, m, cmd
}


// digitFromKey returns the numeric value (0-9) of a single-rune digit key, or false otherwise.
func digitFromKey(msg tea.KeyMsg) (int, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if r < '0' || r > '9' {
		return 0, false
	}
	return int(r - '0'), true
}

// vimCount returns the effective count for a motion: the pending count when
// non-zero, otherwise 1 (a single motion). Centralizes the "0 means 1" rule
// so handlers don't open-code it.
func vimCount(pending int) int {
	if pending < 1 {
		return 1
	}
	return pending
}

// repeatCursorMove invokes step n times, bailing out early if the move makes
// no progress (cursor would otherwise be stuck at a boundary). Used by
// count-aware diff cursor motions to avoid spinning when the user types a
// large count near the file edge.
func (m *Model) repeatCursorMove(n int, step func(*Model)) {
	for i := 0; i < n; i++ {
		prev := m.nav.diffCursor
		prevOnAnnot := m.annot.cursorOnAnnotation
		step(m)
		if m.nav.diffCursor == prev && m.annot.cursorOnAnnotation == prevOnAnnot {
			return
		}
	}
}
