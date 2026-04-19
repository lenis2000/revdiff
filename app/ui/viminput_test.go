package ui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/overlay"
)

// fakeClipboard records the last payload handed to WriteAll and can be wired
// to return an error on demand.
type fakeClipboard struct {
	payload string
	calls   int
	err     error
}

func (f *fakeClipboard) WriteAll(s string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.payload = s
	return nil
}

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

// yankTestModel returns a Model wired with the given diff lines, a fake
// clipboard, focus on the diff pane, and the cursor at lineIdx.
func yankTestModel(lines []diff.DiffLine, lineIdx int) (Model, *fakeClipboard) {
	cb := &fakeClipboard{}
	m := Model{overlay: overlay.NewManager(), clipboard: cb}
	m.file.lines = lines
	m.nav.diffCursor = lineIdx
	m.layout.focus = paneDiff
	return m, cb
}

func TestConsumeVimPrefix_YChordStart(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}

	handled, m2, _ := m.consumeVimPrefix(keyMsg('y'))

	assert.True(t, handled)
	assert.Equal(t, "y", m2.vim.pendingChord)
}

func TestYY_CopiesCurrentLine(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeContext, Content: "ctx line"},
		{ChangeType: diff.ChangeAdd, Content: "added line"},
		{ChangeType: diff.ChangeRemove, Content: "removed line"},
	}
	m, cb := yankTestModel(lines, 1)

	// first y primes the chord
	handled, m, _ := m.consumeVimPrefix(keyMsg('y'))
	require.True(t, handled)
	// second y completes yy
	handled, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.True(t, handled)
	assert.Equal(t, 1, cb.calls)
	assert.Equal(t, "added line", cb.payload)
	assert.Equal(t, "yanked line", m.commits.hint)
	assert.Equal(t, 0, m.vim.pendingCount)
	assert.Equal(t, "", m.vim.pendingChord)
}

func TestYY_CountYanksMultipleLines(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeContext, Content: "one"},
		{ChangeType: diff.ChangeAdd, Content: "two"},
		{ChangeType: diff.ChangeRemove, Content: "three"},
		{ChangeType: diff.ChangeContext, Content: "four"},
	}
	m, cb := yankTestModel(lines, 0)
	m.vim.pendingCount = 3

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, "one\ntwo\nthree", cb.payload)
	assert.Equal(t, "yanked 3 lines", m.commits.hint)
}

func TestYY_CountPastEndYanksWhatsAvailable(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeContext, Content: "one"},
		{ChangeType: diff.ChangeAdd, Content: "two"},
	}
	m, cb := yankTestModel(lines, 0)
	m.vim.pendingCount = 10

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, "one\ntwo", cb.payload)
	assert.Equal(t, "yanked 2 lines", m.commits.hint)
}

func TestYY_SkipsDividersBinaryAndPlaceholders(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeContext, Content: "keep-1"},
		{ChangeType: diff.ChangeDivider, Content: "..."},
		{ChangeType: diff.ChangeContext, Content: "binary", IsBinary: true},
		{ChangeType: diff.ChangeContext, Content: "placeholder", IsPlaceholder: true},
		{ChangeType: diff.ChangeAdd, Content: "keep-2"},
	}
	m, cb := yankTestModel(lines, 0)
	m.vim.pendingCount = 5

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, "keep-1\nkeep-2", cb.payload)
}

func TestYY_OnDividerOnlyIsNoOp(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeDivider, Content: "..."},
	}
	m, cb := yankTestModel(lines, 0)

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, 0, cb.calls)
	assert.Equal(t, "nothing to yank", m.commits.hint)
}

func TestYY_OnAnnotationRowIsNoOp(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeAdd, Content: "should not leak"},
	}
	m, cb := yankTestModel(lines, 0)
	m.annot.cursorOnAnnotation = true

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, 0, cb.calls)
	assert.Empty(t, m.commits.hint, "annotation-row yank is silent")
}

func TestYY_TreePaneIsNoOp(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeAdd, Content: "x"},
	}
	m, cb := yankTestModel(lines, 0)
	m.layout.focus = paneTree

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, 0, cb.calls)
	assert.Empty(t, m.commits.hint)
}

func TestYY_ClipboardErrorSurfacesHint(t *testing.T) {
	lines := []diff.DiffLine{
		{ChangeType: diff.ChangeAdd, Content: "x"},
	}
	m, cb := yankTestModel(lines, 0)
	cb.err = errors.New("xclip: not installed")

	_, m, _ = m.consumeVimPrefix(keyMsg('y'))
	_, m, _ = m.consumeVimPrefix(keyMsg('y'))

	assert.Equal(t, 1, cb.calls)
	assert.Contains(t, m.commits.hint, "clipboard error")
}

func TestYY_YNonY_ClearsChordAndFallsThrough(t *testing.T) {
	m := Model{overlay: overlay.NewManager()}
	m.vim.pendingChord = "y"
	m.vim.pendingCount = 5

	handled, m2, _ := m.consumeVimPrefix(keyMsg('j'))

	assert.False(t, handled, "y+j must clear chord and fall through")
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
