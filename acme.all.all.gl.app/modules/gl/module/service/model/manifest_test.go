package model

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

var fixtureRef = ConfigRef{ConfigID: "GL_TEST_001", Version: 1, ContentDigest: "sha256:" + strings.Repeat("0", 64)}

// fixtureContent is the GL-TEST-001 configuration as a mutable map.
func fixtureContent(t *testing.T) map[string]any {
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

// docOf parses and validates content; blocking findings fail the test.
func docOf(t *testing.T, c map[string]any) *Document {
	t.Helper()
	b, _ := json.Marshal(c)
	d, fs := Parse(b)
	fs = append(fs, Validate(d)...)
	for _, f := range fs {
		if f.Severity == "error" {
			t.Fatalf("configuration has a blocking finding: %+v", f)
		}
	}
	return d
}

func manifestOf(t *testing.T, c map[string]any) (Manifest, ManifestExplanation) {
	t.Helper()
	return CompileManifest(docOf(t, c), "DEMO", fixtureRef)
}

func fieldIn(m Manifest, event, list, code string) (ManifestField, bool) {
	for _, e := range m.Events {
		if e.EventType != event {
			continue
		}
		for _, f := range e.Fields {
			if f.ListCode == list && f.FieldCode == code {
				return f, true
			}
		}
	}
	return ManifestField{}, false
}

func mustField(t *testing.T, m Manifest, event, list, code string) ManifestField {
	t.Helper()
	f, ok := fieldIn(m, event, list, code)
	if !ok {
		t.Fatalf("%s: %s%s is not in the manifest", event, prefix(list), code)
	}
	return f
}

func prefix(list string) string {
	if list == "" {
		return ""
	}
	return list + "."
}

func hasReq(f ManifestField, kind, ref string) bool {
	for _, r := range f.RequiredBy {
		if r.Kind == kind && r.Ref == ref {
			return true
		}
	}
	return false
}

func rulesOf(c map[string]any, code string) map[string]any {
	for _, r := range c["postingRules"].([]any) {
		if r.(map[string]any)["code"] == code {
			return r.(map[string]any)
		}
	}
	return nil
}

// The GL-TEST-001 manifest is pinned: any change to what it asks for is a reviewed change to
// this snapshot (go test ./model -run Snapshot -update rewrites it).
func TestGLTest001ManifestSnapshot(t *testing.T) {
	m, _ := manifestOf(t, fixtureContent(t))
	got, _ := json.MarshalIndent(m, "", "  ")
	got = append(got, '\n')
	golden := filepath.Join(spec, "_harness", "fixtures", "GL-TEST-001.manifest.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("the GL-TEST-001 manifest changed; review it and rerun with -update.\n%s", got)
	}
}

// The spec's compile examples (GL-TEST-001.md): each event asks for exactly what its rules,
// keys and date policy read.
func TestEachEventRequiresWhatItsConfigurationReads(t *testing.T) {
	m, _ := manifestOf(t, fixtureContent(t))
	for _, c := range []struct {
		event string
		want  []string // LIST.CODE for list fields
	}{
		{"PREM_ADDED", []string{"PREMIUM", "TAX", "COMPANY", "LOB", "CURRENCY", "POLICY_NO", "TERM_NO", "TRANSACTION_DATE", "EFFECTIVE_DATE", "SOURCE_EVENT_ID"}},
		{"INVOICED", []string{"INVOICE_LIST.INVOICE_NO", "INVOICE_LIST.INVOICE_AMT", "POLICY_NO", "TRANSACTION_DATE", "EFFECTIVE_DATE", "COMPANY"}},
		{"PAYMENT_RECEIVED", []string{"PAY_AMT", "BANK_ACCOUNT", "RECEIPT_NO", "ALLOCATION_LIST.ALLOCATION_ID", "ALLOCATION_LIST.INVOICE_NO",
			"ALLOCATION_LIST.ALLOC_AMT", "TRANSACTION_DATE", "EFFECTIVE_DATE", "COMPANY", "CURRENCY"}},
	} {
		for _, w := range c.want {
			list, code := "", w
			if i := strings.IndexByte(w, '.'); i > 0 {
				list, code = w[:i], w[i+1:]
			}
			f := mustField(t, m, c.event, list, code)
			if f.Kind != "SOURCE" {
				t.Errorf("%s %s is %s, want SOURCE", c.event, w, f.Kind)
			}
		}
	}
	// The receipt's 1:N allocations are a list, keyed by ALLOCATION_ID and linked by INVOICE_NO.
	alloc := mustField(t, m, "PAYMENT_RECEIVED", "ALLOCATION_LIST", "ALLOC_AMT")
	if alloc.Cardinality != "LIST" || !alloc.Mandatory || !hasReq(alloc, "POSTING_RULE", "PR_PAYMENT_RECEIVED/ALLOC") {
		t.Errorf("ALLOC_AMT = %+v", alloc)
	}
	if f := mustField(t, m, "PAYMENT_RECEIVED", "ALLOCATION_LIST", "ALLOCATION_ID"); f.KeyRole != "BUSINESS_KEY" || !hasReq(f, "POSTING_RULE", "PR_PAYMENT_RECEIVED/ALLOC") {
		t.Errorf("ALLOCATION_ID = %+v (it identifies each item the ALLOC line expands by)", f)
	}
	if f := mustField(t, m, "PAYMENT_RECEIVED", "ALLOCATION_LIST", "INVOICE_NO"); f.KeyRole != "LINK_KEY" || !hasReq(f, "KEY_PROFILE", "INVOICE_NO") ||
		!hasReq(f, "ACCOUNT_ASSIGNMENT", "9948600") || !f.Mandatory {
		t.Errorf("INVOICE_NO = %+v (the AR1 account needs it as subledger key on every allocation leg)", f)
	}
	// The bank account is collected because the lookup that derives the banking company reads
	// it, and the cash leg posts in the banking company.
	bank := mustField(t, m, "PAYMENT_RECEIVED", "", "BANK_ACCOUNT")
	if !bank.Mandatory || !hasReq(bank, "LOOKUP", "BANKING_COMPANY_BY_ACCOUNT") || !hasReq(bank, "POSTING_RULE", "PR_PAYMENT_RECEIVED/CASH") {
		t.Errorf("BANK_ACCOUNT = %+v", bank)
	}
	// Dates are needed by the date policy.
	if f := mustField(t, m, "PAYMENT_RECEIVED", "", "EFFECTIVE_DATE"); !f.Mandatory || !hasReq(f, "DATE_POLICY", "DP_STANDARD") {
		t.Errorf("EFFECTIVE_DATE = %+v", f)
	}
	if f := mustField(t, m, "PREM_ADDED", "", "SOURCE_EVENT_ID"); f.KeyRole != "DEDUP_KEY" || !f.Mandatory {
		t.Errorf("SOURCE_EVENT_ID = %+v", f)
	}
}

// GL derives BANKING_COMPANY, BOOKING_CLASS and GROSS; the source is asked for their inputs.
func TestDerivedValuesAreNeverRequestedAsSourceColumns(t *testing.T) {
	c := fixtureContent(t)
	// An advance-aware assignment rule makes BOOKING_CLASS matter to the premium leg, and a
	// receipt rule reads an unallocated amount computed from the allocations.
	c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any),
		map[string]any{"code": "AA_PW0_ADVANCE", "class": "PW0", "account": "4100100", "match": map[string]any{"BOOKING_CLASS": "A"}, "priority": -1})
	c["derivedMeasures"] = append(c["derivedMeasures"].([]any),
		map[string]any{"code": "UNALLOCATED", "formula": "PAY_AMT - SUM(ALLOCATION_LIST.ALLOC_AMT)", "resultType": "AMOUNT", "eventTypes": []any{"PAYMENT_RECEIVED"}})
	pr := rulesOf(c, "PR_PAYMENT_RECEIVED")
	pr["filter"] = "UNALLOCATED == 0"
	m, x := manifestOf(t, c)

	for _, e := range m.Events {
		for _, f := range e.Fields {
			switch f.FieldCode {
			case "BANKING_COMPANY", "BOOKING_CLASS", "GROSS", "UNALLOCATED", "BOOK_DATE", "ACCOUNTING_PERIOD":
				if f.Kind != "DERIVED" {
					t.Errorf("%s %s is requested as %s", e.EventType, f.FieldCode, f.Kind)
				}
			}
		}
	}
	if f := mustField(t, m, "PAYMENT_RECEIVED", "", "BANKING_COMPANY"); strings.Join(f.DerivedFrom, ",") != "BANK_ACCOUNT" || !f.Mandatory {
		t.Errorf("BANKING_COMPANY = %+v", f)
	}
	if f := mustField(t, m, "PREM_ADDED", "", "GROSS"); strings.Join(f.DerivedFrom, ",") != "PREMIUM,TAX" || !hasReq(f, "POSTING_RULE", "PR_PREM_ADDED/AR0_DR") {
		t.Errorf("GROSS = %+v", f)
	}
	bc := mustField(t, m, "PREM_ADDED", "", "BOOKING_CLASS")
	if strings.Join(bc.DerivedFrom, ",") != "TRANSACTION_DATE,EFFECTIVE_DATE" || !hasReq(bc, "ACCOUNT_ASSIGNMENT", "AA_PW0_ADVANCE") || bc.Mandatory {
		t.Errorf("BOOKING_CLASS = %+v (not mandatory: AA_PW0 still assigns without it)", bc)
	}
	if f := mustField(t, m, "PAYMENT_RECEIVED", "", "UNALLOCATED"); strings.Join(f.DerivedFrom, ",") != "PAY_AMT,ALLOC_AMT" || !hasReq(f, "FILTER", "PR_PAYMENT_RECEIVED") {
		t.Errorf("UNALLOCATED = %+v", f)
	}
	// The explanation shows the chain: PAY_AMT feeds the filter through UNALLOCATED.
	var chain []string
	for _, e := range x.Events {
		for _, f := range e.Fields {
			if e.EventType == "PAYMENT_RECEIVED" && f.FieldCode == "PAY_AMT" {
				for _, r := range f.Reasons {
					if r.Role == RoleFilter {
						chain = r.Via
					}
				}
			}
		}
	}
	if strings.Join(chain, "|") != "UNALLOCATED = PAY_AMT - SUM(ALLOCATION_LIST.ALLOC_AMT)" {
		t.Errorf("PAY_AMT filter chain = %q", chain)
	}
}

// A field an event declares but nothing reads is not requested.
func TestFieldsNothingReadsAreNotRequested(t *testing.T) {
	m, x := manifestOf(t, fixtureContent(t))
	if f, ok := fieldIn(m, "PREM_ADDED", "", "PRODUCT"); ok {
		t.Fatalf("PRODUCT is requested although nothing reads it: %+v", f)
	}
	found := false
	for _, e := range x.Events {
		for _, f := range e.Unused {
			found = found || (e.EventType == "PREM_ADDED" && f.FieldCode == "PRODUCT")
		}
	}
	if !found {
		t.Fatal("PRODUCT is not reported as unused")
	}
	for _, e := range m.Events {
		for _, f := range e.Fields {
			if len(f.RequiredBy) == 0 {
				t.Errorf("%s %s has no requirement", e.EventType, f.FieldCode)
			}
		}
	}
}

// Mandatory means the engine refuses the event without the value.
func TestMandatoryFollowsWhatTheEngineRefuses(t *testing.T) {
	c := fixtureContent(t)
	m, _ := manifestOf(t, c)
	// LOB chooses the motor receivable, but AA_AR0_DEFAULT assigns without it.
	if f := mustField(t, m, "PREM_ADDED", "", "LOB"); f.Mandatory || !hasReq(f, "ACCOUNT_ASSIGNMENT", "AA_AR0_MOTOR") {
		t.Errorf("LOB = %+v", f)
	}
	// COMPANY is read by a condition (optional) and is the default legal entity (mandatory).
	if f := mustField(t, m, "PAYMENT_RECEIVED", "", "COMPANY"); !f.Mandatory {
		t.Errorf("COMPANY = %+v", f)
	}
	// Without the default rule, every AR0 rule needs LOB.
	var kept []any
	for _, r := range c["accountAssignmentRules"].([]any) {
		if r.(map[string]any)["code"] != "AA_AR0_DEFAULT" {
			kept = append(kept, r)
		}
	}
	c["accountAssignmentRules"] = kept
	m2, _ := manifestOf(t, c)
	if f := mustField(t, m2, "PREM_ADDED", "", "LOB"); !f.Mandatory {
		t.Errorf("without a default AR0 rule, LOB = %+v", f)
	}
}

// The manifest follows the configuration: a change that changes what is read changes the
// digest and says which mappings it affects; a change that does not keeps the digest.
func TestManifestChangesExactlyWhenWhatIsReadChanges(t *testing.T) {
	base, _ := manifestOf(t, fixtureContent(t))

	cosmetic := fixtureContent(t)
	cosmetic["accounts"].([]any)[0].(map[string]any)["name"] = "Renamed account"
	rulesOf(cosmetic, "PR_PREM_ADDED")["description"] = "Premium and tax to the policy receivable"
	m, _ := manifestOf(t, cosmetic)
	if d := DiffManifests(base, m); d.Changed || len(d.Events) != 0 {
		t.Fatalf("a change that reads the same fields changed the manifest: %+v", d)
	}

	// Rewriting PW0_CR as GROSS - TAX reads the same fields through a new path: the lineage
	// (and so the digest) changes, but no mapping has to be reviewed.
	rewritten := fixtureContent(t)
	rulesOf(rewritten, "PR_PREM_ADDED")["lines"].([]any)[1].(map[string]any)["amount"] = "GROSS - TAX"
	m, _ = manifestOf(t, rewritten)
	if d := DiffManifests(base, m); !d.Changed {
		t.Fatalf("a lineage change kept the digest: %+v", d)
	} else {
		for _, e := range d.Events {
			for _, f := range e.Fields {
				if f.Remap || f.Change != "CHANGED" {
					t.Errorf("lineage-only change %+v asks for a remap", f)
				}
			}
		}
	}

	filtered := fixtureContent(t)
	rulesOf(filtered, "PR_PREM_ADDED")["filter"] = "PRODUCT != 'HOME_BUILDING'"
	m, _ = manifestOf(t, filtered)
	d := DiffManifests(base, m)
	if !d.Changed || d.FromDigest == d.ToDigest || len(d.Events) != 1 || d.Events[0].EventType != "PREM_ADDED" {
		t.Fatalf("diff = %+v", d)
	}
	f := d.Events[0].Fields
	if len(f) != 1 || f[0].FieldCode != "PRODUCT" || f[0].Change != "ADDED" || !f[0].Remap || !strings.Contains(strings.Join(f[0].Details, ";"), "FILTER PR_PREM_ADDED") {
		t.Fatalf("PRODUCT change = %+v", f)
	}

	dropped := fixtureContent(t)
	var rules []any
	for _, r := range dropped["postingRules"].([]any) {
		if r.(map[string]any)["code"] != "PR_INVOICED" {
			rules = append(rules, r)
		}
	}
	dropped["postingRules"] = rules
	m, _ = manifestOf(t, dropped)
	var amt *FieldChange
	for _, e := range DiffManifests(base, m).Events {
		for i, x := range e.Fields {
			if e.EventType == "INVOICED" && x.FieldCode == "INVOICE_AMT" {
				amt = &e.Fields[i]
			}
		}
	}
	if amt == nil || amt.Change != "REMOVED" || !amt.Remap {
		t.Fatalf("INVOICE_AMT change = %+v", amt)
	}
	// The invoice keys are still read by the key graph.
	if _, ok := fieldIn(m, "INVOICED", "INVOICE_LIST", "INVOICE_NO"); !ok {
		t.Fatal("INVOICE_NO dropped with the rule, but the key graph still reads it")
	}
}
