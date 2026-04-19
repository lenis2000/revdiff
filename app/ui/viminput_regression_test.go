package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/umputun/revdiff/app/diff"
)

// TestRegression_UnprefixedJ_OneLine asserts the baseline contract that pressing
// `j` with no prefix moves the cursor exactly one line — same as before vim
// prefix support was introduced.
func TestRegression_UnprefixedJ_OneLine(t *testing.T) {
	lines := makeContextFile(10)
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.layout.focus = paneDiff
	result, _ := m.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model := result.(Model)
	assert.Equal(t, 0, model.nav.diffCursor)

	result, _ = model.Update(keyMsg('j'))
	model = result.(Model)
	assert.Equal(t, 1, model.nav.diffCursor, "single j should move exactly one line (no count side effect)")
	assert.Equal(t, 0, model.vim.pendingCount)
	assert.Equal(t, "", model.vim.pendingChord)
}

// TestRegression_NonChordKey_NoVimStateMutation asserts that a regular keybinding
// (here `?` for help) dispatches without leaving any vim prefix state behind.
func TestRegression_NonChordKey_NoVimStateMutation(t *testing.T) {
	m := testModel([]string{"a.go"}, nil)
	m.tree = testNewFileTree([]string{"a.go"})
	m.file.name = "a.go"

	result, _ := m.Update(keyMsg('?'))
	model := result.(Model)
	assert.True(t, model.overlay.Active(), "? should open the help overlay")
	assert.Equal(t, 0, model.vim.pendingCount, "no vim count state should leak from non-chord key")
	assert.Equal(t, "", model.vim.pendingChord)
}

// TestRegression_DigitsInSearch verifies digits typed during search input go to
// the search textinput and do NOT accumulate as a vim count.
func TestRegression_DigitsInSearch(t *testing.T) {
	lines := makeContextFile(5)
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.layout.focus = paneDiff
	result, _ := m.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model := result.(Model)

	// open search
	result, _ = model.Update(keyMsg('/'))
	model = result.(Model)
	assert.True(t, model.search.active)

	// type "5" — should reach the search textinput, not vim prefix
	result, _ = model.Update(keyMsg('5'))
	model = result.(Model)
	assert.Equal(t, 0, model.vim.pendingCount, "digits in search must not absorb into vim count")
	assert.Equal(t, "5", model.search.input.Value(), "digit should land in the search textinput")
}

// TestRegression_DigitsInAnnotation verifies digits typed during annotation
// input go to the annotation textinput and do NOT accumulate as a vim count.
func TestRegression_DigitsInAnnotation(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "ctx", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "added", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.layout.focus = paneDiff
	result, _ := m.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model := result.(Model)
	model.nav.diffCursor = 1 // on the changed line

	// start annotation with `a`
	result, _ = model.Update(keyMsg('a'))
	model = result.(Model)
	assert.True(t, model.annot.annotating, "a should start annotation input")

	// type "5" — should reach annotation textinput
	result, _ = model.Update(keyMsg('5'))
	model = result.(Model)
	assert.Equal(t, 0, model.vim.pendingCount, "digits in annotation must not absorb into vim count")
	assert.Equal(t, "5", model.annot.input.Value(), "digit should land in the annotation textinput")
}

// TestRegression_EscDoesNotShadowSearchClear verifies Esc still clears search
// matches when no vim prefix is pending (existing behavior preserved).
func TestRegression_EscDoesNotShadowSearchClear(t *testing.T) {
	lines := []diff.DiffLine{
		{NewNum: 1, Content: "match", ChangeType: diff.ChangeContext},
		{NewNum: 2, Content: "other", ChangeType: diff.ChangeContext},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.tree = testNewFileTree([]string{"a.go"})
	m.layout.focus = paneDiff
	result, _ := m.Update(fileLoadedMsg{file: "a.go", lines: lines})
	model := result.(Model)
	model.search.matches = []int{0} // simulate active matches
	model.search.term = "match"

	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = result.(Model)
	assert.Empty(t, model.search.matches, "Esc must still clear search matches when no vim prefix is pending")
}
