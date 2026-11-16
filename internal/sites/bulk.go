package sites

import (
	"errors"
	"slices"

	"zero2dev/internal/store"
)

// SetEnabledMany switches the sites in ids on or off with one store update and
// one Regenerate (so the site server restarts once, not once per site). ids ==
// nil means every site; a non-nil empty slice means none; ids that name no
// site are ignored. It returns how many sites actually changed; when that is
// zero nothing is written and nothing restarts.
func (m *Manager) SetEnabledMany(ids []string, on bool) (int, error) {
	changed := 0
	err := m.st.Update(func(d *store.Data) error {
		for i := range d.Sites {
			if ids != nil && !slices.Contains(ids, d.Sites[i].ID) {
				continue
			}
			if d.Sites[i].Enabled != on {
				d.Sites[i].Enabled = on
				changed++
			}
		}
		if changed == 0 {
			return errNoChange
		}
		return nil
	})
	if err == errNoChange {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	m.afterChange()
	return changed, nil
}

var errNoChange = errors.New("no change")
