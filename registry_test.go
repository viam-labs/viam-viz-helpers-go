package visuals

import (
	"testing"
)

func clearRegistry() {
	for _, n := range RegisteredNames() {
		Unregister(n)
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	clearRegistry()
	defer clearRegistry()
	obj := struct{ x int }{42}
	Register("vis", obj)
	got := Lookup("vis")
	if got != obj {
		t.Errorf("expected %v, got %v", obj, got)
	}
}

func TestRegistry_LookupMissingReturnsNil(t *testing.T) {
	clearRegistry()
	if Lookup("nope") != nil {
		t.Error("expected nil for missing name")
	}
}

func TestRegistry_RegisterReplacesPriorValue(t *testing.T) {
	clearRegistry()
	defer clearRegistry()
	Register("vis", "first")
	Register("vis", "second")
	if Lookup("vis") != "second" {
		t.Errorf("expected 'second', got %v", Lookup("vis"))
	}
}

func TestRegistry_UnregisterRemoves(t *testing.T) {
	clearRegistry()
	defer clearRegistry()
	Register("vis", "v")
	Unregister("vis")
	if Lookup("vis") != nil {
		t.Error("expected nil after unregister")
	}
}

func TestRegistry_UnregisterMissingIsNoOp(t *testing.T) {
	clearRegistry()
	Unregister("never_registered") // must not panic
}

func TestRegistry_RegisteredNamesSorted(t *testing.T) {
	clearRegistry()
	defer clearRegistry()
	Register("zeta", 1)
	Register("alpha", 2)
	Register("mu", 3)
	got := RegisteredNames()
	want := []string{"alpha", "mu", "zeta"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
