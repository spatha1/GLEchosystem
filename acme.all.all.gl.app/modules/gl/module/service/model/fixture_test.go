package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

// ALTB-01: on GL-TEST-001, every E1–E3 line says which way it moves its class: the receivable
// allocations decrease AR1, everything else increases its class.
func TestGLTest001LinesSayTheirMovement(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "fixtures", "GL-TEST-001.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := Parse(b)
	e2 := e2In()
	e2.Dimensions["LOB"] = "MOTOR"
	for _, c := range []struct {
		name string
		in   EventInput
		want []string
	}{
		{"E1", e1In(), []string{"AR0 ASSET D D INCREASE", "PW0 INCOME C C INCREASE", "TX0 LIABILITY C C INCREASE"}},
		{"E2", e2, []string{"AR1 ASSET D D INCREASE", "AR1 ASSET D D INCREASE", "AR0 ASSET C D DECREASE", "AR0 ASSET C D DECREASE"}},
		{"E3", e3In("BANK-C09"), []string{"PR0 ASSET D D INCREASE", "IC1 LIABILITY C C INCREASE", "IC0 ASSET D D INCREASE", "AR1 ASSET C D DECREASE", "AR1 ASSET C D DECREASE"}},
	} {
		p, err := PreviewPosting(doc, demoRef, c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := movements(p); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s movements:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
}

// ALTB-02: golden regression for GL-TEST-001. The fixture's own E1, E2 and E3 scenarios, run by
// the engine, produce exactly these lines: entity, side, account, amount, subledger-key value,
// rule line and the assignment rule that chose the account. E1_HOME falls back to the default
// receivable account, and E3_SAME_COMPANY is refused because the C09 bank account cannot take a
// C01 line. Any change to these is a change to posted accounting and must be deliberate.
func TestGLTest001GoldenJournalLines(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "fixtures", "GL-TEST-001.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, fs := Parse(b)
	fs = append(fs, Validate(doc)...)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("the fixture must validate: %+v", e)
	}
	subKey := map[string]string{}
	for _, a := range doc.Accounts {
		subKey[a.Code] = a.SubledgerKey
	}
	golden := map[string][]string{
		"E1": {
			"C01 D 9948523 1100.00 P1 AR0_DR AA_AR0_MOTOR",
			"C01 C 4100100 1000.00 - PW0_CR AA_PW0",
			"C01 C 2200100 100.00 - TX0_CR AA_TX0",
		},
		"E1_HOME": {
			"C01 D 9948500 1100.00 P2 AR0_DR AA_AR0_DEFAULT",
			"C01 C 4100100 1000.00 - PW0_CR AA_PW0",
			"C01 C 2200100 100.00 - TX0_CR AA_TX0",
		},
		"E2": {
			"C01 D 9948600 550.00 INV1 AR1_DR AA_AR1",
			"C01 D 9948600 550.00 INV2 AR1_DR AA_AR1",
			"C01 C 9948523 550.00 P1 AR0_CR AA_AR0_MOTOR",
			"C01 C 9948523 550.00 P1 AR0_CR AA_AR0_MOTOR",
		},
		"E3": {
			"C09 D 1000900 1099.50 - CASH AA_PR0",
			"C09 C 2500101 1099.50 - DUE_TO AA_IC1",
			"C01 D 1500109 1099.50 - DUE_FROM AA_IC0",
			"C01 C 9948600 550.00 INV1 ALLOC AA_AR1",
			"C01 C 9948600 549.50 INV2 ALLOC AA_AR1",
		},
	}
	ref := ConfigRef{ConfigID: "GL_TEST_001", Version: 1, ContentDigest: "sha256:fixture"}
	seen := map[string]bool{}
	for _, r := range RunScenarios(doc, ref, []string{"E1", "E1_HOME", "E2", "E3", "E3_SAME_COMPANY"}) {
		seen[r.Code] = true
		if r.Status != "PASSED" {
			t.Errorf("%s: status %s, mismatches %v, error %+v", r.Code, r.Status, r.Mismatches, r.Error)
		}
		if r.Code == "E3_SAME_COMPANY" {
			if r.Error == nil || r.Error.Code != "ENTITY_NOT_ALLOWED" || !strings.Contains(r.Error.Message, "1000900") {
				t.Errorf("E3_SAME_COMPANY must be refused: the C09 bank account 1000900 cannot take a C01 line; got %+v", r.Error)
			}
			continue
		}
		if r.Plan == nil || !r.Plan.Balanced {
			t.Errorf("%s: no balanced plan: %+v", r.Code, r.Plan)
			continue
		}
		var got []string
		for _, l := range r.Plan.Lines {
			key := "-"
			if k := subKey[l.Account]; k != "" {
				key = l.Keys[k]
			}
			rule := "-"
			if l.Assignment != nil {
				rule = l.Assignment.Rule
			}
			got = append(got, strings.Join([]string{l.Entity, l.Side, l.Account, l.Amount, key, l.Template, rule}, " "))
		}
		if want := golden[r.Code]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s lines:\n got %v\nwant %v", r.Code, got, want)
		}
	}
	for _, c := range []string{"E1", "E1_HOME", "E2", "E3", "E3_SAME_COMPANY"} {
		if !seen[c] {
			t.Errorf("scenario %s missing from the fixture", c)
		}
	}
}
