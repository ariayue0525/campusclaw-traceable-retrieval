package store

import "testing"

func TestFirstRunes(t *testing.T) {
	if got:=firstRunes("数学题ABC",3);got!="数学题"{t.Fatalf("got %q",got)}
	if got:=firstRunes("短句",24);got!="短句"{t.Fatalf("got %q",got)}
}
