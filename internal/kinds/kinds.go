// Package kinds describes what each service kind needs: which form fields
// apply, its settings with their defaults and explanations, and how the
// settings become a command line. The descriptions live in profiles.json,
// which the daemon serves to the UI at GET /api/kinds, so they are written once.
package kinds

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed profiles.json
var profilesJSON []byte

// Field says whether a form field applies to a kind.
type Field struct {
	Use     string `json:"use"` // required | optional | hidden
	Label   string `json:"label,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Default int    `json:"default,omitempty"`
}

// Fixed are arguments always added, with the reason shown to the user.
type Fixed struct {
	Args    []string `json:"args"`
	Help    string   `json:"help"`
	Windows bool     `json:"windows,omitempty"` // only on Windows
}

// FixedEnv is an environment variable always set.
type FixedEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Help  string `json:"help"`
}

// Choice is one value of a select or multi option. Args, when present,
// replace the option's emit.arg for this value (an empty list emits nothing).
type Choice struct {
	Value string   `json:"value"`
	Label string   `json:"label"`
	Args  []string `json:"args,omitempty"`
}

// Emit says what a setting writes.
type Emit struct {
	Arg     []string          `json:"arg,omitempty"`     // added when the value is non-empty (bool: true)
	ArgOff  []string          `json:"argOff,omitempty"`  // bool: added when false
	Ini     string            `json:"ini,omitempty"`     // php.ini key
	Env     string            `json:"env,omitempty"`     // environment variable set to the value
	EnvSet  map[string]string `json:"envSet,omitempty"`  // bool: variables set when true
	Program map[string]string `json:"program,omitempty"` // value -> program run instead of the runtime binary
	Special string            `json:"special,omitempty"` // handled in code (php.go)
}

// Option is one setting.
type Option struct {
	Key         string            `json:"key"`
	Group       string            `json:"group"`
	Label       string            `json:"label"`
	Common      bool              `json:"common,omitempty"` // shown up front; the rest is under "More settings"
	Type        string            `json:"type"`             // text number bool select multi size lines
	Default     string            `json:"default"`
	Help        string            `json:"help"`
	Placeholder string            `json:"placeholder,omitempty"`
	Choices     []Choice          `json:"choices,omitempty"`
	ChoicesFrom string            `json:"choicesFrom,omitempty"` // phpExtensions: read from the runtime
	Suggest     string            `json:"suggest,omitempty"`     // npmScripts: suggestions from the project
	Min         *int              `json:"min,omitempty"`
	Max         *int              `json:"max,omitempty"`
	Pattern     string            `json:"pattern,omitempty"`
	Show        map[string]string `json:"show,omitempty"` // key -> value ("*" = set; multi: contains)
	MinVersion  string            `json:"minVersion,omitempty"`
	Required    bool              `json:"required,omitempty"`
	At          string            `json:"at,omitempty"` // "end": after the other options
	Emit        Emit              `json:"emit"`

	re *regexp.Regexp
}

// Profile is everything the form and the supervisor need to know about a kind.
type Profile struct {
	Kind          string     `json:"kind"`
	Label         string     `json:"label"`
	Summary       string     `json:"summary"`           // one line on what the type is, for the type picker
	Managed       bool       `json:"managed,omitempty"` // created by Zero2Dev, not offered for new services
	Note          string     `json:"note,omitempty"`
	Folder        Field      `json:"folder"`
	Port          Field      `json:"port"`
	Health        bool       `json:"health"`
	DataDir       string     `json:"dataDir"` // service | shared | none
	Program       bool       `json:"program,omitempty"`
	ExtraArgs     string     `json:"extraArgs"`
	ExtraArgsHint string     `json:"extraArgsHint"`
	Fixed         []Fixed    `json:"fixed,omitempty"`
	Env           []FixedEnv `json:"env,omitempty"`
	Options       []Option   `json:"options"`
}

var profiles []Profile

func init() {
	if err := json.Unmarshal(profilesJSON, &profiles); err != nil {
		panic("kinds: profiles.json: " + err.Error())
	}
	for i := range profiles {
		for j := range profiles[i].Options {
			o := &profiles[i].Options[j]
			if o.Pattern != "" {
				o.re = regexp.MustCompile(o.Pattern)
			}
		}
	}
}

// JSON is the raw profiles file, served as is.
func JSON() []byte { return profilesJSON }

// Profiles returns all profiles.
func Profiles() []Profile { return profiles }

// Lookup returns the profile of kind.
func Lookup(kind string) (*Profile, bool) {
	for i := range profiles {
		if profiles[i].Kind == kind {
			return &profiles[i], true
		}
	}
	return nil, false
}

// FolderRequired reports whether a service of kind must name a folder.
func FolderRequired(kind string) bool {
	p, ok := Lookup(kind)
	return !ok || p.Folder.Use == "required"
}

func (p *Profile) option(key string) *Option {
	for i := range p.Options {
		if p.Options[i].Key == key {
			return &p.Options[i]
		}
	}
	return nil
}

// Defaults returns the default value of every option of kind.
func Defaults(kind string) map[string]string {
	out := map[string]string{}
	if p, ok := Lookup(kind); ok {
		for _, o := range p.Options {
			out[o.Key] = o.Default
		}
	}
	return out
}

// Effective fills in defaults for keys the settings do not have, so options
// added in later versions never break a saved service.
func Effective(kind string, settings map[string]string) map[string]string {
	out := Defaults(kind)
	for k, v := range settings {
		if _, ok := out[k]; ok {
			out[k] = v
		}
	}
	return out
}

// Visible reports whether o applies with the given (effective) settings.
func (p *Profile) Visible(o *Option, s map[string]string) bool {
	for k, want := range o.Show {
		got := s[k]
		dep := p.option(k)
		switch {
		case want == "*":
			if got == "" || got == "false" {
				return false
			}
		case dep != nil && dep.Type == "multi":
			if !slices.Contains(SplitList(got), want) {
				return false
			}
		default:
			if got != want {
				return false
			}
		}
		if dep != nil && !p.Visible(dep, s) {
			return false
		}
	}
	return true
}

// SplitList splits a multi value ("a,b,c") into its non-empty parts.
func SplitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidationError names the setting that is wrong.
type ValidationError struct {
	Key, Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// Validate checks settings against the profile of kind. Unknown keys,
// wrong types, values outside the choices or range and line breaks (which
// could smuggle extra php.ini lines or arguments) are rejected.
func Validate(kind string, settings map[string]string) error {
	p, ok := Lookup(kind)
	if !ok {
		if len(settings) > 0 {
			return &ValidationError{Msg: fmt.Sprintf("%s services have no settings", kind)}
		}
		return nil
	}
	if len(settings) > 64 {
		return &ValidationError{Msg: "too many settings"}
	}
	for k := range settings {
		if p.option(k) == nil {
			return &ValidationError{Key: k, Msg: fmt.Sprintf("unknown setting %q for %s services", k, kind)}
		}
	}
	eff := Effective(kind, settings)
	for i := range p.Options {
		o := &p.Options[i]
		v := eff[o.Key]
		if err := o.check(v); err != nil {
			return &ValidationError{Key: o.Key, Msg: o.Label + ": " + err.Error()}
		}
		if o.Required && strings.TrimSpace(v) == "" && p.Visible(o, eff) {
			return &ValidationError{Key: o.Key, Msg: o.Label + " is required"}
		}
	}
	return nil
}

func (o *Option) check(v string) error {
	if strings.ContainsRune(v, 0) {
		return fmt.Errorf("contains invalid characters")
	}
	if len(v) > 4096 {
		return fmt.Errorf("is too long")
	}
	if o.Type != "lines" && strings.ContainsAny(v, "\r\n") {
		return fmt.Errorf("must be on one line")
	}
	switch o.Type {
	case "bool":
		if v != "true" && v != "false" {
			return fmt.Errorf("must be true or false")
		}
	case "number":
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("must be a whole number")
		}
		if o.Min != nil && n < *o.Min || o.Max != nil && n > *o.Max {
			return fmt.Errorf("must be between %d and %d", deref(o.Min), deref(o.Max))
		}
	case "select":
		if !o.hasChoice(v) {
			return fmt.Errorf("%q is not one of the choices", v)
		}
	case "multi":
		for _, x := range SplitList(v) {
			if len(o.Choices) > 0 && !o.hasChoice(x) {
				return fmt.Errorf("%q is not one of the choices", x)
			}
			if o.re != nil && !o.re.MatchString(x) {
				return fmt.Errorf("%q is not a valid name", x)
			}
		}
		return nil
	case "lines":
		for _, l := range strings.Split(strings.ReplaceAll(v, "\r\n", "\n"), "\n") {
			if err := iniLine(l); err != nil {
				return err
			}
		}
		return nil
	}
	if o.re != nil && v != "" && !o.re.MatchString(v) {
		return fmt.Errorf("%q is not valid here", v)
	}
	return nil
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func (o *Option) hasChoice(v string) bool {
	for _, c := range o.Choices {
		if c.Value == v {
			return true
		}
	}
	return false
}

func (o *Option) choice(v string) *Choice {
	for i := range o.Choices {
		if o.Choices[i].Value == v {
			return &o.Choices[i]
		}
	}
	return nil
}

var iniLineRE = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_.\[\]]*\s*=\s*[^\[\]]*$`)

// iniLine accepts "name = value", a ; comment or an empty line.
func iniLine(l string) error {
	t := strings.TrimSpace(l)
	if t == "" || strings.HasPrefix(t, ";") {
		return nil
	}
	if !iniLineRE.MatchString(t) {
		return fmt.Errorf("%q is not a php.ini line (name = value)", t)
	}
	return nil
}

// VersionAtLeast compares dotted versions numerically ("20.11.1" >= "20.6").
// An unknown version ("" or not numeric) counts as new enough.
func VersionAtLeast(have, want string) bool {
	h, w := nums(have), nums(want)
	if len(h) == 0 || len(w) == 0 {
		return true
	}
	for i := 0; i < len(w); i++ {
		hv := 0
		if i < len(h) {
			hv = h[i]
		}
		if hv != w[i] {
			return hv > w[i]
		}
	}
	return true
}

func nums(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out []int
	for _, p := range strings.Split(v, ".") {
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		if end == 0 {
			break
		}
		n, _ := strconv.Atoi(p[:end])
		out = append(out, n)
		if end < len(p) {
			break
		}
	}
	return out
}
