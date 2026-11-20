package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// spec is the repository's specification folder, relative to this package.
var spec = filepath.Join("..", "..", "..", "..", "..", "..", "acme.all.all.gl.spec")

// legKey is what a journal leg is, independent of ids and wording.
func legKey(entity, account, class, side, amount string) string {
	return entity + " " + account + " " + class + " " + side + " " + amount
}

// walkthroughLegs reads the hand-written expected journals of the contract walk-through (C6).
func walkthroughLegs(t *testing.T, file string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "contracts", "walkthrough", file))
	if err != nil {
		t.Fatal(err)
	}
	var journals []struct {
		LegalEntity string `json:"legalEntity"`
		Lines       []struct {
			Account, AccountingClass, Side, Amount string
		} `json:"lines"`
	}
	if err := json.Unmarshal(b, &journals); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	var out []string
	for _, j := range journals {
		for _, l := range j.Lines {
			out = append(out, legKey(j.LegalEntity, l.Account, l.AccountingClass, l.Side, l.Amount))
		}
	}
	sort.Strings(out)
	return out
}

// GL-TEST-001 regression: the canonical demo configuration (spec fixture, also the UI's dev
// seed) validates cleanly, and the engine's previews of E1, E2 and E3 produce exactly the legs
// of the hand-written walk-through journals — accounts, classes, sides, amounts, entities.
func TestGLTest001ConfigurationReproducesTheWalkthroughJournals(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "fixtures", "GL-TEST-001.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, fs := Parse(b)
	fs = append(fs, Validate(doc)...)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("the demo configuration must validate: %+v", e)
	}
	e2 := e2In()
	e2.Dimensions["LOB"] = "MOTOR"
	for _, c := range []struct {
		name    string
		in      EventInput
		journal string
	}{
		{"E1", e1In(), "e1-premium.journal.json"},
		{"E2", e2, "e2-invoice.journal.json"},
		{"E3", e3In("BANK-C09"), "e3-payment.journal.json"},
	} {
		p, err := PreviewPosting(doc, demoRef, c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var got []string
		for _, l := range p.Lines {
			got = append(got, legKey(l.Entity, l.Account, l.Class, l.Side, l.Amount))
		}
		sort.Strings(got)
		if want := walkthroughLegs(t, c.journal); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, want)
		}
	}
}
