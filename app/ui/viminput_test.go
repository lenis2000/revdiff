package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModel_ClearVimPrefix(t *testing.T) {
	m := Model{}
	m.vim.pendingCount = 42
	m.vim.pendingChord = "g"

	m.clearVimPrefix()

	assert.Equal(t, 0, m.vim.pendingCount)
	assert.Equal(t, "", m.vim.pendingChord)
}
