//go:build windows

package winpick

// pickFile shows IFileOpenDialog / IFileSaveDialog. Wave 0 stub: the
// db-backend work replaces it with the real dialog (see pickFolder).
func pickFile(FileOptions) (string, bool, error) { return "", false, ErrUnsupported }
