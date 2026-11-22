package model

import (
	"reflect"
	"testing"
	"time"
)

// withReversingPremium gives GL-TEST-001 journal types and posts premium (E1) as a reversing
// journal, like a month-end level.
func withReversingPremium(c map[string]any) map[string]any {
	c["journalTypes"] = []any{map[string]any{"code": "GJ", "description": "General journal"},
		map[string]any{"code": "RJ", "description": "Reversing journal", "reverse": "NEXT_PERIOD"}}
	for _, r := range c["postingRules"].([]any) {
		rule := r.(map[string]any)
		rule["journalType"] = map[bool]string{true: "RJ", false: "GJ"}[rule["code"] == "PR_PREM_ADDED"]
	}
	return c
}

func balanceAt(bs []LedgerBalance, account string) string { return balanceOf(bs, account, "") }

// ALTB-09: a reversing journal is reversed on the first day of the next period, as its own entry
// linked to the original; balances as at a date count the reversal only from that date.
func TestReversingJournalsAreReversedNextPeriod(t *testing.T) {
	d := docOf(t, withReversingPremium(fixtureMap(t)))
	e1 := scenarioEvents(t, d, "E1")[0]
	p, err := PreviewPosting(d, fixtureRef, e1.Input)
	if err != nil {
		t.Fatal(err)
	}
	if p.JournalType != "RJ" {
		t.Fatalf("plan journal type %q", p.JournalType)
	}
	js, kept, err := Journals(d, "CLIENT_A", e1.Input, p)
	if err != nil {
		t.Fatal(err)
	}
	checkC6(t, js)
	if len(js) != 2 || js[1].ReversalOf != js[0].JournalID || js[1].BookingDate != "2026-02-01" || js[1].AccountingPeriod != "2026-02" || js[0].JournalType != "RJ" {
		t.Fatalf("original and reversal: %+v", js)
	}
	for i, l := range js[0].Lines {
		if r := js[1].Lines[i]; r.Account != l.Account || r.Amount != l.Amount || r.Side == l.Side || r.LineID != l.LineID+"/REV" {
			t.Errorf("reversal line %d: %+v of %+v", i, r, l)
		}
	}
	led := NewTempLedger(d)
	if err := led.Post(1, p); err != nil {
		t.Fatal(err)
	}
	jan, _ := ParseDay("2026-01-31")
	feb, _ := ParseDay("2026-02-01")
	if got := balanceAt(led.BalancesAsOf(jan), "4100100"); got != "-1000.00" {
		t.Errorf("premium as at 31 Jan: %s", got)
	}
	if got := balanceAt(led.BalancesAsOf(feb), "4100100"); got != "0.00" {
		t.Errorf("premium as at 1 Feb (reversed): %s", got)
	}
	if !led.LastBookDate().Equal(mustDay(t, "2026-01-01")) {
		t.Errorf("the last book date ignores the reversal: %v", led.LastBookDate())
	}
	// What a store keeps reads back the same, reversal included.
	stored := NewTempLedger(d)
	if err := stored.Load(kept); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.BalancesAsOf(feb), led.BalancesAsOf(feb)) || !stored.LastBookDate().Equal(led.LastBookDate()) {
		t.Errorf("a loaded ledger differs from the posting ledger")
	}
	// A lifecycle shows the ledger as at the last event, before the reversal.
	res := PreviewLifecycle(d, fixtureRef, scenarioEvents(t, d, "E1"))
	if got := balanceAt(res.Steps[0].Balances, "4100100"); got != "-1000.00" {
		t.Errorf("lifecycle balance after E1: %s", got)
	}
	// A general journal is not reversed.
	e2 := scenarioEvents(t, d, "E2")[0]
	p2, _ := PreviewPosting(d, fixtureRef, e2.Input)
	if js2, _, _ := Journals(d, "CLIENT_A", e2.Input, p2); len(js2) != 1 || js2[0].JournalType != "GJ" {
		t.Errorf("GJ entries: %+v", js2)
	}
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	x, ok := ParseDay(s)
	if !ok {
		t.Fatal(s)
	}
	return x
}

// ALTB-09: journal types and the rules naming them are checked.
func TestJournalTypesAreValidated(t *testing.T) {
	c := withReversingPremium(fixtureMap(t))
	c["postingRules"].([]any)[0].(map[string]any)["journalType"] = "XX"
	if fs := validateMap(c); !hasCode(fs, "UNKNOWN_JOURNAL_TYPE") {
		t.Errorf("unknown journal type: %v", findingCodes(fs))
	}
	c = withReversingPremium(fixtureMap(t))
	c["journalTypes"].([]any)[1].(map[string]any)["reverse"] = "LATER"
	if fs := validateMap(c); !hasCode(fs, "INVALID_REVERSAL") {
		t.Errorf("invalid reversal: %v", findingCodes(fs))
	}
	if fs := validateMap(withReversingPremium(fixtureMap(t))); len(errorsOf(fs)) > 0 {
		t.Errorf("valid journal types: %+v", errorsOf(fs))
	}
}
