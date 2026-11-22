// Package compare checks if two JSON responses mean the same thing for the
// clients of the old system.
//
// The important part: null, an empty list and a missing field are three
// different answers. Some old clients crash on null, some only check if the
// key exists. So we never treat them as equal, we report exactly which one
// it was and you decide later if you care.
package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Kind is the type of a difference.
type Kind string

const (
	MissingInNew  Kind = "missing in new"
	MissingInOld  Kind = "missing in old"
	NullVsEmpty   Kind = "null vs empty"
	TypeChanged   Kind = "type changed"
	ValueChanged  Kind = "value changed"
	LengthChanged Kind = "length changed"
)

// Diff is one difference between the old and the new response.
type Diff struct {
	Path string // like $.orders[0].items
	Kind Kind
	Old  string // short JSON of the old value, empty if missing
	New  string
}

func (d Diff) String() string {
	return fmt.Sprintf("%s: %s (old=%s new=%s)", d.Path, d.Kind, orDash(d.Old), orDash(d.New))
}

// Options for a comparison.
type Options struct {
	// Ignore has paths we already know are different and we are ok with,
	// for example fields only the old system ever used. Use [*] to match
	// any index: "$.customers[*].legacy_rev".
	Ignore []string
}

// JSON compares two JSON documents. Diffs are returned in a stable order.
func JSON(oldDoc, newDoc []byte, opt Options) ([]Diff, error) {
	a, err := decode(oldDoc)
	if err != nil {
		return nil, fmt.Errorf("old: %w", err)
	}
	b, err := decode(newDoc)
	if err != nil {
		return nil, fmt.Errorf("new: %w", err)
	}
	c := &comparer{ignore: map[string]bool{}}
	for _, p := range opt.Ignore {
		c.ignore[p] = true
	}
	c.walk("$", a, b, true, true)
	return c.diffs, nil
}

func decode(doc []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	// UseNumber so 42.50 does not become a float before we look at it.
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

type comparer struct {
	ignore map[string]bool
	diffs  []Diff
}

func (c *comparer) add(path string, k Kind, a, b any, aOK, bOK bool) {
	d := Diff{Path: path, Kind: k}
	if aOK {
		d.Old = short(a)
	}
	if bOK {
		d.New = short(b)
	}
	c.diffs = append(c.diffs, d)
}

func (c *comparer) walk(path string, a, b any, aOK, bOK bool) {
	if c.ignore[path] || c.ignore[wildcard(path)] {
		return
	}
	switch {
	case !aOK && !bOK:
		return
	case !bOK:
		c.add(path, MissingInNew, a, b, aOK, bOK)
		return
	case !aOK:
		c.add(path, MissingInOld, a, b, aOK, bOK)
		return
	case a == nil && b == nil:
		return
	case a == nil || b == nil:
		if isEmpty(a) || isEmpty(b) {
			c.add(path, NullVsEmpty, a, b, aOK, bOK)
		} else {
			c.add(path, TypeChanged, a, b, aOK, bOK)
		}
		return
	}

	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			c.add(path, TypeChanged, a, b, aOK, bOK)
			return
		}
		for _, k := range keys(av, bv) {
			x, xOK := av[k]
			y, yOK := bv[k]
			c.walk(path+"."+k, x, y, xOK, yOK)
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			c.add(path, TypeChanged, a, b, aOK, bOK)
			return
		}
		if len(av) != len(bv) {
			c.diffs = append(c.diffs, Diff{Path: path, Kind: LengthChanged,
				Old: strconv.Itoa(len(av)), New: strconv.Itoa(len(bv))})
		}
		for i := 0; i < len(av) && i < len(bv); i++ {
			c.walk(fmt.Sprintf("%s[%d]", path, i), av[i], bv[i], true, true)
		}
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			c.add(path, TypeChanged, a, b, aOK, bOK)
			return
		}
		// 42.5 and 42.50 are the same number for every client I know.
		x, err1 := strconv.ParseFloat(av.String(), 64)
		y, err2 := strconv.ParseFloat(bv.String(), 64)
		if err1 != nil || err2 != nil || x != y {
			c.add(path, ValueChanged, a, b, aOK, bOK)
		}
	default: // string, bool
		if typeName(a) != typeName(b) {
			c.add(path, TypeChanged, a, b, aOK, bOK)
		} else if a != b {
			c.add(path, ValueChanged, a, b, aOK, bOK)
		}
	}
}

func keys(a, b map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func typeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	}
	return fmt.Sprintf("%T", v)
}

// wildcard turns $.a[3].b into $.a[*].b
func wildcard(path string) string {
	var sb strings.Builder
	inIndex := false
	for _, r := range path {
		switch {
		case r == '[':
			inIndex = true
			sb.WriteString("[*")
		case r == ']':
			inIndex = false
			sb.WriteRune(r)
		case !inIndex:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func short(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	if len(b) > 60 {
		return string(b[:57]) + "..."
	}
	return string(b)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
