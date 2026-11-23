package supervisor

import (
	"os"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
)

// ComposerCommand is the command that runs the installed Composer for a PHP service: the
// service's own PHP binary (the default PHP for a service that is not a PHP one) and
// [composer.phar] (the default Composer runtime, else the first one). A one-off task whose program is "composer" runs this, with the service's environment, so
// Composer always uses the PHP of the project (the composer.bat launcher would take the first
// php on PATH). It is a 409 when no Composer runtime is installed or the service has no PHP to run it.
func (s *Supervisor) ComposerCommand(svc model.Service) (bin string, args []string, err error) {
	missing := apperr.Conflict("Composer is not installed: install it on the Runtimes page")
	d := s.st.Snapshot()
	var pick *model.Runtime
	for i := range d.Runtimes {
		r := &d.Runtimes[i]
		if r.Kind != "composer" || r.Binary == "" {
			continue
		}
		if st, e := os.Stat(model.ComposerPhar(*r)); e != nil || st.IsDir() {
			continue // registered, but its file is gone
		}
		if pick == nil || (r.Default && !pick.Default) {
			pick = r
		}
	}
	if pick == nil {
		return "", nil, missing
	}
	if svc.Kind != "php" { // a service of another kind (a custom command) runs it with the default PHP
		svc.Kind, svc.RuntimeID = "php", ""
	}
	_, php, err := s.RuntimeFor(svc)
	if err != nil {
		return "", nil, err
	}
	return php, []string{model.ComposerPhar(*pick)}, nil
}
