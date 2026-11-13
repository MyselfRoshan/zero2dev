//go:build !windows

package tray

// Start reports ErrUnsupported: there is no tray outside Windows; Zero2Dev
// runs in a terminal there.
func Start(m Menu) (Icon, error) { return nil, ErrUnsupported }
