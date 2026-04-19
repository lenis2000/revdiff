// Package clipboard writes strings to the OS clipboard. It wraps
// atotto/clipboard so OS-level boundary code stays out of app/ui. The
// backend uses pbcopy on darwin and xclip/xsel on linux; availability
// depends on the host. WriteAll returns the underlying error when the
// helper binary is missing or fails, letting the caller surface a
// user-visible hint.
package clipboard

import "github.com/atotto/clipboard"

// Writer copies text to the OS clipboard. Stateless; the type exists
// to group behavior as methods per project convention.
type Writer struct{}

// WriteAll copies s to the OS clipboard.
func (Writer) WriteAll(s string) error {
	return clipboard.WriteAll(s)
}
