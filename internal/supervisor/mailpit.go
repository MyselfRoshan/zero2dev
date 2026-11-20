package supervisor

import (
	"strconv"

	"zero2dev/internal/apperr"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
)

// healthPath is the page the health check asks for: the service's own, else
// its kind's (Mailpit answers /livez), else "" for a plain port check.
func healthPath(svc model.Service) string { return kinds.HealthPath(svc.Kind, svc.HealthPath) }

// smtpPort is the SMTP port a Mailpit service listens on, 0 for other kinds
// and for Mailpit services with their own arguments (Zero2Dev does not know
// the port then).
func smtpPort(svc model.Service) int {
	if svc.Kind != "mailpit" || svc.CustomArgs {
		return 0
	}
	p, err := strconv.Atoi(kinds.Effective(svc.Kind, svc.Settings)["smtpPort"])
	if err != nil || p <= 0 || p > 65535 {
		return 0
	}
	return p
}

// checkExtraPorts refuses to start a service whose second port (Mailpit's
// SMTP port) is the web port or is taken already, before the process starts
// and fails with a less clear message of its own.
func checkExtraPorts(svc model.Service, free func(int) bool) error {
	p := smtpPort(svc)
	switch {
	case p == 0:
		return nil
	case p == svc.Port:
		return apperr.Conflict("the SMTP port and the web port are both %d; give them different ports", p)
	case !free(p):
		return apperr.Conflict("SMTP port %d is already in use", p)
	}
	return nil
}
