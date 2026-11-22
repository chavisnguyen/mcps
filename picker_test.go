package main

import (
	"reflect"
	"testing"
)

func TestDiffSelection(t *testing.T) {
	was := map[string]bool{"a": true, "b": true}
	added, removed := diffSelection(was, []string{"b", "c", "d"})
	if !reflect.DeepEqual(added, []string{"c", "d"}) || !reflect.DeepEqual(removed, []string{"a"}) {
		t.Fatalf("added=%v removed=%v", added, removed)
	}
	if a, r := diffSelection(nil, []string{}); a != nil || r != nil {
		t.Fatalf("empty diff: %v %v", a, r)
	}
}
