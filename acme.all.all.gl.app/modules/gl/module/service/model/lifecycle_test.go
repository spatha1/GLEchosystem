package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureMap is the GL-TEST-001 configuration as a map, to extend in a test.
func fixtureMap(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "fixtures", "GL-TEST-001.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// withWriteOff adds a synthetic write-off event to GL-TEST-001 (its E4): the amount is the
// invoice's open balance read from the ledger, never sent by the source. This exercises the
// mechanism only; no write-off limit or client treatment is implied.
func withWriteOff(c map[string]any, amount string) map[string]any {
	c["events"] = append(c["events"].([]any), map[string]any{
		"code": "INVOICE_WRITTEN_OFF", "description": "Invoice balance written off", "dimensions": []any{"COMPANY"}, "economicKey": []any{"INVOICE_NO"}})
	c["sourceAuthority"] = append(c["sourceAuthority"].([]any), map[string]any{"eventType": "INVOICE_WRITTEN_OFF", "intakeMode": "RAW_EVENT", "measure": "*", "sourceSystem": "BILLING"})
	c["derivedMeasures"] = append(c["derivedMeasures"].([]any), map[string]any{"code": "WO_AMT", "eventTypes": []any{"INVOICE_WRITTEN_OFF"}, "formula": amount, "resultType": "AMOUNT"})
	c["postingRules"] = append(c["postingRules"].([]any), map[string]any{"code": "PR_WRITE_OFF", "eventType": "INVOICE_WRITTEN_OFF", "rank": 10, "lines": []any{
		map[string]any{"code": "WO_DR", "class": "WO", "side": "D", "amount": "WO_AMT"},
		map[string]any{"code": "AR1_CR", "class": "AR1", "side": "C", "amount": "WO_AMT"}}})
	return c
}

func writeOff(invoice, day string) LifecycleEvent {
	return LifecycleEvent{Code: "WO_" + invoice, Input: EventInput{EventType: "INVOICE_WRITTEN_OFF", Currency: "AUD", TransactionDate: day, EffectiveDate: day,
		Keys: map[string]string{"INVOICE_NO": invoice}, Dimensions: map[string]string{"COMPANY": "C01"}}}
}

// scenarioEvents are the fixture's scenarios as lifecycle events, in the order given.
func scenarioEvents(t *testing.T, d *Document, codes ...string) []LifecycleEvent {
	t.Helper()
	var out []LifecycleEvent
	for _, code := range codes {
		s, ok := FindScenario(d, code)
		if !ok {
			t.Fatalf("scenario %s missing", code)
		}
		in := s.Input
		in.EventType = s.EventType
		out = append(out, LifecycleEvent{Code: code, Input: in, Scenario: code})
	}
	return out
}

func balanceOf(bs []LedgerBalance, account, value string) string {
	for _, b := range bs {
		if b.Account == account && b.Value == value {
			return b.Balance
		}
	}
	return "none"
}

// ALTB-04: a lifecycle runs each event through the same engine as a one-event preview: the
// lines are identical, and the temporary ledger carries the running balances.
func TestLifecycleUsesTheSameEngineAsAPreview(t *testing.T) {
	d := docOf(t, fixtureMap(t))
	events := scenarioEvents(t, d, "E1", "E2", "E3")
	res := PreviewLifecycle(d, fixtureRef, events)
	if !res.Complete || res.Stopped != "" || res.Lines != 12 || res.Entries != 4 || res.Posted {
		t.Fatalf("E1–E3: complete %v stopped %q lines %d entries %d posted %v", res.Complete, res.Stopped, res.Lines, res.Entries, res.Posted)
	}
	for i, e := range events {
		single, err := PreviewPosting(d, fixtureRef, e.Input)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res.Steps[i].Plan.Lines, single.Lines) {
			t.Errorf("%s: lifecycle lines differ from the one-event preview", e.Code)
		}
	}
	after := res.Steps[2].Balances
	for _, c := range []struct{ account, value, want string }{
		{"9948523", "P1", "0.00"},   // policy receivable: invoiced in full
		{"9948600", "INV1", "0.00"}, // INV1 paid
		{"9948600", "INV2", "0.50"}, // INV2: 0.50 left
		{"1500109", "", "1099.50"},  // C01 due from C09
		{"2500101", "", "-1099.50"}, // C09 due to C01
		{"1000900", "", "1099.50"},  // cash in C09
	} {
		if got := balanceOf(after, c.account, c.value); got != c.want {
			t.Errorf("after E3, %s %s: %s, want %s", c.account, c.value, got, c.want)
		}
	}
}

// ALTB-04: OPEN_BALANCE reads what earlier events posted, as of the event's book date.
func TestOpenBalanceReadsTheTemporaryLedger(t *testing.T) {
	d := docOf(t, withWriteOff(fixtureMap(t), "OPEN_BALANCE('AR1', 'INVOICE_NO')"))
	events := append(scenarioEvents(t, d, "E1", "E2", "E3"), writeOff("INV2", "2026-01-15"))
	res := PreviewLifecycle(d, fixtureRef, events)
	if !res.Complete {
		t.Fatalf("stopped: %s", res.Stopped)
	}
	wo := res.Steps[3].Plan
	if got := legs(wo); !reflect.DeepEqual(got, []string{"WO C 0.50 C01", "AR1 C 0.50 C01"}) && !reflect.DeepEqual(got, []string{"WO D 0.50 C01", "AR1 C 0.50 C01"}) {
		t.Errorf("the write-off clears the 0.50 left on INV2: %v", got)
	}
	if got := balanceOf(res.Steps[3].Balances, "9948600", "INV2"); got != "0.00" {
		t.Errorf("INV2 after the write-off: %s", got)
	}
	// Dated before the receipt, the same write-off reads the whole invoice.
	early := PreviewLifecycle(d, fixtureRef, append(scenarioEvents(t, d, "E1", "E2", "E3"), writeOff("INV2", "2026-01-09")))
	if got := legs(early.Steps[3].Plan); len(got) != 2 || !strings.Contains(got[0], "550.00") {
		t.Errorf("as of 9 Jan INV2 is still 550.00 open: %v", got)
	}
	// Nothing left on INV1: every amount is zero. The engine refuses a plan with no lines, as a
	// posting run would, so the lifecycle stops there rather than passing over it.
	zero := PreviewLifecycle(d, fixtureRef, append(scenarioEvents(t, d, "E1", "E2", "E3"), writeOff("INV1", "2026-01-15")))
	if zero.Complete || len(zero.Steps) != 4 || zero.Steps[3].Error == nil || zero.Steps[3].Error.Code != "UNBALANCED" ||
		!strings.Contains(zero.Steps[3].Error.Message, "produced no lines") {
		t.Errorf("a zero write-off is refused as the engine refuses it: %q", zero.Stopped)
	}
}

// ALTB-04: a one-event preview has no ledger, so OPEN_BALANCE is refused, never guessed.
func TestOpenBalanceIsRefusedWithoutALedger(t *testing.T) {
	d := docOf(t, withWriteOff(fixtureMap(t), "OPEN_BALANCE('AR1', 'INVOICE_NO')"))
	_, err := PreviewPosting(d, fixtureRef, writeOff("INV2", "2026-01-15").Input)
	if err == nil || !strings.Contains(err.Error(), "LEDGER_STATE_UNAVAILABLE") {
		t.Errorf("one-event preview of a ledger read: %v", err)
	}
}

// ALTB-04: OPEN_BALANCE's literal arguments are checked: a real class and a real business key.
func TestOpenBalanceArgumentsAreValidated(t *testing.T) {
	for formula, code := range map[string]string{
		"OPEN_BALANCE('NOPE', 'INVOICE_NO')": "UNKNOWN_CLASS",
		"OPEN_BALANCE('AR1', 'NOPE')":        "UNKNOWN_KEY",
	} {
		if fs := validateMap(withWriteOff(fixtureMap(t), formula)); !hasCode(fs, code) {
			t.Errorf("%s: want %s, got %v", formula, code, findingCodes(fs))
		}
	}
	if fs := validateMap(withWriteOff(fixtureMap(t), "OPEN_BALANCE('AR1', 'INVOICE_NO')")); len(errorsOf(fs)) > 0 {
		t.Errorf("a valid ledger read: %+v", errorsOf(fs))
	}
}

// ALTB-04: the lifecycle stops at the first event that cannot be posted, since later events
// may depend on it, and says why.
func TestLifecycleStopsAtAnEventThatCannotPost(t *testing.T) {
	d := docOf(t, fixtureMap(t))
	res := PreviewLifecycle(d, fixtureRef, scenarioEvents(t, d, "E3_SAME_COMPANY", "E1"))
	if res.Complete || len(res.Steps) != 1 || res.Steps[0].Error == nil || res.Steps[0].Error.Code != "ENTITY_NOT_ALLOWED" {
		t.Fatalf("stops at E3_SAME_COMPANY: %+v", res)
	}
	if !strings.Contains(res.Stopped, "step 1 (E3_SAME_COMPANY)") {
		t.Errorf("says where it stopped: %q", res.Stopped)
	}
}

// ALTB-04: an open balance is signed by the class's normal side, so a credit-normal class
// (premium written, tax payable) reads positive just as a receivable does.
func TestOpenBalanceIsSignedByTheNormalSide(t *testing.T) {
	d := docOf(t, fixtureMap(t))
	e1 := scenarioEvents(t, d, "E1")[0]
	p, err := PreviewPosting(d, fixtureRef, e1.Input)
	if err != nil {
		t.Fatal(err)
	}
	led := NewTempLedger(d)
	if err := led.Post(1, p); err != nil {
		t.Fatal(err)
	}
	day, _ := ParseDay(p.BookDate)
	sides := map[string]bool{}
	for _, l := range p.Lines {
		got, err := led.OpenBalance(l.Class, "POLICY_NO", l.Keys["POLICY_NO"], l.Entity, l.Currency, day)
		if err != nil {
			t.Fatal(err)
		}
		if got.Sign() <= 0 || decimalOf(got, l.Currency) != l.Amount {
			t.Errorf("%s (%s, normal %s): open balance %s, want +%s", l.Class, l.Side, l.NormalSide, decimalOf(got, l.Currency), l.Amount)
		}
		sides[l.Side] = true
	}
	if !sides["D"] || !sides["C"] {
		t.Fatalf("E1 should have debit- and credit-normal lines: %v", sides)
	}
	if _, err := led.OpenBalance("NOPE", "POLICY_NO", "P1", "C01", "AUD", day); ErrorCode(err) != "UNKNOWN_CLASS" {
		t.Errorf("unknown class: %v", err)
	}
}

// ALTB-04: a ledger read is ledger state in the manifest: listed so the dependency is visible,
// fed by the business key it is read for (which becomes mandatory), and never requested.
func TestALedgerReadIsLedgerStateInTheManifest(t *testing.T) {
	inFormula := withWriteOff(fixtureMap(t), "OPEN_BALANCE('AR1', 'INVOICE_NO')")
	inLine := withWriteOff(fixtureMap(t), "0")
	rules := inLine["postingRules"].([]any)
	for _, l := range rules[len(rules)-1].(map[string]any)["lines"].([]any) {
		l.(map[string]any)["amount"] = "OPEN_BALANCE('AR1', 'INVOICE_NO')"
	}
	for name, c := range map[string]map[string]any{"in a derived measure": inFormula, "in a line amount": inLine} {
		d := docOf(t, c)
		m, x := CompileManifest(d, "T", fixtureRef)
		fields := map[string]ManifestField{}
		for _, e := range m.Events {
			if e.EventType == "INVOICE_WRITTEN_OFF" {
				for _, f := range e.Fields {
					fields[f.FieldCode] = f
				}
			}
		}
		ob, ok := fields["OPEN_BALANCE_AR1_INVOICE_NO"]
		if !ok || ob.Kind != "LEDGER_STATE" || !ob.Mandatory || !reflect.DeepEqual(ob.DerivedFrom, []string{"INVOICE_NO"}) || len(ob.RequiredBy) == 0 {
			t.Errorf("%s: the ledger read: %+v", name, ob)
		}
		if k := fields["INVOICE_NO"]; k.Kind != "SOURCE" || !k.Mandatory {
			t.Errorf("%s: the key it is read for: %+v", name, k)
		}
		for _, f := range fields {
			if f.Kind == "SOURCE" && strings.Contains(f.FieldCode, "BALANCE") {
				t.Errorf("%s: a balance is requested from the source: %+v", name, f)
			}
		}
		if name == "in a derived measure" && !reflect.DeepEqual(fields["WO_AMT"].DerivedFrom, []string{"OPEN_BALANCE_AR1_INVOICE_NO"}) {
			t.Errorf("WO_AMT is derived from the ledger read: %+v", fields["WO_AMT"])
		}
		found := false
		for _, e := range x.Events {
			for _, f := range e.Fields {
				found = found || (f.FieldCode == "OPEN_BALANCE_AR1_INVOICE_NO" && f.Kind == "LEDGER_STATE" && len(f.Reasons) > 0)
			}
		}
		if !found {
			t.Errorf("%s: the explanation lists the ledger read with its reasons", name)
		}
	}
}

// ALTB-08: a scenario that reads earlier postings cannot be checked on its own (LIFECYCLE, not
// a failure); as a lifecycle step after the scenarios it depends on, its expectation is checked.
func TestLedgerReadingScenariosAreCheckedInTheirLifecycle(t *testing.T) {
	c := withWriteOff(fixtureMap(t), "OPEN_BALANCE('AR1', 'INVOICE_NO')")
	in := map[string]any{"currency": "AUD", "transactionDate": "2026-01-15", "effectiveDate": "2026-01-15", "keys": map[string]any{"INVOICE_NO": "INV2"},
		"dimensions": map[string]any{"COMPANY": "C01"}, "sourceSystem": "BILLING", "sourceEventId": "WO1", "sourceEventVersion": 1}
	legs := func(amount string) []any {
		return []any{map[string]any{"class": "WO", "side": "D", "amount": amount}, map[string]any{"class": "AR1", "side": "C", "amount": amount}}
	}
	c["scenarios"] = append(c["scenarios"].([]any),
		map[string]any{"code": "WO_INV2", "eventType": "INVOICE_WRITTEN_OFF", "input": in, "expect": map[string]any{"rule": "PR_WRITE_OFF", "legs": legs("0.50")}},
		map[string]any{"code": "WO_INV2_WRONG", "eventType": "INVOICE_WRITTEN_OFF", "input": in, "expect": map[string]any{"rule": "PR_WRITE_OFF", "legs": legs("9.99")}})
	d := docOf(t, c)
	for _, r := range RunScenarios(d, fixtureRef, []string{"WO_INV2", "E1"}) {
		want := map[string]string{"WO_INV2": "LIFECYCLE", "E1": "PASSED"}[r.Code]
		if r.Status != want || len(r.Mismatches) != 0 {
			t.Errorf("%s on its own: %s %v, want %s", r.Code, r.Status, r.Mismatches, want)
		}
	}
	for code, want := range map[string]string{"WO_INV2": "PASSED", "WO_INV2_WRONG": "FAILED"} {
		res := PreviewLifecycle(d, fixtureRef, append(scenarioEvents(t, d, "E1", "E2", "E3"), scenarioEvents(t, d, code)...))
		for i, st := range res.Steps {
			if st.Check == nil || (i < 3 && st.Check.Status != "PASSED") {
				t.Fatalf("%s: step %d check %+v", code, i+1, st.Check)
			}
		}
		if got := res.Steps[3].Check; got.Status != want || (want == "FAILED" && len(got.Mismatches) == 0) || got.Plan != nil {
			t.Errorf("%s in its lifecycle: %s %v", code, got.Status, got.Mismatches)
		}
	}
	// A step that is not a scenario is not checked.
	if res := PreviewLifecycle(d, fixtureRef, []LifecycleEvent{{Code: "E1", Input: scenarioEvents(t, d, "E1")[0].Input}}); res.Steps[0].Check != nil {
		t.Errorf("a synthetic step is checked: %+v", res.Steps[0].Check)
	}
}
