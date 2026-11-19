package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// p1Catalog is the GL-TEST-001 event catalog: three distinct, configurable events.
// premiumFrom selects which source posts premium — the only difference between client setups.
func p1Catalog(premiumFrom string, authority ...map[string]any) map[string]any {
	auth := []any{
		map[string]any{"eventType": "PREM_ADDED", "measure": "*", "sourceSystem": premiumFrom, "intakeMode": "RAW_EVENT"},
		map[string]any{"eventType": "INVOICED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"},
		map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"},
	}
	for _, a := range authority {
		auth = append(auth, a)
	}
	return map[string]any{
		"sourceSystems": []any{
			map[string]any{"code": "POLICY", "kind": "POLICY"},
			map[string]any{"code": "BILLING", "kind": "BILLING"},
		},
		"events": []any{
			map[string]any{"code": "PREM_ADDED", "description": "Premium posted", "economicKey": []any{"POLICY_NO", "TERM_NO"}, "measures": []any{"PREMIUM", "TAX"}},
			map[string]any{"code": "INVOICED", "description": "Invoice generated", "economicKey": []any{"POLICY_NO", "TERM_NO", "INVOICE_RUN"},
				"lists": []any{map[string]any{"code": "INVOICE_LIST", "itemKey": []any{"INVOICE_NO"}, "measures": []any{"INVOICE_AMT"}}}},
			map[string]any{"code": "PAYMENT_RECEIVED", "description": "Payment received", "economicKey": []any{"RECEIPT_NO"}, "measures": []any{"PAY_AMT"},
				"lists": []any{map[string]any{"code": "ALLOCATION_LIST", "itemKey": []any{"ALLOCATION_ID"}, "keys": []any{"INVOICE_NO"}, "measures": []any{"ALLOC_AMT"}}}},
		},
		"sourceAuthority": auth,
	}
}

func parse(t *testing.T, content map[string]any) (*Document, []Finding) {
	t.Helper()
	b, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	doc, fs := Parse(b)
	return doc, append(fs, Validate(doc)...)
}

func errorsOf(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Severity == "error" {
			out = append(out, f)
		}
	}
	return out
}

func hasCode(fs []Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func day(s string) time.Time {
	t, ok := ParseDay(s)
	if !ok {
		panic(s)
	}
	return t
}

// AC: premium posted, invoice generated and payment received are distinct configurable events.
func TestThreeDistinctConfigurableEventsValidate(t *testing.T) {
	doc, fs := parse(t, p1Catalog("POLICY"))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("the P1 catalog should validate: %+v", e)
	}
	if len(doc.Events) != 3 || doc.Events[0].Code != "PREM_ADDED" || doc.Events[1].Code != "INVOICED" || doc.Events[2].Code != "PAYMENT_RECEIVED" {
		t.Fatalf("events: %+v", doc.Events)
	}
	if got := carried(doc.Events[2]); !reflect.DeepEqual(got, []string{"PAY_AMT", "ALLOC_AMT"}) {
		t.Errorf("payment carries %v", got)
	}
}

// AC: a client can select Policy for premium and Billing for payments without code changes —
// the same code resolves differently purely from configuration.
func TestClientsChooseAuthorityByConfigurationOnly(t *testing.T) {
	a, _ := parse(t, p1Catalog("POLICY"))
	b, _ := parse(t, p1Catalog("BILLING"))
	jan := day("2026-01-01")
	for _, c := range []struct {
		doc  *Document
		want string
	}{{a, "POLICY"}, {b, "BILLING"}} {
		got, err := ResolveAuthority(c.doc, "PREM_ADDED", "PREMIUM", jan)
		if err != nil || got.SourceSystem != c.want {
			t.Errorf("premium authority: got %+v, %v; want %s", got, err, c.want)
		}
		pay, err := ResolveAuthority(c.doc, "PAYMENT_RECEIVED", "PAY_AMT", jan)
		if err != nil || pay.SourceSystem != "BILLING" {
			t.Errorf("payment authority: %+v %v", pay, err)
		}
	}
	// The Billing copy of a premium posts for client B and is reconciliation-only for client A.
	if ok, _ := MayPost(a, "PREM_ADDED", "PREMIUM", "BILLING", "RAW_EVENT", jan); ok {
		t.Error("client A: a Billing premium must not post")
	}
	if ok, _ := MayPost(b, "PREM_ADDED", "PREMIUM", "BILLING", "RAW_EVENT", jan); !ok {
		t.Error("client B: the Billing premium must post")
	}
	if ok, _ := MayPost(a, "PREM_ADDED", "PREMIUM", "POLICY", "SOURCE_JOURNAL", jan); ok {
		t.Error("the right source in the wrong intake mode must not post")
	}
}

// AC: ambiguous duplicate-source authority is rejected — or explicitly resolved by
// non-overlapping effective windows.
func TestOverlappingAuthorityIsRejectedOrResolvedByWindows(t *testing.T) {
	_, fs := parse(t, p1Catalog("POLICY", map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT"}))
	if !hasCode(fs, "AMBIGUOUS_SOURCE_AUTHORITY") {
		t.Fatalf("two open authorities for payments were accepted: %+v", fs)
	}
	// Explicit resolution: Billing until 1 March, Policy from 1 March.
	c := p1Catalog("POLICY")
	auth := c["sourceAuthority"].([]any)
	auth[2] = map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT", "effectiveTo": "2026-03-01"}
	c["sourceAuthority"] = append(auth, map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT", "effectiveFrom": "2026-03-01"})
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("adjacent windows are not overlapping: %+v", e)
	}
	for d, want := range map[string]string{"2026-02-28": "BILLING", "2026-03-01": "POLICY", "2030-01-01": "POLICY", "2000-01-01": "BILLING"} {
		got, err := ResolveAuthority(doc, "PAYMENT_RECEIVED", "PAY_AMT", day(d))
		if err != nil || got.SourceSystem != want {
			t.Errorf("%s: got %+v %v, want %s", d, got, err, want)
		}
	}
	// Overlap by one day is still ambiguous.
	auth = c["sourceAuthority"].([]any)
	auth[len(auth)-1] = map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT", "effectiveFrom": "2026-02-28"}
	if _, fs := parse(t, c); !hasCode(fs, "AMBIGUOUS_SOURCE_AUTHORITY") {
		t.Error("a one-day overlap was accepted")
	}
}

// A measure-specific authority takes precedence over the event's "*" entry.
func TestSpecificMeasureTakesPrecedenceOverWildcard(t *testing.T) {
	doc, fs := parse(t, p1Catalog("POLICY", map[string]any{"eventType": "PREM_ADDED", "measure": "TAX", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"}))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	tax, _ := ResolveAuthority(doc, "PREM_ADDED", "TAX", day("2026-01-01"))
	prem, _ := ResolveAuthority(doc, "PREM_ADDED", "PREMIUM", day("2026-01-01"))
	if tax.SourceSystem != "BILLING" || prem.SourceSystem != "POLICY" {
		t.Errorf("TAX from %s (want BILLING), PREMIUM from %s (want POLICY)", tax.SourceSystem, prem.SourceSystem)
	}
}

// Tests: missing source authority — statically (a measure no entry covers) and at runtime
// (a date outside every window) — is an error, never a default.
func TestMissingAuthorityIsAnError(t *testing.T) {
	c := p1Catalog("POLICY")
	c["sourceAuthority"] = c["sourceAuthority"].([]any)[:2] // drop the payment authority
	_, fs := parse(t, c)
	var miss []string
	for _, f := range fs {
		if f.Code == "MISSING_SOURCE_AUTHORITY" {
			miss = append(miss, f.Message)
		}
	}
	if len(miss) != 2 || !strings.Contains(strings.Join(miss, "|"), "PAY_AMT") || !strings.Contains(strings.Join(miss, "|"), "ALLOC_AMT") {
		t.Fatalf("both payment measures must be reported missing: %v", miss)
	}
	c = p1Catalog("POLICY")
	auth := c["sourceAuthority"].([]any)
	auth[0] = map[string]any{"eventType": "PREM_ADDED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT", "effectiveFrom": "2026-03-01"}
	doc, _ := parse(t, c)
	_, err := ResolveAuthority(doc, "PREM_ADDED", "PREMIUM", day("2026-02-01"))
	if ae, ok := err.(*AuthorityError); !ok || ae.Code != "MISSING_SOURCE_AUTHORITY" {
		t.Errorf("a date before every window must be MISSING_SOURCE_AUTHORITY, got %v", err)
	}
	if ok, err := MayPost(doc, "PREM_ADDED", "PREMIUM", "POLICY", "RAW_EVENT", day("2026-02-01")); ok || err == nil {
		t.Error("MayPost must refuse when no authority covers the date")
	}
}

// Every way a catalog can be wrong is reported with its own code and path.
func TestCatalogErrorsAreReported(t *testing.T) {
	cases := map[string]struct {
		mutate func(c map[string]any)
		code   string
	}{
		"unknown event": {func(c map[string]any) {
			c["sourceAuthority"] = append(c["sourceAuthority"].([]any), map[string]any{"eventType": "NOPE", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT"})
		}, "UNKNOWN_EVENT"},
		"unknown source":  {func(c map[string]any) { c["sourceAuthority"].([]any)[0].(map[string]any)["sourceSystem"] = "LEDGERX" }, "UNKNOWN_SOURCE"},
		"unknown measure": {func(c map[string]any) { c["sourceAuthority"].([]any)[0].(map[string]any)["measure"] = "COMMISSION" }, "UNKNOWN_MEASURE"},
		"bad intake mode": {func(c map[string]any) { c["sourceAuthority"].([]any)[0].(map[string]any)["intakeMode"] = "CSV" }, "INVALID_INTAKE_MODE"},
		"inverted window": {func(c map[string]any) {
			m := c["sourceAuthority"].([]any)[0].(map[string]any)
			m["effectiveFrom"] = "2026-03-01"
			m["effectiveTo"] = "2026-01-01"
		}, "INVALID_WINDOW"},
		"impossible date": {func(c map[string]any) {
			c["sourceAuthority"].([]any)[0].(map[string]any)["effectiveFrom"] = "2026-02-30"
		}, "INVALID_WINDOW"},
		"duplicate event": {func(c map[string]any) { c["events"] = append(c["events"].([]any), c["events"].([]any)[0]) }, "DUPLICATE_CODE"},
		"bad kind":        {func(c map[string]any) { c["sourceSystems"].([]any)[0].(map[string]any)["kind"] = "ERP" }, "INVALID_KIND"},
		"no economic key": {func(c map[string]any) { c["events"].([]any)[0].(map[string]any)["economicKey"] = []any{} }, "MISSING_ECONOMIC_KEY"},
		"ambiguous grain": {func(c map[string]any) {
			c["events"].([]any)[2].(map[string]any)["measures"] = []any{"PAY_AMT", "ALLOC_AMT"}
		}, "AMBIGUOUS_GRAIN"},
		"list without item key": {func(c map[string]any) {
			c["events"].([]any)[1].(map[string]any)["lists"].([]any)[0].(map[string]any)["itemKey"] = []any{}
		}, "MISSING_ITEM_KEY"},
		"unknown field (typo)": {func(c map[string]any) { c["events"].([]any)[0].(map[string]any)["mesures"] = []any{"X"} }, "INVALID_ITEM"},
		"lower-case code":      {func(c map[string]any) { c["events"].([]any)[0].(map[string]any)["code"] = "prem_added" }, "INVALID_CODE"},
	}
	for name, k := range cases {
		c := p1Catalog("POLICY")
		k.mutate(c)
		if _, fs := parse(t, c); !hasCode(errorsOf(fs), k.code) {
			t.Errorf("%s: expected an error %s, got %+v", name, k.code, fs)
		}
	}
}

func TestFindingsAreDeterministic(t *testing.T) {
	c := p1Catalog("POLICY", map[string]any{"eventType": "NOPE", "measure": "*", "sourceSystem": "X", "intakeMode": "Y"})
	_, a := parse(t, c)
	_, b := parse(t, c)
	if !reflect.DeepEqual(a, b) || len(a) < 3 {
		t.Errorf("findings differ between runs or are missing: %v vs %v", a, b)
	}
}
