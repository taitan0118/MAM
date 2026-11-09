package main

import "testing"

func TestToEvents(t *testing.T) {
	if l := toEvents([]byte(`[{"id":"Sys1","at":5,"src":"disk","eid":7,"lvl":2},{"id":"App2","at":6,"lvl":1}]`)); len(l) != 2 || l[0].EID != 7 || l[1].Lvl != 1 {
		t.Fatalf("mảng: %+v", l)
	}
	if l := toEvents([]byte(`{"id":"Sys9","at":5,"lvl":2}`)); len(l) != 1 || l[0].ID != "Sys9" {
		t.Fatalf("một mục: %+v", l)
	}
	if l := toEvents([]byte("")); l != nil {
		t.Fatalf("rỗng: %+v", l)
	}
}
