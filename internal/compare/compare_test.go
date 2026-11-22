package compare

import (
	"reflect"
	"testing"
)

func TestJSON(t *testing.T) {
	tests := []struct {
		name   string
		old    string
		new    string
		ignore []string
		want   []Diff
	}{
		{name: "same", old: `{"a":1,"b":"x"}`, new: `{"b":"x","a":1}`},
		{name: "both null", old: `{"a":null}`, new: `{"a":null}`},
		{
			name: "null vs empty list",
			old:  `{"items":null}`, new: `{"items":[]}`,
			want: []Diff{{Path: "$.items", Kind: NullVsEmpty, Old: "null", New: "[]"}},
		},
		{
			name: "empty object vs null",
			old:  `{"meta":{}}`, new: `{"meta":null}`,
			want: []Diff{{Path: "$.meta", Kind: NullVsEmpty, Old: "{}", New: "null"}},
		},
		{
			name: "null vs missing",
			old:  `{"items":null}`, new: `{}`,
			want: []Diff{{Path: "$.items", Kind: MissingInNew, Old: "null"}},
		},
		{
			name: "missing vs empty list",
			old:  `{}`, new: `{"items":[]}`,
			want: []Diff{{Path: "$.items", Kind: MissingInOld, New: "[]"}},
		},
		{
			name: "null vs real value",
			old:  `{"name":null}`, new: `{"name":"Marta"}`,
			want: []Diff{{Path: "$.name", Kind: TypeChanged, Old: "null", New: `"Marta"`}},
		},
		{name: "same number different text", old: `{"total":42.50}`, new: `{"total":42.5}`},
		{
			name: "number as string",
			old:  `{"total":"42.50"}`, new: `{"total":42.5}`,
			want: []Diff{{Path: "$.total", Kind: TypeChanged, Old: `"42.50"`, New: "42.5"}},
		},
		{
			name: "value changed deep",
			old:  `{"orders":[{"status":"SHIPPED"}]}`, new: `{"orders":[{"status":"shipped"}]}`,
			want: []Diff{{Path: "$.orders[0].status", Kind: ValueChanged, Old: `"SHIPPED"`, New: `"shipped"`}},
		},
		{
			name: "list length",
			old:  `[1,2,3]`, new: `[1,2]`,
			want: []Diff{{Path: "$", Kind: LengthChanged, Old: "3", New: "2"}},
		},
		{
			name:   "ignore exact path",
			old:    `{"id":"c-1","legacy_rev":7}`,
			new:    `{"id":"c-1"}`,
			ignore: []string{"$.legacy_rev"},
		},
		{
			name:   "ignore with wildcard",
			old:    `{"list":[{"id":1,"legacy_rev":7},{"id":2,"legacy_rev":8}]}`,
			new:    `{"list":[{"id":1},{"id":2}]}`,
			ignore: []string{"$.list[*].legacy_rev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JSON([]byte(tt.old), []byte(tt.new), Options{Ignore: tt.ignore})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestJSONBadInput(t *testing.T) {
	if _, err := JSON([]byte(`{"a":`), []byte(`{}`), Options{}); err == nil {
		t.Error("expected error for broken old json")
	}
	if _, err := JSON([]byte(`{}`), []byte(`nope`), Options{}); err == nil {
		t.Error("expected error for broken new json")
	}
}

func TestWildcard(t *testing.T) {
	if got := wildcard("$.a[12].b[0]"); got != "$.a[*].b[*]" {
		t.Errorf("got %s", got)
	}
}
