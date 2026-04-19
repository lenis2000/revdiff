package ui

// clearVimPrefix resets both fields of m.vim to their zero values.
// Called when entering a modal mode, on Esc, and after a chord completes.
func (m *Model) clearVimPrefix() {
	m.vim.pendingCount = 0
	m.vim.pendingChord = ""
}
