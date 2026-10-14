package domain

import "testing"

func TestRoleCanMatrix(t *testing.T) {
	type c struct {
		r    Role
		a    Action
		want bool
	}
	cases := []c{
		{RoleOwner, ActionPurge, true},
		{RoleOwner, ActionMembers, true},
		{RoleEditor, ActionEdit, true},
		{RoleEditor, ActionTrash, true},
		{RoleEditor, ActionPurge, false},
		{RoleEditor, ActionMembers, false},
		{RoleViewer, ActionRead, true},
		{RoleViewer, ActionEdit, false},
		{RoleViewer, ActionCreate, false},
		{Role(""), ActionRead, false},
	}
	for _, x := range cases {
		if got := RoleCan(x.r, x.a); got != x.want {
			t.Errorf("RoleCan(%q,%q)=%v want %v", x.r, x.a, got, x.want)
		}
	}
}

func TestNewIDSortableAndPrefixed(t *testing.T) {
	a := NewID("doc")
	b := NewID("doc")
	if len(a) != len("doc_")+26 {
		t.Fatalf("unexpected id shape %q", a)
	}
	if a >= b {
		t.Errorf("ids must strictly increase: %q %q", a, b)
	}
	if NewID("x") == a {
		t.Error("collision")
	}
}
