package ui

import (
	tea "github.com/charmbracelet/bubbletea"
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

	return false, m, nil
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
