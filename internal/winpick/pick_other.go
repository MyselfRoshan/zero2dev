//go:build !windows

package winpick

const supported = false

func pickFolder(title, start string) (string, bool, error) { return "", false, ErrUnsupported }
