package supervisor

import (
	"zero2dev/internal/model"
)

// Stop order for "Stop all": what depends on something goes first.
const (
	tierApps       = iota // your own services, Node, PHP, Go, Java
	tierDatabases         // they must outlive the apps that use them
	tierSiteServer        // Caddy / nginx, the front door, goes last
)

// StopTier is the position of a service kind in the Stop all order.
func StopTier(kind string) int {
	switch {
	case model.IsDatabase(kind):
		return tierDatabases
	case kind == "caddy" || kind == "nginx":
		return tierSiteServer
	}
	return tierApps
}

// StopFailure is a service that did not stop.
type StopFailure struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// StopReport is the outcome of StopOrdered / StopAllOrdered. Both lists are never nil.
type StopReport struct {
	Stopped []string      `json:"stopped"`
	Failed  []StopFailure `json:"failed"`
}

// StopAllOrdered stops every running service without shutting the daemon down
// (see StopOrdered). Unlike StopAll, new starts stay possible afterwards.
func (s *Supervisor) StopAllOrdered() StopReport { return s.StopOrdered(nil) }
