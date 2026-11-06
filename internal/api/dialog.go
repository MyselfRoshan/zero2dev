package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"zero2dev/internal/apperr"
	"zero2dev/internal/winpick"
)

// pickFolder opens the native "Select Folder" dialog on this PC and answers
// with the chosen absolute path once the user closes it (the request stays
// open meanwhile). Body: {"title": "...", "start": "C:\\work"}, both optional.
// 200 {"path": "C:\\work\\app", "cancelled": false} or {"path": "", "cancelled": true}.
func (s *Server) pickFolder(w http.ResponseWriter, r *http.Request) error {
	if s.PickFolder == nil {
		return &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: winpick.ErrUnsupported.Error()}
	}
	var in struct {
		Title string `json:"title"`
		Start string `json:"start"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &in); err != nil {
			return err
		}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Select a folder"
	}
	if utf8.RuneCountInString(title) > 100 || strings.ContainsAny(title, "\x00\r\n") {
		return apperr.BadRequest("title must be one line of at most 100 characters")
	}
	path, ok, err := s.PickFolder(title, strings.TrimSpace(in.Start))
	switch {
	case errors.Is(err, winpick.ErrBusy):
		return apperr.Conflict("a folder dialog is already open on this PC; finish or close it first")
	case errors.Is(err, winpick.ErrUnsupported):
		return &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: err.Error()}
	case err != nil:
		return apperr.Internal("%v", err)
	}
	writeJSON(w, 200, map[string]any{"path": path, "cancelled": !ok})
	return nil
}
