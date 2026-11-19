package model

import (
	"reflect"
	"strings"
	"testing"
)

// The OOTB catalog covers the insurance measure families the backlog names, each with sign,
// grain, currency, definition and source scope.
func TestOOTBCatalogCoversInsuranceMeasures(t *testing.T) {
	byCat := map[string][]string{}
	for _, m := range OOTBMeasures() {
		if m.Name == "" || m.Definition == "" || !oneOf(m.DataType, dataTypes) || !oneOf(m.Sign, signs) ||
			!oneOf(m.Currency, currencies) || !oneOf(m.Grain, grains) || len(m.SourceScope) == 0 || m.Origin != "OOTB" {
			t.Errorf("incomplete OOTB measure: %+v", m)
		}
		for _, k := range m.SourceScope {
			if !oneOf(k, SourceKinds) {
				t.Errorf("%s: unknown source kind %s", m.Code, k)
			}
		}
		byCat[m.Category] = append(byCat[m.Category], m.Code)
	}
	for _, cat := range []string{"PREMIUM", "TAX", "FEE", "RECEIVABLE", "CASH", "COMMISSION", "CLAIM", "EARNINGS"} {
		if len(byCat[cat]) == 0 {
			t.Errorf("no OOTB measure for %s", cat)
		}
	}
	if OOTBCatalogVersion() != "gl.measures.ootb/v1" {
		t.Errorf("catalog version %q", OOTBCatalogVersion())
	}
}

// AC: a client override never changes the global default — nor another client's catalog.
func TestOverrideNeverChangesTheGlobalDefault(t *testing.T) {
	before := OOTBMeasures()
	clientA := p1Catalog("POLICY")
	clientA["baseMeasures"] = []any{map[string]any{"code": "PREMIUM", "definition": "Gross written premium incl. policy fee (client A)", "sign": "POSITIVE", "sourceScope": []any{"POLICY"}}}
	docA, fs := parse(t, clientA)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	docB, _ := parse(t, p1Catalog("POLICY"))

	a := ResolveMeasures(docA)["PREMIUM"]
	if a.Origin != "OVERRIDDEN" || !strings.Contains(a.Definition, "client A") || a.Sign != "POSITIVE" || !reflect.DeepEqual(a.Overridden, []string{"definition", "sign", "sourceScope"}) {
		t.Errorf("client A premium: %+v", a)
	}
	// Mutating what a caller received must not leak back either.
	a.SourceScope[0] = "TAMPERED"
	b := ResolveMeasures(docB)["PREMIUM"]
	if b.Origin != "OOTB" || strings.Contains(b.Definition, "client A") || b.Sign != "SIGNED" {
		t.Errorf("client B sees client A's override: %+v", b)
	}
	if after := OOTBMeasures(); !reflect.DeepEqual(before, after) {
		t.Errorf("the global catalog changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if again := ResolveMeasures(docA)["PREMIUM"]; again.SourceScope[0] != "POLICY" {
		t.Errorf("a caller's mutation leaked into resolution: %+v", again)
	}
}

// Overrides may describe a measure differently; they may not change what it is.
func TestOverridesCannotChangeWhatAMeasureIs(t *testing.T) {
	for field, value := range map[string]string{"dataType": "COUNT", "currency": "NONE", "grain": "ITEM", "category": "FEE"} {
		c := p1Catalog("POLICY")
		c["baseMeasures"] = []any{map[string]any{"code": "PREMIUM", field: value}}
		if _, fs := parse(t, c); !hasCode(errorsOf(fs), "OVERRIDE_NOT_ALLOWED") {
			t.Errorf("overriding %s was accepted", field)
		}
	}
}

// AC: unknown or ambiguous measure definitions block (validation, hence approval, hence the
// mapping compilation that only runs on approved configuration).
func TestUnknownOrAmbiguousMeasuresBlock(t *testing.T) {
	cases := map[string]struct {
		mutate func(c map[string]any)
		code   string
	}{
		"event carries an unknown measure": {func(c map[string]any) {
			c["events"].([]any)[0].(map[string]any)["measures"] = []any{"PREMIUM", "STAMP_DUTY"}
		}, "UNKNOWN_MEASURE"},
		"override of a non-OOTB code": {func(c map[string]any) { c["baseMeasures"] = []any{map[string]any{"code": "STAMP_DUTY"}} }, "UNKNOWN_MEASURE"},
		"custom shadows an OOTB code": {func(c map[string]any) {
			c["baseMeasures"] = []any{map[string]any{"code": "PREMIUM", "custom": true, "name": "P", "definition": "d", "dataType": "AMOUNT", "sign": "SIGNED", "currency": "EVENT", "grain": "ANY", "sourceScope": []any{"POLICY"}}}
		}, "AMBIGUOUS_MEASURE"},
		"duplicate measure": {func(c map[string]any) {
			c["baseMeasures"] = []any{map[string]any{"code": "TAX"}, map[string]any{"code": "TAX"}}
		}, "DUPLICATE_MEASURE"},
		"custom without meaning": {func(c map[string]any) {
			c["baseMeasures"] = []any{map[string]any{"code": "STAMP_DUTY", "custom": true}}
		}, "UNDEFINED_MEASURE"},
		"custom amount, no ccy": {func(c map[string]any) {
			c["baseMeasures"] = []any{map[string]any{"code": "STAMP_DUTY", "custom": true, "name": "S", "definition": "d", "dataType": "AMOUNT", "sign": "POSITIVE", "currency": "NONE", "grain": "EVENT", "sourceScope": []any{"POLICY"}}}
		}, "INVALID_CURRENCY"},
		"item measure at event": {func(c map[string]any) {
			c["events"].([]any)[2].(map[string]any)["measures"] = []any{"PAY_AMT", "INVOICE_AMT"}
		}, "GRAIN_MISMATCH"},
		"event measure on a list": {func(c map[string]any) {
			c["events"].([]any)[1].(map[string]any)["lists"].([]any)[0].(map[string]any)["measures"] = []any{"PAY_AMT"}
		}, "GRAIN_MISMATCH"},
	}
	for name, k := range cases {
		c := p1Catalog("POLICY")
		k.mutate(c)
		if _, fs := parse(t, c); !hasCode(errorsOf(fs), k.code) {
			t.Errorf("%s: expected blocking %s, got %+v", name, k.code, fs)
		}
	}
}

// A custom measure is available to events once fully defined.
func TestCustomMeasuresExtendTheCatalog(t *testing.T) {
	c := p1Catalog("POLICY")
	c["baseMeasures"] = []any{map[string]any{"code": "STAMP_DUTY", "custom": true, "name": "Stamp duty", "category": "TAX",
		"definition": "State stamp duty on premium", "dataType": "AMOUNT", "sign": "POSITIVE", "currency": "EVENT", "grain": "EVENT", "sourceScope": []any{"POLICY"}}}
	c["events"].([]any)[0].(map[string]any)["measures"] = []any{"PREMIUM", "TAX", "STAMP_DUTY"}
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	if m := ResolveMeasures(doc)["STAMP_DUTY"]; m.Origin != "CUSTOM" || m.Category != "TAX" {
		t.Errorf("custom measure: %+v", m)
	}
}

// AC: base measures are distinct from chart-of-account postings — a measure has no account
// and no debit/credit side, and an attempt to give it one is refused.
func TestMeasuresAreNotPostings(t *testing.T) {
	for _, field := range []string{"account", "side", "debit", "accountingClass"} {
		c := p1Catalog("POLICY")
		c["baseMeasures"] = []any{map[string]any{"code": "PREMIUM", field: "4100100"}}
		if _, fs := parse(t, c); !hasCode(errorsOf(fs), "INVALID_ITEM") {
			t.Errorf("a measure accepted posting field %q", field)
		}
	}
	for _, m := range OOTBMeasures() {
		if strings.Contains(strings.ToLower(m.Definition), "debit") || strings.Contains(strings.ToLower(m.Definition), "credit to") {
			t.Errorf("OOTB measure %s describes a posting: %s", m.Code, m.Definition)
		}
	}
}

// An authority outside a measure's source scope is flagged (warning: legitimate but unusual).
func TestAuthorityOutsideSourceScopeIsFlagged(t *testing.T) {
	c := p1Catalog("POLICY")
	c["sourceSystems"] = append(c["sourceSystems"].([]any), map[string]any{"code": "CLAIMSYS", "kind": "CLAIMS"})
	c["sourceAuthority"].([]any)[2] = map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "CLAIMSYS", "intakeMode": "RAW_EVENT"}
	_, fs := parse(t, c)
	var warned bool
	for _, f := range fs {
		if f.Code == "OUT_OF_SOURCE_SCOPE" && f.Severity == "warning" {
			warned = true
		}
	}
	if !warned || len(errorsOf(fs)) != 0 {
		t.Errorf("want an OUT_OF_SOURCE_SCOPE warning and no errors, got %+v", fs)
	}
}
