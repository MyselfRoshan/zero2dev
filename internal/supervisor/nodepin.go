package supervisor

import (
	"fmt"

	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
)

// nodePinWarning is the one warning line of a Node service whose folder pins a Node version
// (.node-version, devEngines.runtime, engines.node, .nvmrc) that its Node runtime rt does not
// satisfy. It names an installed Node that does, if there is one, and otherwise says where to get
// one. "" when nothing is pinned, the pin is met, or the pin is a form this does not read.
func nodePinWarning(cwd string, rt *model.Runtime, all []model.Runtime) string {
	pin, ok := kinds.ReadNodePin(cwd)
	if !ok {
		return ""
	}
	met, known := kinds.SatisfiesNode(pin.Spec, rt.Version)
	if !known || met {
		return ""
	}
	best := ""
	for _, r := range all {
		if r.Kind != "node" || r.ID == rt.ID {
			continue
		}
		if m, k := kinds.SatisfiesNode(pin.Spec, r.Version); k && m && (best == "" || kinds.VersionAtLeast(r.Version, best)) {
			best = r.Version
		}
	}
	w := fmt.Sprintf("This folder pins Node %s (%s) but the Node of this service is %s", pin.Spec, pin.Source, rt.Version)
	if best != "" {
		return w + fmt.Sprintf(": Node %s is installed, select it as this service's runtime", best)
	}
	return w + ": install a matching Node on the Runtimes page and select it as this service's runtime"
}
