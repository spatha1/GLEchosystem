package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// schemaCheck checks an object against one level of a JSON schema: its required properties,
// no property it does not define (additionalProperties: false), and each string property's
// pattern, const or enum, following $ref to $defs.
func schemaCheck(t *testing.T, where string, schema, defs map[string]any, obj map[string]any) {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	for _, r := range schema["required"].([]any) {
		if _, ok := obj[r.(string)]; !ok {
			t.Errorf("%s: required %s missing", where, r)
		}
	}
	for k, v := range obj {
		p, ok := props[k].(map[string]any)
		if !ok {
			t.Errorf("%s: %s is not in the schema", where, k)
			continue
		}
		if ref, ok := p["$ref"].(string); ok {
			p = defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		}
		s, isString := v.(string)
		if c, ok := p["const"]; ok && v != c {
			t.Errorf("%s: %s = %v, want %v", where, k, v, c)
		}
		if e, ok := p["enum"].([]any); ok {
			found := false
			for _, x := range e {
				found = found || x == v
			}
			if !found {
				t.Errorf("%s: %s = %v not in %v", where, k, v, e)
			}
		}
		if pat, ok := p["pattern"].(string); ok && (!isString || !regexp.MustCompile(pat).MatchString(s)) {
			t.Errorf("%s: %s = %v does not match %s", where, k, v, pat)
		}
	}
}

func checkC6(t *testing.T, entries []JournalEntry) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(spec, "design", "02_canonical_contracts", "schemas", "journal-entry.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	lineSchema := schema["properties"].(map[string]any)["lines"].(map[string]any)["items"].(map[string]any)
	b, _ := json.Marshal(entries)
	var objs []map[string]any
	if err := json.Unmarshal(b, &objs); err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		schemaCheck(t, o["journalId"].(string), schema, defs, o)
		for _, l := range o["lines"].([]any) {
			schemaCheck(t, l.(map[string]any)["lineId"].(string), lineSchema, defs, l.(map[string]any))
		}
	}
}

func journalsOf(t *testing.T, d *Document, code string) ([]JournalEntry, []LedgerLine) {
	t.Helper()
	e := scenarioEvents(t, d, code)[0]
	led := NewTempLedger(d)
	p, err := PreviewPostingWith(d, fixtureRef, e.Input, led)
	if err != nil {
		t.Fatal(err)
	}
	js, lines, err := Journals(d, "CLIENT_A", e.Input, p)
	if err != nil {
		t.Fatal(err)
	}
	return js, lines
}

// ALTB-05: a plan becomes C6 journal entries — one per legal entity and currency, linked by one
// group, each balanced, with stable line ids and the schema's shape.
func TestJournalsFollowTheC6Contract(t *testing.T) {
	d := docOf(t, fixtureMap(t))
	e1, _ := journalsOf(t, d, "E1")
	e2, l2 := journalsOf(t, d, "E2")
	e3, _ := journalsOf(t, d, "E3")
	checkC6(t, append(append(append([]JournalEntry{}, e1...), e2...), e3...))
	if len(e1) != 1 || len(e2) != 1 || len(e3) != 2 {
		t.Fatalf("entries: E1 %d, E2 %d, E3 %d (E3 spans C01 and C09)", len(e1), len(e2), len(e3))
	}
	if e3[0].GroupID != e3[1].GroupID || e3[0].LegalEntity == e3[1].LegalEntity {
		t.Errorf("E3's entries share one group, one per entity: %s %s / %s %s", e3[0].GroupID, e3[0].LegalEntity, e3[1].GroupID, e3[1].LegalEntity)
	}
	if e2[0].EconomicEventKey != "INVOICED:"+strings.Join(econValues(d, "INVOICED", scenarioEvents(t, d, "E2")[0].Input), ":") {
		t.Errorf("economic key: %s", e2[0].EconomicEventKey)
	}
	if e2[0].AccountingPeriod != "2026-01" || e2[0].BookingDate != "2026-01-05" || e2[0].IntakeMode != "RAW_EVENT" || e2[0].Status != "posted" || e2[0].ConfigVersion != 1 {
		t.Errorf("E2 header: %+v", e2[0])
	}
	sub := map[string]bool{}
	for _, l := range e2[0].Lines {
		sub[l.SubledgerKey] = true
		if l.RuleID != "PR_INVOICED" || l.RuleVersion != 1 || len(l.SourceRowIDs) != 1 || !strings.HasPrefix(l.SourceRowIDs[0], "BILLING:E2#") {
			t.Errorf("E2 line: %+v", l)
		}
	}
	if !sub["INVOICE_NO:INV1"] || !sub["INVOICE_NO:INV2"] {
		t.Errorf("subledger keys: %v", sub)
	}
	if len(l2) != 4 || l2[0].BookDate != "2026-01-05" || l2[0].Keys["INVOICE_NO"] == "" {
		t.Errorf("ledger lines keep what OPEN_BALANCE reads: %+v", l2)
	}
	again, _ := journalsOf(t, d, "E2")
	for i := range again[0].Lines {
		if again[0].Lines[i].LineID != e2[0].Lines[i].LineID {
			t.Errorf("line ids are stable across retries: %s vs %s", again[0].Lines[i].LineID, e2[0].Lines[i].LineID)
		}
	}
	ids := map[string]bool{}
	for _, e := range append(append(e1, e2...), e3...) {
		for _, l := range e.Lines {
			if ids[l.LineID] {
				t.Errorf("duplicate line id %s", l.LineID)
			}
			ids[l.LineID] = true
		}
	}
}

func econValues(d *Document, event string, in EventInput) []string {
	var out []string
	for _, e := range d.Events {
		if e.Code == event {
			for _, k := range e.EconomicKey {
				out = append(out, in.Keys[k])
			}
		}
	}
	return out
}

// ALTB-05: a plan that cannot post never becomes a journal.
func TestJournalsRefuseWhatCannotPost(t *testing.T) {
	d := docOf(t, fixtureMap(t))
	e := scenarioEvents(t, d, "E3_SAME_COMPANY")[0]
	p, _ := PreviewPosting(d, fixtureRef, e.Input)
	if _, _, err := Journals(d, "CLIENT_A", e.Input, p); ErrorCode(err) != "MISSING_ACCOUNT" {
		t.Errorf("a line without a usable account: %v", err)
	}
	ok := scenarioEvents(t, d, "E1")[0]
	p1, _ := PreviewPosting(d, fixtureRef, ok.Input)
	p1.Balanced = false
	if _, _, err := Journals(d, "CLIENT_A", ok.Input, p1); ErrorCode(err) != "UNBALANCED" {
		t.Errorf("an unbalanced plan: %v", err)
	}
	if _, _, err := Journals(d, "CLIENT_A", ok.Input, &PostingPlan{}); ErrorCode(err) != "UNBALANCED" {
		t.Errorf("an empty plan: %v", err)
	}
}

// ALTB-05: a ledger loaded with posted lines reads exactly as one that posted them: what a
// posting run reads back from the store equals what the preview carried in memory.
func TestALoadedLedgerReadsAsThePreviewLedger(t *testing.T) {
	d := docOf(t, withWriteOff(fixtureMap(t), "OPEN_BALANCE('AR1', 'INVOICE_NO')"))
	events := scenarioEvents(t, d, "E1", "E2", "E3")
	preview := PreviewLifecycle(d, fixtureRef, append(events, writeOff("INV2", "2026-01-15")))
	if !preview.Complete {
		t.Fatal(preview.Stopped)
	}
	// Post E1–E3 as a store would keep them, then run the write-off on a ledger loaded from them.
	var stored []LedgerLine
	for i, e := range events {
		_, lines, err := Journals(d, "CLIENT_A", e.Input, preview.Steps[i].Plan)
		if err != nil {
			t.Fatal(err)
		}
		stored = append(stored, lines...)
	}
	led := NewTempLedger(d)
	if err := led.Load(stored); err != nil {
		t.Fatal(err)
	}
	later := RunLifecycle(d, fixtureRef, []LifecycleEvent{writeOff("INV2", "2026-01-15")}, led)
	if !later.Complete || legs(later.Steps[0].Plan)[0] != legs(preview.Steps[3].Plan)[0] {
		t.Fatalf("write-off after loading: %v, in the preview: %v", legs(later.Steps[0].Plan), legs(preview.Steps[3].Plan))
	}
	if got, want := later.Steps[0].Balances, preview.Steps[3].Balances; len(got) != len(want) {
		t.Errorf("balances after loading: %d rows, preview %d", len(got), len(want))
	}
	if err := NewTempLedger(d).Load([]LedgerLine{{LineID: "x", Amount: "1.00"}}); ErrorCode(err) != "MISSING_BOOK_DATE" {
		t.Errorf("a stored line without a book date: %v", err)
	}
}
