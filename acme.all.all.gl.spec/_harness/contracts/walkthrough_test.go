package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const wt = "walkthrough"

func mustDecode(t *testing.T, name string, v any) {
	t.Helper()
	if err := DecodeStrict(filepath.Join(wt, name), v); err != nil {
		t.Fatal(err)
	}
}

func cents(t *testing.T, s string) int64 {
	t.Helper()
	n, err := Cents(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Every schema is valid JSON and pins its own contract version, so instances are self-describing.
func TestSchemasParseAndPinTheirContractVersion(t *testing.T) {
	want := map[string]string{
		"config-version.schema.json":  "gl.config/v1",
		"field-manifest.schema.json":  "gl.manifest/v1",
		"mapping-version.schema.json": "gl.mapping/v1",
		"financial-event.schema.json": "gl.event/v1",
		"source-journal.schema.json":  "gl.source-journal/v1",
		"journal-entry.schema.json":   "gl.journal/v1",
		"exception-case.schema.json":  "gl.exception/v1",
	}
	dir := filepath.Join("..", "..", "design", "02_canonical_contracts", "schemas")
	files, err := filepath.Glob(filepath.Join(dir, "*.schema.json"))
	if err != nil || len(files) != len(want) {
		t.Fatalf("expected %d schemas in %s, found %d (%v)", len(want), dir, len(files), err)
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		var s struct {
			Properties struct {
				SchemaVersion struct {
					Const string `json:"const"`
				} `json:"schemaVersion"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if got := s.Properties.SchemaVersion.Const; got != want[filepath.Base(f)] {
			t.Errorf("%s pins %q, want %q", filepath.Base(f), got, want[filepath.Base(f)])
		}
	}
}

// The walk-through events satisfy C4 and keep transaction and effective dates apart.
func TestEventsSatisfyTheEventContract(t *testing.T) {
	for _, f := range []string{"e1-premium.event.json", "e1-premium.billing-copy.event.json", "e2-invoice.event.json", "e3-payment.event.json"} {
		var e Event
		mustDecode(t, f, &e)
		if p := ValidateEvent(e); len(p) > 0 {
			t.Errorf("%s: %v", f, p)
		}
	}
}

// Negative: the contract refuses money as a float, extra decimals, a missing identity and unknown fields.
func TestTheEventContractRejectsBadInput(t *testing.T) {
	var good Event
	mustDecode(t, "e3-payment.event.json", &good)
	cases := map[string]func(e *Event){
		"float-looking money": func(e *Event) { e.Measures["PAY_AMT"] = "1.0995e3" },
		"three decimals":      func(e *Event) { e.Measures["PAY_AMT"] = "1099.505" },
		"no economic key":     func(e *Event) { e.EconomicEventKey = "" },
		"no lineage":          func(e *Event) { e.SourceRowIDs = nil },
		"duplicate list item": func(e *Event) { e.Lists["ALLOCATION_LIST"][1].ItemID = "A1" },
	}
	for name, mutate := range cases {
		e := good
		e.Measures = map[string]string{"PAY_AMT": good.Measures["PAY_AMT"]}
		e.Lists = map[string][]ListItem{"ALLOCATION_LIST": slices.Clone(good.Lists["ALLOCATION_LIST"])}
		mutate(&e)
		if len(ValidateEvent(e)) == 0 {
			t.Errorf("%s was accepted", name)
		}
	}
	tmp := filepath.Join(t.TempDir(), "extra.json")
	raw, _ := os.ReadFile(filepath.Join(wt, "e1-premium.event.json"))
	_ = os.WriteFile(tmp, []byte(strings.Replace(string(raw), `"tenantId"`, `"premiumAsNumber": 1000.0, "tenantId"`, 1)), 0o600)
	var e Event
	if err := DecodeStrict(tmp, &e); err == nil {
		t.Error("an unknown field was accepted by strict decoding")
	}
}

// AC: source authority is selectable per event + measure, and ambiguity is a configuration error.
func TestSourceAuthorityIsUnambiguousPerEventAndMeasure(t *testing.T) {
	for _, f := range []string{"authority.raw-events.json", "authority.source-journal.json"} {
		var s AuthoritySetup
		mustDecode(t, f, &s)
		if bad := ValidateAuthority(s); len(bad) > 0 {
			t.Errorf("%s: %v", f, bad)
		}
	}
	var s AuthoritySetup
	mustDecode(t, "authority.raw-events.json", &s)
	s.SourceAuthority = append(s.SourceAuthority, SourceAuthority{EventType: "PREM_ADDED", Measure: "PREMIUM", SourceSystem: "BILLING", IntakeMode: "RAW_EVENT"})
	if bad := ValidateAuthority(s); len(bad) != 1 || !strings.Contains(bad[0], "PREM_ADDED/PREMIUM has 2 authorities") {
		t.Errorf("a second authority for PREM_ADDED/PREMIUM was not rejected: %v", bad)
	}
}

// AC: overlapping sources never post twice. The same premium arrives from Policy and Billing; only
// the authoritative source posts, and each economic event posts exactly once.
func TestOverlappingSourcesPostEachEconomicEventOnce(t *testing.T) {
	var s AuthoritySetup
	mustDecode(t, "authority.raw-events.json", &s)
	posts := map[string][]string{}
	for _, f := range []string{"e1-premium.event.json", "e1-premium.billing-copy.event.json", "e2-invoice.event.json", "e3-payment.event.json"} {
		var e Event
		mustDecode(t, f, &e)
		if Authoritative(s, e.EventType, e.SourceSystem, "RAW_EVENT") {
			posts[e.EconomicEventKey] = append(posts[e.EconomicEventKey], e.SourceSystem+":"+e.SourceEventID)
		}
	}
	for key, by := range posts {
		if len(by) != 1 {
			t.Errorf("%s would post %d times: %v", key, len(by), by)
		}
	}
	if got := posts["PREMIUM:P1:T1"]; len(got) != 1 || got[0] != "POLICY:E1" {
		t.Errorf("premium must post once, from POLICY; got %v", got)
	}
	if len(posts) != 3 {
		t.Errorf("want 3 posting economic events (premium, invoicing, payment), got %d", len(posts))
	}
}

func balancedEntries(t *testing.T, name string) []JournalEntry {
	t.Helper()
	var js []JournalEntry
	mustDecode(t, name, &js)
	for _, j := range js {
		if j.SchemaVersion != "gl.journal/v1" || j.ConfigVersion < 1 || j.TenantID == "" {
			t.Errorf("%s: header incomplete", j.JournalID)
		}
		bal, err := Balance(j)
		if err != nil {
			t.Fatal(err)
		}
		if bal != 0 {
			t.Errorf("%s (%s/%s) is out of balance by %d cents", j.JournalID, j.LegalEntity, j.Currency, bal)
		}
	}
	return js
}

func lineage(t *testing.T, e Event, js []JournalEntry) {
	t.Helper()
	for _, j := range js {
		if j.EconomicEventKey != e.EconomicEventKey || j.MappingVersion != e.MappingVersion {
			t.Errorf("%s does not trace to event %s / mapping v%d", j.JournalID, e.EconomicEventKey, e.MappingVersion)
		}
		for _, l := range j.Lines {
			for _, r := range l.SourceRowIDs {
				if !slices.Contains(e.SourceRowIDs, r) {
					t.Errorf("%s cites source row %s that the event does not carry", l.LineID, r)
				}
			}
		}
	}
}

func sumBy(js []JournalEntry, class, side string) int64 {
	var n int64
	for _, j := range js {
		for _, l := range j.Lines {
			if l.AccountingClass == class && l.Side == side {
				c, _ := Cents(l.Amount)
				n += c
			}
		}
	}
	return n
}

// Evidence: walk the premium path — E1 → 3 balanced lines; AR0 = PREMIUM + TAX.
func TestWalkPremiumPath(t *testing.T) {
	var e Event
	mustDecode(t, "e1-premium.event.json", &e)
	js := balancedEntries(t, "e1-premium.journal.json")
	lineage(t, e, js)
	if len(js) != 1 || len(js[0].Lines) != 3 {
		t.Fatalf("premium: want 1 entry with 3 lines")
	}
	gross := cents(t, e.Measures["PREMIUM"]) + cents(t, e.Measures["TAX"])
	if got := sumBy(js, "AR0", "D"); got != gross || gross != 110000 {
		t.Errorf("AR0 debit %d, want PREMIUM+TAX = %d (1100.00)", got, gross)
	}
	if sumBy(js, "PW0", "C") != cents(t, e.Measures["PREMIUM"]) || sumBy(js, "TX0", "C") != cents(t, e.Measures["TAX"]) {
		t.Error("premium and tax credits must equal the PREMIUM and TAX measures")
	}
}

// Evidence: walk the invoice path — E2 expands INVOICE_LIST into one DR/CR pair per invoice.
func TestWalkInvoicePath(t *testing.T) {
	var e Event
	mustDecode(t, "e2-invoice.event.json", &e)
	js := balancedEntries(t, "e2-invoice.journal.json")
	lineage(t, e, js)
	items := e.Lists["INVOICE_LIST"]
	if len(js) != 1 || len(js[0].Lines) != 2*len(items) {
		t.Fatalf("invoicing: want %d lines (one pair per invoice)", 2*len(items))
	}
	var listTotal int64
	for _, it := range items {
		amt := cents(t, it.Measures["INVOICE_AMT"])
		listTotal += amt
		var ar1 int64
		for _, l := range js[0].Lines {
			if l.AccountingClass == "AR1" && l.Side == "D" && l.SubledgerKey == "INVOICE:"+it.Keys["INVOICE_NO"] {
				ar1 += cents(t, l.Amount)
			}
		}
		if ar1 != amt {
			t.Errorf("invoice %s: AR1 debit %d, want %d", it.ItemID, ar1, amt)
		}
	}
	if listTotal != cents(t, e.Measures["INVOICE_AMT"]) {
		t.Errorf("INVOICE_LIST totals %d but the event says %s", listTotal, e.Measures["INVOICE_AMT"])
	}
	if sumBy(js, "AR0", "C") != listTotal {
		t.Error("policy receivable must be relieved by exactly the invoiced amount")
	}
}

// Evidence: walk the payment path — E3 banked by C09 for a C01 policy: two entries, each balanced,
// intercompany nets to zero, allocations reduce each invoice, and PAY_AMT is never multiplied.
func TestWalkPaymentPath(t *testing.T) {
	var e Event
	mustDecode(t, "e3-payment.event.json", &e)
	js := balancedEntries(t, "e3-payment.journal.json")
	lineage(t, e, js)
	entities := map[string]bool{}
	lines := 0
	for _, j := range js {
		entities[j.LegalEntity] = true
		lines += len(j.Lines)
		if j.GroupID != "G:PAYMENT:R1" {
			t.Errorf("%s is not linked to the payment's posting group", j.JournalID)
		}
	}
	if len(js) != 2 || !entities["C01"] || !entities["C09"] || lines != 5 {
		t.Fatalf("payment: want 2 entries (C01, C09) with 5 lines, got %d entries / %d lines", len(js), lines)
	}
	if ic := sumBy(js, "IC0", "D") - sumBy(js, "IC1", "C"); ic != 0 {
		t.Errorf("intercompany due-from/due-to must net to zero, got %d", ic)
	}
	var alloc int64
	for _, it := range e.Lists["ALLOCATION_LIST"] {
		a := cents(t, it.Measures["ALLOC_AMT"])
		alloc += a
		var cr int64
		for _, j := range js {
			for _, l := range j.Lines {
				if l.AccountingClass == "AR1" && l.Side == "C" && l.SubledgerKey == "INVOICE:"+it.Keys["INVOICE_NO"] {
					cr += cents(t, l.Amount)
				}
			}
		}
		if cr != a {
			t.Errorf("allocation %s: AR1 credit %d, want %d", it.ItemID, cr, a)
		}
	}
	pay := cents(t, e.Measures["PAY_AMT"])
	if alloc != pay {
		t.Errorf("allocations total %d, PAY_AMT %d", alloc, pay)
	}
	if bank := sumBy(js, "PR0", "D"); bank != pay {
		t.Errorf("bank debit %d must equal PAY_AMT %d once, not once per allocation", bank, pay)
	}
}

// AC: existing journals are imported without a synthetic counter-leg, and a raw event plus its
// source journal cannot both post (setup B makes the Billing GL feed authoritative for payments).
func TestSourceJournalImportAddsNoCounterLegAndPostsOnce(t *testing.T) {
	var setup AuthoritySetup
	mustDecode(t, "authority.source-journal.json", &setup)
	var feed SourceJournalFeed
	mustDecode(t, "e3-payment.source-journals.json", &feed)
	var e Event
	mustDecode(t, "e3-payment.event.json", &e)

	if Authoritative(setup, e.EventType, e.SourceSystem, "RAW_EVENT") {
		t.Fatal("setup B: the raw Billing payment event must be reconciliation-only")
	}
	postings := 0
	for _, sj := range feed.Journals {
		if !Authoritative(setup, sj.EventType, sj.SourceSystem, "SOURCE_JOURNAL") {
			t.Fatalf("%s should be authoritative in setup B", sj.SourceJournalID)
		}
		j, err := ImportSourceJournal(sj, feed.AccountRefMap, "DEMO_GL", 1)
		if err != nil {
			t.Fatal(err)
		}
		postings++
		if len(j.Lines) != len(sj.Lines) {
			t.Errorf("%s: imported %d lines from %d source lines — a counter-leg was added or a line dropped", sj.SourceJournalID, len(j.Lines), len(sj.Lines))
		}
		for i, l := range j.Lines {
			src := sj.Lines[i]
			if l.SourceLineID != src.SourceLineID || l.Side != src.Side || l.Amount != src.Amount {
				t.Errorf("%s: line %d changed on import", sj.SourceJournalID, i)
			}
		}
		if j.IntakeMode != "SOURCE_JOURNAL" || j.SourceJournalID != sj.SourceJournalID {
			t.Errorf("%s: imported entry lost its source journal identity", sj.SourceJournalID)
		}
	}
	if postings != 2 {
		t.Errorf("want one imported entry per company journal (2), got %d", postings)
	}

	// Negative: an unbalanced source journal and an unmapped account reference are refused.
	bad := feed.Journals[1]
	bad.Lines = slices.Clone(bad.Lines)
	bad.Lines[2].Amount = "549.00"
	if _, err := ImportSourceJournal(bad, feed.AccountRefMap, "DEMO_GL", 1); err == nil || !strings.Contains(err.Error(), "UNBALANCED") {
		t.Errorf("an unbalanced source journal was not refused: %v", err)
	}
	unmapped := feed.Journals[0]
	unmapped.Lines = slices.Clone(unmapped.Lines)
	unmapped.Lines[0].AccountRef = "BGL-9999"
	if _, err := ImportSourceJournal(unmapped, feed.AccountRefMap, "DEMO_GL", 1); err == nil || !strings.Contains(err.Error(), "MISSING_ACCOUNT") {
		t.Errorf("an unmapped account reference was not refused: %v", err)
	}
}

// The acceptance fixture's stated totals are internally consistent (7 groups, 22 lines).
func TestGLTest001FixtureIsConsistent(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "fixtures", "GL-TEST-001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Synthetic bool `json:"synthetic"`
		Events    []struct {
			EventID string `json:"eventId"`
		} `json:"events"`
		Expect struct {
			JournalGroups int `json:"journalGroups"`
			JournalLines  int `json:"journalLines"`
		} `json:"expect"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	perGroup := map[string]int{"E1": 3, "E2": 4, "E3": 5, "E4": 2, "E5": 2, "E5R": 4, "E6": 2}
	total := 0
	for _, ev := range f.Events {
		n, ok := perGroup[ev.EventID]
		if !ok {
			t.Errorf("fixture event %s has no expected group size", ev.EventID)
		}
		total += n
	}
	if !f.Synthetic || len(f.Events) != f.Expect.JournalGroups || total != f.Expect.JournalLines {
		t.Errorf("fixture: %d events/groups, %d lines; expect %d groups, %d lines", len(f.Events), total, f.Expect.JournalGroups, f.Expect.JournalLines)
	}
}
