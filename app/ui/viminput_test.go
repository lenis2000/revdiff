package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/umputun/revdiff/app/ui/overlay"
)

func TestModel_ClearVimPrefix(t *testing.T) {
	m := Model{}
	m.vim.pendingCount = 42
	m.vim.pendingChord = "g"

	m.clearVimPrefix()

	assert.Equal(t, 0, m.vim.pendingCount)
	assert.Equal(t, "", m.vim.pendingChord)
}

// keyMsg returns a tea.KeyMsg matching what bubbletea produces for the given single rune.
func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestConsumeVimPrefix_SingleDigit(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	handled, m2, _ := m.consumeVimPrefix(keyMsg('5'))

	assert.True(t, handled)
	assert.Equal(t, 5, m2.vim.pendingCount)
}

func TestConsumeVimPrefix_MultiDigit(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	_, m, _ = m.consumeVimPrefix(keyMsg('1'))
	_, m, _ = m.consumeVimPrefix(keyMsg('0'))

	assert.Equal(t, 10, m.vim.pendingCount)
}

func TestConsumeVimPrefix_LeadingZeroFallsThrough(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	handled, m2, _ := m.consumeVimPrefix(keyMsg('0'))

	assert.False(t, handled, "bare 0 should fall through (no count to extend)")
	assert.Equal(t, 0, m2.vim.pendingCount)
}

func TestConsumeVimPrefix_ZeroExtendsExistingCount(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.vim.pendingCount = 5

	handled, m2, _ := m.consumeVimPrefix(keyMsg('0'))

	assert.True(t, handled)
	assert.Equal(t, 50, m2.vim.pendingCount)
}

func TestConsumeVimPrefix_CapAt10000(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	for i := 0; i < 8; i++ {
		_, m, _ = m.consumeVimPrefix(keyMsg('1'))
	}

	assert.Equal(t, 10000, m.vim.pendingCount)
}

func TestConsumeVimPrefix_ModalShortCircuit_Search(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.search.active = true

	handled, m2, _ := m.consumeVimPrefix(keyMsg('5'))

	assert.False(t, handled)
	assert.Equal(t, 0, m2.vim.pendingCount, "must not absorb digits in search mode")
}

func TestConsumeVimPrefix_ModalShortCircuit_Annotation(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.annot.annotating = true

	handled, m2, _ := m.consumeVimPrefix(keyMsg('5'))

	assert.False(t, handled)
	assert.Equal(t, 0, m2.vim.pendingCount)
}

func TestConsumeVimPrefix_ModalShortCircuit_DiscardConfirm(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.inConfirmDiscard = true

	handled, m2, _ := m.consumeVimPrefix(keyMsg('5'))

	assert.False(t, handled)
	assert.Equal(t, 0, m2.vim.pendingCount)
}

func TestConsumeVimPrefix_EscClearsPending(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.vim.pendingCount = 5
	m.vim.pendingChord = "g"

	handled, m2, _ := m.consumeVimPrefix(tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, handled, "Esc must fall through to existing handler")
	assert.Equal(t, 0, m2.vim.pendingCount)
	assert.Equal(t, "", m2.vim.pendingChord)
}

func TestConsumeVimPrefix_NonDigitFallsThrough(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	handled, m2, _ := m.consumeVimPrefix(keyMsg('j'))

	assert.False(t, handled, "non-digit, non-chord keys must fall through")
	assert.Equal(t, 0, m2.vim.pendingCount)
}

func TestConsumeVimPrefix_NonDigitPreservesPendingCount(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.vim.pendingCount = 5

	_, m2, _ := m.consumeVimPrefix(keyMsg('j'))

	assert.Equal(t, 5, m2.vim.pendingCount, "count must survive into dispatch path; caller clears after consuming")
}

func TestConsumeVimPrefix_GChordStart(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	handled, m2, _ := m.consumeVimPrefix(keyMsg('g'))

	assert.True(t, handled)
	assert.Equal(t, "g", m2.vim.pendingChord)
}

func TestConsumeVimPrefix_GChord_NonGSecondKey_ClearsAndFallsThrough(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.vim.pendingChord = "g"
	m.vim.pendingCount = 5

	handled, m2, _ := m.consumeVimPrefix(keyMsg('j'))

	assert.False(t, handled, "g+j must clear chord and fall through")
	assert.Equal(t, "", m2.vim.pendingChord)
	assert.Equal(t, 5, m2.vim.pendingCount, "count must survive chord clear")
}

func TestVimPendingSegment_Empty(t *testing.T) {
	m := Model{}
	m.cfg.noColors = true
	assert.Empty(t, m.vimPendingSegment())
}

func TestVimPendingSegment_CountOnly(t *testing.T) {
	m := Model{}
	m.cfg.noColors = true
	m.vim.pendingCount = 5
	assert.Equal(t, "5", m.vimPendingSegment())
}

func TestVimPendingSegment_ChordOnly_G(t *testing.T) {
	m := Model{}
	m.cfg.noColors = true
	m.vim.pendingChord = "g"
	assert.Equal(t, "g", m.vimPendingSegment())
}

func TestVimPendingSegment_ChordOnly_CtrlW(t *testing.T) {
	m := Model{}
	m.cfg.noColors = true
	m.vim.pendingChord = "ctrl+w"
	assert.Equal(t, "^W", m.vimPendingSegment())
}

func TestVimPendingSegment_CountAndChord(t *testing.T) {
	// reachable in theory if a future chord supports count; not in scope B today.
	m := Model{}
	m.cfg.noColors = true
	m.vim.pendingCount = 5
	m.vim.pendingChord = "z"
	assert.Equal(t, "5z", m.vimPendingSegment())
}
