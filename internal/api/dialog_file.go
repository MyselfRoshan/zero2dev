package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"zero2dev/internal/apperr"
	"zero2dev/internal/winpick"
)

// The native Open / Save as dialog (0.0.5, docs/API.md "File dialog"), for
// picking a dump to import or the file an export writes.
func (s *Server) fileDialogRoutes() {
	s.handle("POST /api/dialog/file", s.pickFile)
}

func (s *Server) pickFile(w http.ResponseWriter, r *http.Request) error {
	if s.PickFile == nil {
		return &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: winpick.ErrUnsupported.Error()}
	}
	var in struct {
		Title       string           `json:"title"`
		Start       string           `json:"start"`
		Save        bool             `json:"save"`
		DefaultName string           `json:"defaultName"`
		Filters     []winpick.Filter `json:"filters"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &in); err != nil {
			return err
		}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Select a file"
		if in.Save {
			title = "Save as"
		}
	}
	if utf8.RuneCountInString(title) > 100 || strings.ContainsAny(title, "\x00\r\n") {
		return apperr.BadRequest("title must be one line of at most 100 characters")
	}
	if len(in.Filters) > 10 {
		return apperr.BadRequest("at most 10 filters")
	}
	for _, f := range in.Filters {
		if f.Name == "" || f.Pattern == "" || len(f.Name) > 60 || len(f.Pattern) > 100 || strings.ContainsAny(f.Name+f.Pattern, "\x00\r\n") {
			return apperr.BadRequest("each filter needs a name and a pattern like *.sql;*.sql.gz")
		}
	}
	if strings.ContainsAny(in.DefaultName, "\x00\r\n\\/:") || len(in.DefaultName) > 200 {
		return apperr.BadRequest("defaultName must be a file name")
	}
	path, ok, err := s.PickFile(winpick.FileOptions{
		Title: title, Start: strings.TrimSpace(in.Start), Save: in.Save, DefaultName: in.DefaultName, Filters: in.Filters,
	})
	switch {
	case errors.Is(err, winpick.ErrBusy):
		return apperr.Conflict("a file or folder dialog is already open on this PC; finish or close it first")
	case errors.Is(err, winpick.ErrUnsupported):
		return &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: err.Error()}
	case err != nil:
		return apperr.Internal("%v", err)
	}
	writeJSON(w, 200, map[string]any{"path": path, "cancelled": !ok})
	return nil
}
