package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"mayoi/internal/settings"
)

// doSettings does what a mayoi settings asks and returns the command's
// status: with the person's preferences, or with those of the folder the
// command ran in, in its .mayoi/settings.json.
func doSettings(c Client, req Request, stdout, stderr io.Writer) int {
	path := c.SettingsPath
	if req.Settings.Folder {
		path = settings.FolderPath(req.Cwd)
	}
	out, err := req.Settings.Do(path)
	if err != nil {
		fmt.Fprintf(stderr, "mayoi: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, out)
	return 0
}

// Do does what a mayoi settings asks with the preferences kept in the
// file at path, the person's settings.json or a folder's
// .mayoi/settings.json, and returns what to print. It changes the file
// itself, a preference at a time, keeping the ones it does not change as
// the file has them; a Mayoi running reads the file again, as it does one
// a person saves.
func (s Settings) Do(path string) (string, error) {
	values, err := readPrefs(path)
	if err != nil {
		return "", err
	}
	switch {
	case s.Verb == "get":
		return getPrefs(values, s.Keys, s.Folder)
	case s.Verb == "list" || s.Verb == "":
		return listPrefs(values, s.Folder), nil
	case s.Verb != "set" && s.Verb != "unset":
		return "", fmt.Errorf("no settings command %q: mayoi settings lists, gets, sets or unsets them", s.Verb)
	case len(s.Changes) == 0:
		return "", fmt.Errorf("%s needs a preference: mayoi settings lists them", s.Verb)
	}
	// The changes are all made in a copy, of the file as it is, and
	// written once every one of them is one the preferences take.
	write := maps.Clone(values)
	for _, c := range s.Changes {
		if err := c.apply(write, s.Folder); err != nil {
			return "", err
		}
	}
	if samePrefs(write, values) {
		// A change that changes nothing leaves the file as it is, with
		// its keys in the order it has them.
		return "", nil
	}
	return "", writePrefs(path, write, order(write))
}

// order is the order the keys of values take as the file is written: the
// app's, so that its own save does not rewrite them, and a key it does
// not know after them, by name.
func order(values map[string]json.RawMessage) []string {
	var keys, rest []string
	for _, k := range settings.PrefKeys {
		if _, ok := values[k]; ok {
			keys = append(keys, k)
		}
	}
	for k := range values {
		if !slices.Contains(settings.PrefKeys, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(keys, rest...)
}

// apply makes c as the command asks with the preferences of values.
func (c Change) apply(values map[string]json.RawMessage, folder bool) error {
	t, err := parseTarget(c.Key, folder)
	if err != nil {
		return err
	}
	old := values[t.key]
	raw, err := c.edit(t, old)
	if err != nil {
		return err
	}
	switch {
	case raw == nil:
		delete(values, t.key)
	case !bytes.Equal(raw, old):
		values[t.key] = raw
	}
	if c.Op == OpUnset && t.key == "theme" {
		// An older file kept a theme in appearance, which the app reads
		// back as the theme, so the theme would not be gone: it goes too.
		if legacyThemes[textValue(values["appearance"])] {
			delete(values, "appearance")
		}
	}
	return nil
}

// legacyThemes are the looks an older version kept as an appearance
// rather than a theme, which Load turns into it as it reads them.
var legacyThemes = map[string]bool{"matrix": true, "claude": true}

// edit applies c to old, the value of the preference t names, and returns
// the value that takes its place, nil for none.
func (c Change) edit(t prefTarget, old json.RawMessage) (json.RawMessage, error) {
	f, _ := settings.FieldOf(t.key)
	switch c.Op {
	case OpUnset:
		// The preference goes, or the field of a look on the rail, which
		// is no look once it has no field.
		if t.dir == "" {
			return nil, nil
		}
		return t.update(old, t.field, nil)
	case OpAdd, OpRemove:
		if t.dir != "" || t.field != "" || f.Kind != settings.KindList {
			return nil, fmt.Errorf("%s is not a list, so %s%s= takes no item: %s takes %s", t.key, t.key, itemSign[c.Op], t.key, wants(f))
		}
		items := textList(old)
		item := c.Item
		if c.Op == OpAdd {
			if !slices.Contains(items, item) {
				items = append(items, item)
			}
		} else {
			items = slices.DeleteFunc(items, func(v string) bool { return v == item })
		}
		return marshal(items)
	}
	switch {
	case t.dir == "":
		return checked(f, c.Value)
	case t.field == "":
		// The whole look of the project: its label, color and icon.
		st, err := styleValue(c.Value)
		if err != nil {
			return nil, err
		}
		return t.update(old, "", st)
	default:
		st, err := styleFieldValue(t.field, c.Value)
		if err != nil {
			return nil, err
		}
		return t.update(old, t.field, st)
	}
}

// prefTarget is what a change, or a key to read, names: the preference of
// key in the settings' file, and of a preference that keeps looks on the
// rail by folder, the folder and the field of its look, as
// projectTabStyle.'<folder>'.label.
type prefTarget struct {
	key   string
	dir   string
	field string
}

// parseTarget reads the key of a change, or of a preference to read, and
// checks it: a preference, or a folder and a field of a look on the rail.
func parseTarget(key string, folder bool) (prefTarget, error) {
	segs, err := splitKey(key)
	if err != nil {
		return prefTarget{}, err
	}
	f, err := known(segs[0], folder)
	if err != nil {
		return prefTarget{}, err
	}
	t := prefTarget{key: f.Key}
	switch {
	case len(segs) == 1:
	case f.Kind != settings.KindStyles:
		return t, fmt.Errorf("%s has no fields: %s takes %s", f.Key, f.Key, wants(f))
	case len(segs) > 3:
		return t, fmt.Errorf("%s is too deep: a look on the rail has a label, a color and an icon, as %s.'<folder>'.label", key, f.Key)
	case len(segs) == 3 && !slices.Contains(styleFields, segs[2]):
		return t, fmt.Errorf("no field %q of a look on the rail: it has %s", segs[2], joinOr(styleFields))
	case len(segs) == 3:
		// A field of a look: a text, its label, color or icon.
		t.dir, t.field = segs[1], segs[2]
	default:
		// The look of a folder, in whole.
		t.dir = segs[1]
	}
	return t, nil
}

// splitKey splits a key into its preference and the parts of it, by dots
// that are not within quotes: a folder with dots of its own is told as
// '<folder>', and the quotes are no part of it.
func splitKey(key string) ([]string, error) {
	if key == "" {
		return nil, errors.New("no preference: mayoi settings lists them")
	}
	var segs []string
	var b strings.Builder
	var quote rune
	for _, r := range key {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '.':
			segs = append(segs, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("%s has a quote it does not close", key)
	}
	segs = append(segs, b.String())
	if slices.Contains(segs, "") {
		return nil, fmt.Errorf("%s has a part with no name", key)
	}
	return segs, nil
}

// update returns the looks on the rail of old with the look of t's folder,
// and of t's field of it, made st, which nil takes away: a look left with
// no field goes, the project's own again.
func (t prefTarget) update(old json.RawMessage, field string, st json.RawMessage) (json.RawMessage, error) {
	styles := styleMap(old)
	if field == "" {
		if st == nil {
			delete(styles, t.dir)
		} else {
			styles[t.dir] = st
		}
	} else {
		look := styleMap(styles[t.dir])
		switch {
		case st == nil:
			delete(look, field)
		case field == "label":
			// A label shows in place of the icon, which goes, as the rail's
			// menu sets one.
			delete(look, "icon")
			fallthrough
		default:
			look[field] = st
		}
		switch raw, err := marshal(look); {
		case err != nil:
			return nil, err
		case raw == nil:
			delete(styles, t.dir)
		default:
			styles[t.dir] = raw
		}
	}
	return marshal(styles)
}

// itemSign is the mark of the key that adds an item of a list, or takes
// one away, as fontFeatures+=calt=0 and fontFeatures-=calt=0.
var itemSign = map[string]string{OpAdd: "+", OpRemove: "-"}

// styleFields are the fields of a project's look on the rail.
var styleFields = []string{"label", "color", "icon"}

// railColors are the names the terminal's colors go by in a project's
// look on the rail, "" for the project's own.
var railColors = []string{"", "red", "green", "yellow", "blue", "magenta", "cyan"}

// styleValue checks a look of a project on the rail as a person gives it,
// and returns it as the settings' file keeps it, nil for the project's own
// look.
func styleValue(raw json.RawMessage) (json.RawMessage, error) {
	bad := fmt.Errorf(`the look of a project on the rail takes "label" of up to two characters, "color" of %s, and "icon", as {"label": "MG", "color": "cyan"}, not %s`, joinOr(railColors[1:]), short(raw))
	var in struct {
		Label *string `json:"label"`
		Color *string `json:"color"`
		Icon  *string `json:"icon"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	st := settings.TabStyle{}
	if dec.Decode(&in) != nil {
		return nil, bad
	}
	for _, f := range []struct {
		name string
		val  *string
		dst  *string
	}{{"label", in.Label, &st.Label}, {"color", in.Color, &st.Color}, {"icon", in.Icon, &st.Icon}} {
		if f.val == nil {
			continue
		}
		v, err := styleFieldValue(f.name, mustJSON(*f.val))
		if err != nil {
			return nil, err
		}
		json.Unmarshal(v, f.dst)
	}
	if st == (settings.TabStyle{}) {
		return nil, nil
	}
	return marshal(st)
}

// styleFieldValue checks what a field of a project's look on the rail is
// given: a text, which for the label is up to two characters and for the
// color one of the terminal's. A field left empty is nil, which takes it
// away from the look.
func styleFieldValue(field string, raw json.RawMessage) (json.RawMessage, error) {
	s := strings.TrimSpace(textValue(raw))
	switch field {
	case "label":
		// As many characters as a person sees, as the rail shows them.
		if settings.LabelTooLong(s) {
			return nil, fmt.Errorf("the label of a look on the rail is up to %d characters, not %s", settings.MaxLabel, short(raw))
		}
	case "color":
		if !slices.Contains(railColors, s) {
			return nil, fmt.Errorf("the color of a look on the rail is one of %s, not %s", joinOr(railColors[1:]), short(raw))
		}
	case "icon":
		// The icon names a picture the app copied into a folder of its
		// own, which a name typed here never is: it is chosen in the
		// rail's menu, and only taken away here, with icon= or unset.
		if s != "" {
			return nil, errors.New("an icon is chosen in the rail's menu, not by its name: icon= takes it away")
		}
	}
	if s == "" {
		return nil, nil
	}
	return marshal(s)
}

// styleMap is raw as the looks on the rail it holds, none for nil, or for
// anything else.
func styleMap(raw json.RawMessage) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	if len(raw) > 0 && json.Unmarshal(raw, &m) != nil {
		return map[string]json.RawMessage{}
	}
	return m
}

// textList is raw as the list of text it holds, none for nil, or for
// anything else.
func textList(raw json.RawMessage) []string {
	var items []string
	if len(raw) > 0 {
		json.Unmarshal(raw, &items)
	}
	return items
}

// marshal is v as JSON, nil for a map or a list with nothing in it, which
// is no value of the settings.
func marshal(v any) (json.RawMessage, error) {
	switch t := v.(type) {
	case map[string]json.RawMessage:
		if len(t) == 0 {
			return nil, nil
		}
	case []string:
		if len(t) == 0 {
			return nil, nil
		}
	case settings.TabStyle:
		if t == (settings.TabStyle{}) {
			return nil, nil
		}
	case string:
		if t == "" {
			return nil, nil
		}
	}
	raw, err := json.Marshal(v)
	return raw, err
}

// textValue is raw as the text a person gave: a text of JSON, else the
// JSON as they typed it, so that label=12 is the text 12.
func textValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return string(raw)
	}
	return s
}

// mustJSON is s as a text of JSON.
func mustJSON(s string) json.RawMessage {
	raw, _ := json.Marshal(s)
	return raw
}

// known returns the preference of key, as the settings window knows it: of
// those a folder may choose, for a folder's own file.
func known(key string, folder bool) (settings.Field, error) {
	f, ok := settings.FieldOf(key)
	switch {
	case !ok:
		return f, fmt.Errorf("no preference %q: mayoi settings lists them", key)
	case folder && !f.Folder:
		return f, fmt.Errorf("%s is not a preference a folder chooses: mayoi settings lists them", key)
	}
	return f, nil
}

// readPrefs reads the preferences of the file at path as JSON, those of a
// file that is not there yet none.
func readPrefs(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return map[string]json.RawMessage{}, nil
	case err != nil:
		return nil, err
	}
	values := map[string]json.RawMessage{}
	if strings.TrimSpace(string(data)) == "" {
		return values, nil
	}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("%s does not parse: %v", path, err)
	}
	if values == nil {
		values = map[string]json.RawMessage{}
	}
	return values, nil
}

// writePrefs writes the preferences of values to the file at path, a key
// a line, in the order of keys, which the app leaves them in, and a key
// it does not know last, as the file had it; the folder of the file is
// made when it is not there.
func writePrefs(path string, values map[string]json.RawMessage, keys []string) error {
	if path == "" {
		return errors.New("there is no settings file to write")
	}
	data, err := json.Marshal(ordered{values, keys})
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	return settings.WriteFile(path, out.Bytes())
}

// ordered is values as JSON, in the order of keys and then the rest, as
// a map writes its keys in the order it is given them.
type ordered struct {
	values map[string]json.RawMessage
	keys   []string
}

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if _, ok := o.values[k]; !ok {
			continue
		}
		if i > 0 && b.Len() > 1 {
			b.WriteByte(',')
		}
		b.Write(mustJSON(k))
		b.WriteByte(':')
		b.Write(o.values[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// samePrefs reports whether two sets of preferences, by their keys, are
// the same values.
func samePrefs(x, y map[string]json.RawMessage) bool {
	return maps.EqualFunc(x, y, func(a, b json.RawMessage) bool { return bytes.Equal(a, b) })
}

// prefsOf returns the preferences of values as Mayoi has them, those the
// file leaves out at their default, and those it holds to the wrong type
// too, as a folder's file may. The keys Mayoi has none of are left out.
func prefsOf(values map[string]json.RawMessage) settings.Prefs {
	p := settings.Default().Prefs
	for key, raw := range values {
		p.Set(key, raw)
	}
	return p
}

// listPrefs prints a line for each preference, its key, a tab, and its
// value as JSON, in the order the settings window shows them. A folder's
// file lists the preferences it chooses only.
func listPrefs(values map[string]json.RawMessage, folder bool) string {
	p := prefsOf(values)
	var b strings.Builder
	for _, f := range settings.Schema {
		if folder && !hasPref(values, f.Key) {
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\n", f.Key, p.Raw(f.Key))
	}
	return b.String()
}

// getPrefs prints the value of each of keys as JSON, a line each, and
// nothing for a preference a folder's file does not choose. A field of a
// look on the rail prints that field, null for one the look has not.
func getPrefs(values map[string]json.RawMessage, keys []string, folder bool) (string, error) {
	targets := make([]prefTarget, len(keys))
	// Every key is known before any is printed, so that a wrong one
	// prints nothing.
	for i, key := range keys {
		t, err := parseTarget(key, folder)
		if err != nil {
			return "", err
		}
		targets[i] = t
	}
	p := prefsOf(values)
	var b strings.Builder
	for _, t := range targets {
		if folder && !hasPref(values, t.key) {
			continue
		}
		raw := p.Raw(t.key)
		for _, field := range []string{t.dir, t.field} {
			if field == "" {
				continue
			}
			if raw = styleMap(raw)[field]; raw == nil {
				raw = json.RawMessage("null")
				break
			}
		}
		fmt.Fprintf(&b, "%s\n", raw)
	}
	return b.String(), nil
}

// hasPref reports whether the file of values holds the preference of key,
// rather than leaving it out.
func hasPref(values map[string]json.RawMessage, key string) bool {
	_, ok := values[key]
	return ok
}

// checked checks the value of the preference of f and returns it as the
// settings' file keeps it, in its bounds and its options, as Mayoi makes
// it.
func checked(f settings.Field, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`""`)
	}
	bad := fmt.Errorf("%s takes %s, not %s", f.Key, wants(f), short(raw))
	// The value is checked as given: a number out of its bounds is no
	// value of it, rather than the bound it would be kept to.
	switch f.Kind {
	case settings.KindEnum:
		var v string
		if json.Unmarshal(raw, &v) != nil || !slices.Contains(f.Options, v) {
			return nil, bad
		}
	case settings.KindNumber:
		var n float64
		if json.Unmarshal(raw, &n) != nil || n < f.Min || n > f.Max {
			return nil, bad
		}
	case settings.KindList:
		// A list of text is a list, not the one item a field gives it.
		var items []string
		if json.Unmarshal(raw, &items) != nil {
			return nil, bad
		}
	case settings.KindStyles:
		var styles map[string]json.RawMessage
		if json.Unmarshal(raw, &styles) != nil {
			return nil, bad
		}
		for dir, look := range styles {
			st, err := styleValue(look)
			if err != nil {
				return nil, err
			}
			switch {
			case st == nil:
				delete(styles, dir)
			default:
				styles[dir] = st
			}
		}
		return marshal(styles)
	}
	if f.Kind == settings.KindText {
		raw = mustJSON(textValue(raw))
	}
	var p settings.Prefs
	if err := p.Set(f.Key, raw); err != nil {
		return nil, bad
	}
	return p.Raw(f.Key), nil
}

// wants says what value the preference of f takes, as a person gives it.
func wants(f settings.Field) string {
	switch f.Kind {
	case settings.KindBool:
		return "true or false"
	case settings.KindEnum:
		return "one of " + joinOr(f.Options)
	case settings.KindNumber:
		return fmt.Sprintf("a number from %v to %v", f.Min, f.Max)
	case settings.KindList:
		return "a list of text, as [\"a\", \"b\"], or " + f.Key + "+=item and " + f.Key + "-=item"
	case settings.KindStyles:
		return `the looks of the projects on the rail, as {"<folder>": {"label": "MG"}}, or a field of one`
	}
	return "a line of text"
}

// joinOr joins values as a person says them: `"a"`, `"a"` or `"b"`, and
// `"a"`, `"b"` or `"c"`.
func joinOr(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	switch len(quoted) {
	case 0:
		return "nothing"
	case 1:
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

// short is a value as a person reads it, a text in quotes and other
// values as they are.
func short(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return string(raw)
}
