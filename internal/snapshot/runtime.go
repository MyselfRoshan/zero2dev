package snapshot

import (
	"os"
	"runtime"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/catalog"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// runtimeRef records the runtime svc would run with now (the resolution of
// its next start), or nil when it has none: a custom command, a kind without
// runtimes, or nothing that resolves (then warn says why).
func (m *Manager) runtimeRef(svc model.Service) (ref *RuntimeRef, warn string) {
	if strings.TrimSpace(svc.Command) != "" || svc.Kind == "custom" || svc.Kind == "" {
		return nil, ""
	}
	rt, bin, err := m.Sup.RuntimeFor(svc)
	if err != nil {
		return nil, svc.Name + ": its runtime could not be resolved (" + err.Error() + "), so restoring will not pin one"
	}
	ref = &RuntimeRef{Kind: svc.Kind, Binary: bin, FollowedDefault: svc.RuntimeID == ""}
	if rt != nil {
		ref.Kind, ref.Version, ref.CatalogID = rt.Kind, rt.Version, rt.CatalogID
	}
	return ref, ""
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// matchRuntime finds the installed or registered runtime that ref names: by
// catalogId, then by kind and version, then by binary (a registered program).
func matchRuntime(ref *RuntimeRef, rts []model.Runtime) *model.Runtime {
	if ref == nil {
		return nil
	}
	if ref.CatalogID != "" {
		for i := range rts {
			if rts[i].CatalogID == ref.CatalogID {
				return &rts[i]
			}
		}
	}
	if ref.Version != "" {
		for i := range rts {
			if rts[i].Kind == ref.Kind && rts[i].Version == ref.Version {
				return &rts[i]
			}
		}
	}
	if ref.Binary != "" {
		for i := range rts {
			if rts[i].Kind == ref.Kind && samePath(rts[i].Binary, ref.Binary) {
				return &rts[i]
			}
		}
	}
	return nil
}

// isMissing reports whether a restore cannot find ref: no runtime matches,
// and it is not a program that is still there on disk (one found on PATH when
// the snapshot was taken has no catalogId and no version).
func isMissing(ref *RuntimeRef, rts []model.Runtime) bool {
	if ref == nil || matchRuntime(ref, rts) != nil {
		return false
	}
	if ref.CatalogID == "" && ref.Version == "" && ref.Binary != "" {
		if st, err := os.Stat(ref.Binary); err == nil && !st.IsDir() {
			return false
		}
	}
	return true
}

// missing lists the runtimes mf needs that are not there now, one entry per
// runtime with the names of the services that need it.
func (m *Manager) missing(mf Manifest, d store.Data) []Missing {
	out := []Missing{}
	var cat []model.CatalogEntry
	loaded := false
	for _, e := range mf.Services {
		ref := e.Runtime
		if !isMissing(ref, d.Runtimes) {
			continue
		}
		found := false
		for i := range out {
			if out[i].Kind == ref.Kind && out[i].Version == ref.Version && (ref.CatalogID == "" || out[i].CatalogID == ref.CatalogID) {
				out[i].Services = append(out[i].Services, e.Service.Name)
				found = true
				break
			}
		}
		if found {
			continue
		}
		if !loaded && m.Catalog != nil {
			cat, _ = m.Catalog()
			loaded = true
		}
		miss := Missing{Kind: ref.Kind, Version: ref.Version, CatalogID: ref.CatalogID, Services: []string{e.Service.Name}}
		if id := installable(ref, cat); id != "" {
			miss.Installable, miss.CatalogID = true, id
		}
		out = append(out, miss)
	}
	return out
}

// installable returns the id of the catalog entry for this machine that
// provides ref (same catalogId, else same kind and version), or "".
func installable(ref *RuntimeRef, cat []model.CatalogEntry) string {
	ok := func(e model.CatalogEntry) bool { return e.Supported || catalog.Supported(e) }
	if ref.CatalogID != "" {
		for _, e := range cat {
			if e.ID == ref.CatalogID && ok(e) {
				return e.ID
			}
		}
	}
	if ref.Version != "" {
		for _, e := range cat {
			if e.Kind == ref.Kind && e.Version == ref.Version && ok(e) {
				return e.ID
			}
		}
	}
	return ""
}

// missingError is the 409 of a restore whose runtimes are not all there;
// details.missing lists them as "kind version" separated by ", " (GET
// /api/snapshots/{id} has the full list).
func missingError(list []Missing) error {
	var parts []string
	for _, x := range list {
		s := x.Kind
		if x.Version != "" {
			s += " " + x.Version
		}
		parts = append(parts, s)
	}
	pron := "it"
	if len(parts) > 1 {
		pron = "them"
	}
	return apperr.Conflict("the snapshot needs %s, which %s not installed; install %s first", joinNames(parts), isAre(len(parts)), pron).
		With("missing", strings.Join(parts, ", "))
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
