package model

import (
	"strings"
	"testing"
)

// dimCatalog is the P1 catalog with dimensions, normalisations and the banking-company lookup.
func dimCatalog(bankC09 string) map[string]any {
	c := p1Catalog("POLICY")
	ev := c["events"].([]any)
	ev[0].(map[string]any)["dimensions"] = []any{"LOB", "COMPANY", "PRODUCT"}
	ev[2].(map[string]any)["dimensions"] = []any{"BANK_ACCOUNT", "COMPANY"}
	c["dimensions"] = []any{
		map[string]any{"code": "LOB", "values": []any{"MOTOR", "HOME"}, "normalizations": []any{
			map[string]any{"sourceSystem": "POLICY", "caseInsensitive": true, "map": map[string]any{"Motor": "MOTOR", "MTR": "MOTOR", "Home": "HOME"}},
			map[string]any{"sourceSystem": "BILLING", "map": map[string]any{"01": "MOTOR", "02": "HOME"}},
		}},
		map[string]any{"code": "COMPANY", "values": []any{"C01", "C09"}},
		map[string]any{"code": "PRODUCT", "values": []any{"MOTOR_PRIVATE", "MOTOR_FLEET", "HOME_BUILDING"}},
		map[string]any{"code": "BANK_ACCOUNT"},
		map[string]any{"code": "BANKING_COMPANY", "values": []any{"C01", "C09"}, "derivedBy": map[string]any{"lookup": "BANKING_COMPANY_BY_ACCOUNT"}},
	}
	c["lookups"] = []any{map[string]any{"code": "BANKING_COMPANY_BY_ACCOUNT", "input": "BANK_ACCOUNT", "output": "BANKING_COMPANY", "entries": []any{
		map[string]any{"key": "BANK-C09", "value": bankC09},
		map[string]any{"key": "BANK-C01", "value": "C01"},
		map[string]any{"key": "BANK-OLD", "value": "C01", "effectiveTo": "2026-03-01"},
		map[string]any{"key": "BANK-OLD", "value": "C09", "effectiveFrom": "2026-03-01"},
	}}}
	return c
}

func payment(bank, company, date string) EventInput {
	return EventInput{EventType: "PAYMENT_RECEIVED", Currency: "AUD", EffectiveDate: date,
		Measures: map[string]string{"PAY_AMT": "1099.50"}, Dimensions: map[string]string{"BANK_ACCOUNT": bank, "COMPANY": company}}
}

// AC: LOB = MOTOR matches each source system's own spelling, once normalised — and a value no
// rule maps is UNKNOWN_CODE, never a default.
func TestLobMotorMatchesNormalizedSystemValues(t *testing.T) {
	c := withDerived(dimCatalog("C09"), map[string]any{"code": "MOTOR_PREMIUM", "formula": "IF(LOB == 'MOTOR', PREMIUM, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}})
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	for _, k := range []struct{ src, raw string }{{"POLICY", "Motor"}, {"POLICY", "mtr"}, {"POLICY", "MOTOR"}, {"BILLING", "01"}, {"BILLING", "MOTOR"}} {
		v, err := Normalize(doc, "LOB", k.src, k.raw)
		if err != nil || v != "MOTOR" {
			t.Errorf("%s %q → %q, %v (want MOTOR)", k.src, k.raw, v, err)
			continue
		}
		in := e1()
		in.Dimensions = map[string]string{"LOB": v, "COMPANY": "C01", "PRODUCT": "MOTOR_PRIVATE"}
		got, err := EvaluateDerived(doc, ref, in)
		if err != nil || byCode(got)["MOTOR_PREMIUM"].Value != "1000 AUD" {
			t.Errorf("filter on %s %q: %+v %v", k.src, k.raw, got, err)
		}
	}
	for _, k := range []struct{ src, raw string }{{"POLICY", "BOAT"}, {"BILLING", "Motor"}, {"BILLING", "03"}, {"POLICY", ""}} {
		if _, err := Normalize(doc, "LOB", k.src, k.raw); err == nil || !strings.Contains(err.Error(), "UNKNOWN_CODE") {
			t.Errorf("%s %q was normalised (want UNKNOWN_CODE): %v", k.src, k.raw, err)
		}
	}
	if v, err := Normalize(doc, "BANK_ACCOUNT", "BILLING", "ACC-77"); err != nil || v != "ACC-77" {
		t.Errorf("an open dimension keeps its value: %q %v", v, err)
	}
}

// AC: BANK_ACCOUNT → BANKING_COMPANY is a lookup inside the versioned configuration; every
// resolution records the lookup, key and configuration version, and effective-dated entries
// apply by date.
func TestBankingCompanyLookupIsVersionedAndTraced(t *testing.T) {
	c := withDerived(dimCatalog("C09"), map[string]any{"code": "INTERCOMPANY_AMT", "formula": "IF(BANKING_COMPANY != COMPANY, PAY_AMT, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PAYMENT_RECEIVED"}})
	v1, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	r1 := ConfigRef{"DEMO_GL", 1, "sha256:" + strings.Repeat("1", 64)}
	got, err := EvaluateEvent(v1, r1, payment("BANK-C09", "C01", "2026-01-10"))
	if err != nil {
		t.Fatal(err)
	}
	var bc ResolvedDimension
	for _, d := range got.Dimensions {
		if d.Dimension == "BANKING_COMPANY" {
			bc = d
		}
	}
	if bc.Value != "C09" || bc.Via != "LOOKUP" || bc.Lookup != "BANKING_COMPANY_BY_ACCOUNT" || bc.Key != "BANK-C09" || bc.Config.Version != 1 {
		t.Fatalf("banking company resolution: %+v", bc)
	}
	if ic := byCode(got.Derived)["INTERCOMPANY_AMT"].Value; ic != "1099.5 AUD" {
		t.Errorf("a C09 deposit for a C01 policy is intercompany: %s", ic)
	}
	// Version 2 re-homes the account: same code, different configuration, different answer.
	v2, _ := parse(t, withDerived(dimCatalog("C01"), map[string]any{"code": "INTERCOMPANY_AMT", "formula": "IF(BANKING_COMPANY != COMPANY, PAY_AMT, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PAYMENT_RECEIVED"}}))
	got2, _ := EvaluateEvent(v2, ConfigRef{"DEMO_GL", 2, "sha256:" + strings.Repeat("2", 64)}, payment("BANK-C09", "C01", "2026-01-10"))
	if ic := byCode(got2.Derived)["INTERCOMPANY_AMT"].Value; ic != "0 AUD" {
		t.Errorf("v2: %s", ic)
	}
	// Effective-dated entries: BANK-OLD belongs to C01 until 1 March, then to C09.
	for date, want := range map[string]string{"2026-02-28": "C01", "2026-03-01": "C09"} {
		e, err := EvaluateEvent(v1, r1, payment("BANK-OLD", "C01", date))
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range e.Dimensions {
			if d.Dimension == "BANKING_COMPANY" && d.Value != want {
				t.Errorf("%s: BANK-OLD → %s, want %s", date, d.Value, want)
			}
		}
	}
	if _, err := EvaluateEvent(v1, r1, payment("BANK-NEW", "C01", "2026-01-10")); err == nil || !strings.Contains(err.Error(), "UNKNOWN_CODE") {
		t.Errorf("an account without a lookup entry must be UNKNOWN_CODE, got %v", err)
	}
}

// AC: unknown dimension codes and values fail validation; they are never defaulted.
func TestUnknownDimensionsFailValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(c map[string]any)
		code   string
	}{
		"event carries an unknown dimension": {func(c map[string]any) { c["events"].([]any)[0].(map[string]any)["dimensions"] = []any{"LOB", "REGION"} }, "UNKNOWN_DIMENSION"},
		"filter on a value LOB does not have": {func(c map[string]any) {
			c["derivedMeasures"] = []any{map[string]any{"code": "X", "formula": "IF(LOB == 'MOTR', PREMIUM, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}}
		}, "UNKNOWN_DIMENSION_VALUE"},
		"IN with an unknown value": {func(c map[string]any) {
			c["derivedMeasures"] = []any{map[string]any{"code": "X", "formula": "IF(IN(LOB, 'MOTOR', 'BOAT'), PREMIUM, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}}
		}, "UNKNOWN_DIMENSION_VALUE"},
		"normalisation to a non-value": {func(c map[string]any) {
			c["dimensions"].([]any)[0].(map[string]any)["normalizations"].([]any)[1].(map[string]any)["map"] = map[string]any{"09": "MARINE"}
		}, "UNKNOWN_DIMENSION_VALUE"},
		"lookup to a non-value": {func(c map[string]any) {
			c["lookups"].([]any)[0].(map[string]any)["entries"].([]any)[0].(map[string]any)["value"] = "C99"
		}, "UNKNOWN_DIMENSION_VALUE"},
		"non-OOTB without custom": {func(c map[string]any) {
			c["dimensions"] = append(c["dimensions"].([]any), map[string]any{"code": "REGION", "values": []any{"N"}})
		}, "UNKNOWN_DIMENSION"},
		"custom shadows OOTB":   {func(c map[string]any) { c["dimensions"].([]any)[0].(map[string]any)["custom"] = true }, "AMBIGUOUS_DIMENSION"},
		"closed without values": {func(c map[string]any) { c["dimensions"].([]any)[2].(map[string]any)["values"] = []any{} }, "MISSING_VALUES"},
		"derived by unknown lookup": {func(c map[string]any) {
			c["dimensions"].([]any)[4].(map[string]any)["derivedBy"] = map[string]any{"lookup": "NOPE"}
		}, "UNKNOWN_LOOKUP"},
		"overlapping lookup entries": {func(c map[string]any) {
			l := c["lookups"].([]any)[0].(map[string]any)
			l["entries"] = append(l["entries"].([]any), map[string]any{"key": "BANK-C09", "value": "C01", "effectiveFrom": "2026-06-01"})
		}, "AMBIGUOUS_LOOKUP"},
		"case-insensitive collision": {func(c map[string]any) {
			c["dimensions"].([]any)[0].(map[string]any)["normalizations"].([]any)[0].(map[string]any)["map"] = map[string]any{"Motor": "MOTOR", "MOTOR ": "MOTOR", "motor": "HOME"}
		}, "AMBIGUOUS_NORMALIZATION"},
		"normalisation for unknown source": {func(c map[string]any) {
			c["dimensions"].([]any)[0].(map[string]any)["normalizations"].([]any)[1].(map[string]any)["sourceSystem"] = "GUIDEWIRE"
		}, "UNKNOWN_SOURCE"},
	}
	for name, k := range cases {
		c := dimCatalog("C09")
		k.mutate(c)
		if _, fs := parse(t, c); !hasCode(errorsOf(fs), k.code) {
			t.Errorf("%s: want %s, got %+v", name, k.code, errorsOf(fs))
		}
	}
	// At runtime: a closed dimension's value outside its set, or a dimension the event does
	// not carry, is refused — not passed through.
	doc, _ := parse(t, dimCatalog("C09"))
	in := e1()
	in.Dimensions = map[string]string{"LOB": "Motor"} // not normalised
	if _, err := EvaluateEvent(doc, ref, in); err == nil || !strings.Contains(err.Error(), "UNKNOWN_CODE") {
		t.Errorf("an un-normalised value reached evaluation: %v", err)
	}
	in.Dimensions = map[string]string{"REGION": "N"}
	if _, err := EvaluateEvent(doc, ref, in); err == nil || !strings.Contains(err.Error(), "UNKNOWN_DIMENSION") {
		t.Errorf("a dimension the event does not carry was accepted: %v", err)
	}
}

// Tests: wildcard and condition filters over normalised dimensions.
func TestWildcardAndConditionFilters(t *testing.T) {
	c := withDerived(dimCatalog("C09"),
		map[string]any{"code": "IS_MOTOR_PRODUCT", "formula": "LIKE(PRODUCT, 'MOTOR_*')", "resultType": "BOOL", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "IN_SCOPE", "formula": "IN(COMPANY, 'C01', 'C09') AND NOT LIKE(PRODUCT, '*FLEET')", "resultType": "BOOL", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "LOB_KNOWN", "formula": "NOT ISNULL(LOB)", "resultType": "BOOL", "eventTypes": []any{"PREM_ADDED"}},
	)
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	for product, want := range map[string][2]string{
		"MOTOR_PRIVATE": {"true", "true"}, "MOTOR_FLEET": {"true", "false"}, "HOME_BUILDING": {"false", "true"},
	} {
		in := e1()
		in.Dimensions = map[string]string{"LOB": "MOTOR", "COMPANY": "C01", "PRODUCT": product}
		got, err := EvaluateDerived(doc, ref, in)
		if err != nil {
			t.Fatal(err)
		}
		m := byCode(got)
		if m["IS_MOTOR_PRODUCT"].Value != want[0] || m["IN_SCOPE"].Value != want[1] || m["LOB_KNOWN"].Value != "true" {
			t.Errorf("%s: motor %s scope %s (want %v)", product, m["IS_MOTOR_PRODUCT"].Value, m["IN_SCOPE"].Value, want)
		}
	}
	in := e1()
	in.Dimensions = map[string]string{"COMPANY": "C01", "PRODUCT": "HOME_BUILDING"}
	got, _ := EvaluateDerived(doc, ref, in)
	if byCode(got)["LOB_KNOWN"].Value != "false" {
		t.Errorf("a missing dimension reads as null: %+v", got)
	}
}

func TestOOTBDimensionCatalog(t *testing.T) {
	want := []string{"COUNTRY", "COMPANY", "LOB", "PRODUCT", "TRANSACTION_TYPE", "CURRENCY", "BANK_ACCOUNT", "BANKING_COMPANY", "BOOKING_CLASS"}
	got := OOTBDimensions()
	if len(got) != len(want) || OOTBDimensionCatalogVersion() != "gl.dimensions.ootb/v1" {
		t.Fatalf("%+v", got)
	}
	for i, d := range got {
		if d.Code != want[i] || d.Description == "" {
			t.Errorf("dimension %d: %+v", i, d)
		}
	}
	got[0].Code = "TAMPERED"
	if OOTBDimensions()[0].Code != "COUNTRY" {
		t.Error("callers can change the global catalog")
	}
}
