package workspace

import (
	"path/filepath"
	"testing"
)

// TestWithinRoot covers the containment predicate directly, including
// the root-directory case, which no temporary directory can produce.
func TestWithinRoot(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		name string
		root string
		p    string
		want bool
	}{
		{name: "path is root", root: sep + "srv" + sep + "ws", p: sep + "srv" + sep + "ws", want: true},
		{name: "child of root", root: sep + "srv" + sep + "ws", p: sep + "srv" + sep + "ws" + sep + "a.txt", want: true},
		{name: "nested child", root: sep + "srv" + sep + "ws", p: sep + "srv" + sep + "ws" + sep + "a" + sep + "b", want: true},
		{name: "sibling prefix", root: sep + "srv" + sep + "ws", p: sep + "srv" + sep + "wsx", want: false},
		{name: "parent", root: sep + "srv" + sep + "ws", p: sep + "srv", want: false},
		{name: "unrelated", root: sep + "srv" + sep + "ws", p: sep + "etc" + sep + "passwd", want: false},
		{name: "root directory is itself", root: sep, p: sep, want: true},
		{name: "child of root directory", root: sep, p: sep + "etc", want: true},
		{name: "nested child of root directory", root: sep, p: sep + "etc" + sep + "hostname", want: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withinRoot(c.root, c.p); got != c.want {
				t.Errorf("withinRoot(%q, %q) = %v, want %v", c.root, c.p, got, c.want)
			}
		})
	}
}
