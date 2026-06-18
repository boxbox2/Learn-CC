package command

import "testing"

func TestCompleteListsAllVisibleForSlash(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/status", Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/session", Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/secret", Hidden: true, Kind: KindReadOnly, Handler: noopHandler})
	reg.MustValidate()
	got := reg.Complete("/")
	if got.NoMatch || len(got.Items) != 2 {
		t.Fatalf("Complete(/) = %+v", got)
	}
}

func TestCompleteFiltersByNameAndAlias(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/status", Aliases: []string{"/st"}, Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/session", Kind: KindReadOnly, Handler: noopHandler})
	reg.MustValidate()
	got := reg.Complete("/st")
	if len(got.Items) != 1 || got.Items[0].Canonical != "/status" {
		t.Fatalf("Complete(/st) = %+v", got)
	}
}

func TestCompleteNoMatch(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/status", Kind: KindReadOnly, Handler: noopHandler})
	reg.MustValidate()
	got := reg.Complete("/zzz")
	if !got.NoMatch || len(got.Items) != 0 {
		t.Fatalf("Complete(/zzz) = %+v", got)
	}
}

func TestCompleteInactiveForNonSlashAndMultiline(t *testing.T) {
	reg := testRegistry(t)
	if got := reg.Complete("status"); got.NoMatch || len(got.Items) != 0 {
		t.Fatalf("Complete(non slash) = %+v", got)
	}
	if got := reg.Complete("/status\nmore"); got.NoMatch || len(got.Items) != 0 {
		t.Fatalf("Complete(multiline) = %+v", got)
	}
}
