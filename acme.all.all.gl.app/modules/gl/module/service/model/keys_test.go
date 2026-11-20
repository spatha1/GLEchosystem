package model

import (
	"reflect"
	"strings"
	"testing"
)

// keyProfiles: the GL-TEST-001 keys. TERM_NO is unique only within its policy and an invoice
// run only within its term; an allocation is numbered within its receipt.
func keyProfiles() []any {
	return []any{
		map[string]any{"code": "POLICY_NO", "pattern": "P[0-9]+", "caseInsensitive": true},
		map[string]any{"code": "TERM_NO", "pattern": "[0-9]+", "scope": []any{"POLICY_NO"},
			"links": []any{map[string]any{"to": "POLICY_NO", "cardinality": "MANY_TO_ONE"}}},
		map[string]any{"code": "INVOICE_RUN", "scope": []any{"TERM_NO"},
			"links": []any{map[string]any{"to": "TERM_NO", "cardinality": "MANY_TO_ONE"}}},
		map[string]any{"code": "INVOICE_NO", "pattern": "INV[0-9]+",
			"links": []any{map[string]any{"to": "POLICY_NO", "cardinality": "MANY_TO_ONE"}, map[string]any{"to": "INVOICE_RUN", "cardinality": "MANY_TO_ONE"}}},
		map[string]any{"code": "RECEIPT_NO", "pattern": "R[0-9]+"},
		map[string]any{"code": "ALLOCATION_ID", "scope": []any{"RECEIPT_NO"},
			"links": []any{map[string]any{"to": "RECEIPT_NO", "cardinality": "MANY_TO_ONE"}, map[string]any{"to": "INVOICE_NO", "cardinality": "MANY_TO_ONE"}}},
	}
}

func keyDoc(t *testing.T, edit func(map[string]any)) *Document {
	t.Helper()
	c := p1Catalog("POLICY")
	c["keyProfiles"] = keyProfiles()
	if edit != nil {
		edit(c)
	}
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	return doc
}

const tenant = "CLIENT_A"

func premium(policy, term string) EventInput {
	return EventInput{TenantID: tenant, SourceSystem: "POLICY", SourceEventID: "E1-" + policy, SourceEventVersion: 1, EventType: "PREM_ADDED",
		Keys: map[string]string{"POLICY_NO": policy, "TERM_NO": term}, Currency: "AUD", Measures: map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"}}
}

func invoiced(id, policy, run string, invoices ...string) EventInput {
	var items []map[string]string
	for _, n := range invoices {
		items = append(items, map[string]string{"INVOICE_NO": n, "INVOICE_AMT": "550.00"})
	}
	return EventInput{TenantID: tenant, SourceSystem: "BILLING", SourceEventID: id, SourceEventVersion: 1, EventType: "INVOICED",
		Keys: map[string]string{"POLICY_NO": policy, "TERM_NO": "1", "INVOICE_RUN": run}, Currency: "AUD", Lists: map[string][]map[string]string{"INVOICE_LIST": items}}
}

// paid: allocations as "A1=INV1".
func paid(id, receipt string, allocations ...string) EventInput {
	var items []map[string]string
	for _, a := range allocations {
		p := strings.SplitN(a, "=", 2)
		items = append(items, map[string]string{"ALLOCATION_ID": p[0], "INVOICE_NO": p[1], "ALLOC_AMT": "1.00"})
	}
	return EventInput{TenantID: tenant, SourceSystem: "BILLING", SourceEventID: id, SourceEventVersion: 1, EventType: "PAYMENT_RECEIVED",
		Keys: map[string]string{"RECEIPT_NO": receipt}, Currency: "AUD", Measures: map[string]string{"PAY_AMT": "1099.50"}, Lists: map[string][]map[string]string{"ALLOCATION_LIST": items}}
}

// p1Graph: E1 premium, E2 two invoices, E3 one receipt allocated to both.
func p1Graph(t *testing.T, doc *Document) *KeyGraph {
	t.Helper()
	g := NewKeyGraph(tenant, doc)
	for _, in := range []EventInput{premium("P1", "1"), invoiced("E2", "P1", "RUN1", "INV1", "INV2"), paid("E3", "R1", "A1=INV1", "A2=INV2")} {
		if r := g.AddEvent(in); r != Accepted {
			t.Fatalf("%s: %s %+v", in.SourceEventID, r, g.Exceptions)
		}
	}
	return g
}

func keyRef(t *testing.T, g *KeyGraph, key string, kv ...string) KeyRef {
	t.Helper()
	vals := map[string]string{}
	for i := 0; i < len(kv); i += 2 {
		vals[kv[i]] = kv[i+1]
	}
	r, err := g.Ref(key, vals)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// AC: one receipt may expand into multiple invoice allocations without losing linkage.
func TestOneReceiptExpandsIntoManyAllocationsWithoutLosingLinkage(t *testing.T) {
	g := p1Graph(t, keyDoc(t, nil))
	r1 := keyRef(t, g, "RECEIPT_NO", "RECEIPT_NO", "R1")
	if got := g.Children(r1); !reflect.DeepEqual(got, map[string][]string{"ALLOCATION_ID": {"R1/A1", "R1/A2"}}) {
		t.Errorf("R1's allocations: %v", got)
	}
	for alloc, inv := range map[string]string{"A1": "INV1", "A2": "INV2"} {
		a := keyRef(t, g, "ALLOCATION_ID", "RECEIPT_NO", "R1", "ALLOCATION_ID", alloc)
		if got := g.Parents(a); !reflect.DeepEqual(got, map[string]string{"RECEIPT_NO": "R1", "INVOICE_NO": inv}) {
			t.Errorf("%s keeps both its receipt and its invoice: %v", alloc, got)
		}
	}
	inv2 := keyRef(t, g, "INVOICE_NO", "INVOICE_NO", "INV2")
	if got := g.Parents(inv2); !reflect.DeepEqual(got, map[string]string{"POLICY_NO": "P1", "INVOICE_RUN": "P1/1/RUN1"}) {
		t.Errorf("INV2's owners: %v", got)
	}
	// Per-item measures still evaluate per item: the receipt is not multiplied into its items.
	doc := keyDoc(t, nil)
	if _, err := EvaluateEvent(doc, ConfigRef{ConfigID: "GL", Version: 1}, paid("E3", "R1", "A1=INV1", "A2=INV2")); err != nil {
		t.Errorf("keys do not disturb evaluation: %v", err)
	}
}

// AC: rebuild-by-policy discovers all related invoice and receipt keys.
func TestRebuildByPolicyFindsEveryInvoiceAndReceipt(t *testing.T) {
	doc := keyDoc(t, nil)
	g := p1Graph(t, doc)
	p1 := keyRef(t, g, "POLICY_NO", "POLICY_NO", "P1")
	want := RebuildScope{Start: p1, Traversal: "OWNERSHIP_LINKS",
		Keys: map[string][]string{"ALLOCATION_ID": {"R1/A1", "R1/A2"}, "INVOICE_NO": {"INV1", "INV2"}, "INVOICE_RUN": {"P1/1/RUN1"},
			"RECEIPT_NO": {"R1"}, "TERM_NO": {"P1/1"}},
		Events: []string{"POLICY/E1-P1", "BILLING/E2", "BILLING/E3"}}
	if got := g.Rebuild(p1); !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuild P1:\n got %+v\nwant %+v", got, want)
	}

	// A second policy, and a receipt split across both policies.
	for _, in := range []EventInput{premium("P2", "1"), invoiced("E12", "P2", "RUN1", "INV5"), paid("E19", "R9", "A1=INV2", "A2=INV5")} {
		if r := g.AddEvent(in); r != Accepted {
			t.Fatalf("%s: %s %+v", in.SourceEventID, r, g.Exceptions)
		}
	}
	got := g.Rebuild(p1)
	if !reflect.DeepEqual(got.Keys["RECEIPT_NO"], []string{"R1", "R9"}) || !reflect.DeepEqual(got.Events, []string{"POLICY/E1-P1", "BILLING/E2", "BILLING/E3", "BILLING/E19"}) {
		t.Errorf("the split receipt R9 must be reprocessed with P1: %+v", got)
	}
	if !reflect.DeepEqual(got.Keys["INVOICE_NO"], []string{"INV1", "INV2"}) || got.Keys["POLICY_NO"] != nil || !reflect.DeepEqual(got.Keys["TERM_NO"], []string{"P1/1"}) {
		t.Errorf("R9's allocation to P2 must not pull P2's invoices or terms in: %+v", got)
	}
	// Investigation sees everything connected, including P2 through R9.
	if rel := g.Related(p1); !reflect.DeepEqual(rel["POLICY_NO"], []string{"P2"}) || !reflect.DeepEqual(rel["INVOICE_NO"], []string{"INV1", "INV2", "INV5"}) {
		t.Errorf("related: %v", rel)
	}
	// Rebuilding a receipt reprocesses only the receipt, while naming the invoices it touches.
	r1 := g.Rebuild(keyRef(t, g, "RECEIPT_NO", "RECEIPT_NO", "R1"))
	if !reflect.DeepEqual(r1.Events, []string{"BILLING/E3"}) || !reflect.DeepEqual(r1.Keys["INVOICE_NO"], []string{"INV1", "INV2"}) {
		t.Errorf("rebuild R1: %+v", r1)
	}

	// The same events in a different order give the same keys.
	g2 := NewKeyGraph(tenant, doc)
	for _, in := range []EventInput{paid("E19", "R9", "A1=INV2", "A2=INV5"), paid("E3", "R1", "A1=INV1", "A2=INV2"), invoiced("E12", "P2", "RUN1", "INV5"),
		invoiced("E2", "P1", "RUN1", "INV1", "INV2"), premium("P2", "1"), premium("P1", "1")} {
		if r := g2.AddEvent(in); r != Accepted {
			t.Fatalf("%s: %s %+v", in.SourceEventID, r, g2.Exceptions)
		}
	}
	if got2 := g2.Rebuild(p1); !reflect.DeepEqual(got2.Keys, got.Keys) || len(got2.Events) != len(got.Events) {
		t.Errorf("arrival order must not change the scope:\n%+v\n%+v", got2, got)
	}

	// Without ownership links the rebuild falls back to the connected component.
	flat := keyDoc(t, func(c map[string]any) {
		for _, p := range c["keyProfiles"].([]any) {
			delete(p.(map[string]any), "links")
		}
	})
	g3 := p1Graph(t, flat)
	if s := g3.Rebuild(keyRef(t, g3, "POLICY_NO", "POLICY_NO", "P1")); s.Traversal != "CONNECTED_COMPONENT" || !reflect.DeepEqual(s.Keys["RECEIPT_NO"], []string{"R1"}) {
		t.Errorf("fallback: %+v", s)
	}
}

// snapshot is everything observable about a graph.
func snapshot(g *KeyGraph, t *testing.T) []any {
	var out []any
	for _, r := range []KeyRef{keyRef(t, g, "POLICY_NO", "POLICY_NO", "P1"), keyRef(t, g, "RECEIPT_NO", "RECEIPT_NO", "R1"), keyRef(t, g, "INVOICE_NO", "INVOICE_NO", "INV1")} {
		out = append(out, g.Related(r), g.Rebuild(r), g.Parents(r), g.Children(r))
	}
	return out
}

// AC: missing or conflicting keys enter an exception path, and change nothing.
func TestMissingOrConflictingKeysBecomeExceptions(t *testing.T) {
	noReceipt := paid("E4", "R2", "A1=INV1")
	noReceipt.Keys = map[string]string{"RECEIPT_NO": "  "}
	itemNoInvoice := paid("E4", "R2", "A1=INV1")
	delete(itemNoInvoice.Lists["ALLOCATION_LIST"][0], "INVOICE_NO")
	undeclared := paid("E4", "R2", "A1=INV1")
	undeclared.Keys["POLICY_NO"] = "P1"
	noIdentity := paid("", "R2", "A1=INV1")
	unknownType := paid("E4", "R2", "A1=INV1")
	unknownType.EventType = "WRITE_OFF"
	cases := []struct {
		name, category, detail string
		in                     EventInput
	}{
		{"event-level key missing", "MISSING_KEY", "missing key RECEIPT_NO", noReceipt},
		{"item key missing", "MISSING_KEY", "ALLOCATION_LIST item 1 is missing key INVOICE_NO", itemNoInvoice},
		{"malformed key", "INVALID_KEY", `INVOICE_NO "X-9" does not match`, paid("E4", "R2", "A1=X-9")},
		{"undeclared key", "INVALID_KEY", "does not declare key(s) [POLICY_NO]", undeclared},
		{"no source identity", "MISSING_KEY", "dedup identity", noIdentity},
		{"unknown event type", "UNKNOWN_CODE", "WRITE_OFF is not in the event catalog", unknownType},
		{"invoice moves to another policy", "KEY_CONFLICT", "INVOICE_NO INV1 already belongs to POLICY_NO P1, not P7", invoiced("E5", "P7", "RUN1", "INV1")},
		{"two sources disagree about an allocation", "KEY_CONFLICT", "ALLOCATION_ID R1/A1 already belongs to INVOICE_NO INV1, not INV2", func() EventInput {
			in := paid("B-77", "R1", "A1=INV2")
			in.SourceSystem = "BANK"
			return in
		}()},
		{"same item twice in one event", "KEY_CONFLICT", "items 1 and 2 have the same item key", paid("E4", "R2", "A1=INV1", "A1=INV2")},
		{"same economic event, new source id", "DUPLICATE_EVENT", "already reported economic event PAYMENT_RECEIVED:R1 as BILLING/E3", paid("E3-RESENT", "R1", "A1=INV1", "A2=INV2")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := p1Graph(t, keyDoc(t, nil))
			before := snapshot(g, t)
			if r := g.AddEvent(c.in); r != Rejected {
				t.Fatalf("want REJECTED, got %s", r)
			}
			if len(g.Exceptions) != 1 {
				t.Fatalf("one exception per rejection: %+v", g.Exceptions)
			}
			x := g.Exceptions[0]
			if x.Category != c.category || !strings.Contains(x.Detail, c.detail) || x.SourceEventID != c.in.SourceEventID {
				t.Errorf("got %+v", x)
			}
			if !reflect.DeepEqual(snapshot(g, t), before) {
				t.Error("a rejected event must leave the graph unchanged")
			}
		})
	}
}

// Tests: identity collisions — resends, replays, stale versions, corrections, other sources.
func TestIdentityCollisions(t *testing.T) {
	doc := keyDoc(t, nil)
	g := p1Graph(t, doc)
	if r := g.AddEvent(invoiced("E2", "P1", "RUN1", "INV1", "INV2")); r != Unchanged || len(g.Exceptions) != 0 {
		t.Errorf("an identical resend is idempotent, not an exception: %s %+v", r, g.Exceptions)
	}
	replay := invoiced("E2", "P1", "RUN1", "INV1", "INV3")
	if r := g.AddEvent(replay); r != Rejected || g.Exceptions[0].Category != "DUPLICATE_EVENT" || !strings.Contains(g.Exceptions[0].Detail, "different content") {
		t.Errorf("same version, different content: %s %+v", r, g.Exceptions)
	}
	// The premium also arrives from billing: another source may report the same economic
	// event (source authority decides which one posts, US-005).
	copyFromBilling := premium("P1", "1")
	copyFromBilling.SourceSystem, copyFromBilling.SourceEventID = "BILLING", "B-E1"
	if r := g.AddEvent(copyFromBilling); r != Accepted {
		t.Errorf("billing copy: %s %+v", r, g.Exceptions)
	}
	// Case-insensitive keys: "p1" is P1.
	lower := premium("p1", "1")
	lower.SourceEventID = "E1-again"
	if r := g.AddEvent(lower); r != Rejected || !strings.Contains(g.Exceptions[len(g.Exceptions)-1].Detail, "PREM_ADDED:P1:1") {
		t.Errorf("p1 and P1 are the same economic event: %s %+v", r, g.Exceptions)
	}

	// A correction (version 2) replaces what version 1 asserted: INV2 is re-keyed to INV3.
	v2 := invoiced("E2", "P1", "RUN1", "INV1", "INV3")
	v2.SourceEventVersion = 2
	if r := g.AddEvent(v2); r != Accepted {
		t.Fatalf("correction: %s %+v", r, g.Exceptions)
	}
	p1 := keyRef(t, g, "POLICY_NO", "POLICY_NO", "P1")
	if got := g.Rebuild(p1).Keys["INVOICE_NO"]; !reflect.DeepEqual(got, []string{"INV1", "INV3"}) {
		t.Errorf("after the correction P1 owns INV1 and INV3: %v", got)
	}
	if got := g.Parents(keyRef(t, g, "INVOICE_NO", "INVOICE_NO", "INV2")); got["POLICY_NO"] != "" {
		t.Errorf("version 1's link INV2 → P1 is gone: %v", got)
	}
	// A correction of the receipt may re-point an allocation: version 1's link is replaced.
	e3v2 := paid("E3", "R1", "A1=INV3", "A2=INV1")
	e3v2.SourceEventVersion = 2
	if r := g.AddEvent(e3v2); r != Accepted {
		t.Fatalf("receipt correction: %s %+v", r, g.Exceptions)
	}
	if got := g.Parents(keyRef(t, g, "ALLOCATION_ID", "RECEIPT_NO", "R1", "ALLOCATION_ID", "A1")); got["INVOICE_NO"] != "INV3" {
		t.Errorf("A1 now allocates to INV3: %v", got)
	}
	stale := invoiced("E2", "P1", "RUN1", "INV1", "INV2")
	if r := g.AddEvent(stale); r != Rejected || !strings.Contains(g.Exceptions[len(g.Exceptions)-1].Detail, "older than the accepted version 2") {
		t.Errorf("stale version: %s %+v", r, g.Exceptions)
	}

	// A correction that would conflict is rejected and version 2 stays.
	if r := g.AddEvent(invoiced("E12", "P2", "RUN1", "INV7")); r != Accepted {
		t.Fatal(g.Exceptions)
	}
	before := snapshot(g, t)
	v3 := invoiced("E2", "P1", "RUN1", "INV1", "INV7")
	v3.SourceEventVersion = 3
	if r := g.AddEvent(v3); r != Rejected || g.Exceptions[len(g.Exceptions)-1].Category != "KEY_CONFLICT" {
		t.Errorf("conflicting correction: %s %+v", r, g.Exceptions)
	}
	if !reflect.DeepEqual(snapshot(g, t), before) {
		t.Error("a rejected correction must keep the accepted version")
	}
	// After a rejected correction, version 2 is still the accepted one.
	if r := g.AddEvent(v2); r != Unchanged {
		t.Errorf("version 2 is still accepted: %s", r)
	}
}

// Tests: cross-tenant keys. Equal key values in two tenants never meet.
func TestTenantsNeverShareKeys(t *testing.T) {
	doc := keyDoc(t, nil)
	a := p1Graph(t, doc)
	b := NewKeyGraph("CLIENT_B", doc)
	theirs := invoiced("E2", "P1", "RUN1", "INV9")
	theirs.TenantID = "CLIENT_B"
	if r := b.AddEvent(theirs); r != Accepted {
		t.Fatal(b.Exceptions)
	}
	if got := a.Rebuild(keyRef(t, a, "POLICY_NO", "POLICY_NO", "P1")).Keys["INVOICE_NO"]; !reflect.DeepEqual(got, []string{"INV1", "INV2"}) {
		t.Errorf("CLIENT_A's P1: %v", got)
	}
	if got := b.Rebuild(keyRef(t, b, "POLICY_NO", "POLICY_NO", "P1")).Keys["INVOICE_NO"]; !reflect.DeepEqual(got, []string{"INV9"}) {
		t.Errorf("CLIENT_B's P1: %v", got)
	}

	before := snapshot(a, t)
	if r := a.AddEvent(theirs); r != Rejected {
		t.Fatalf("CLIENT_B's event must not enter CLIENT_A's graph: %s", r)
	}
	x := a.Exceptions[0]
	if x.Category != "TENANT_MISMATCH" || x.SourceEventID != "" || x.EconomicEventKey != "" || strings.Contains(x.Detail, "INV9") || strings.Contains(x.Detail, "CLIENT_B") {
		t.Errorf("nothing of the foreign event is echoed: %+v", x)
	}
	if !reflect.DeepEqual(snapshot(a, t), before) {
		t.Error("the graph is unchanged")
	}
	unstamped := premium("P1", "2")
	unstamped.TenantID = ""
	if r := a.AddEvent(unstamped); r != Rejected {
		t.Error("an event without a tenant is not this tenant's")
	}
}

// Scoped keys: term 1 of P1 and term 1 of P2 are different terms.
func TestScopedKeysDoNotCollide(t *testing.T) {
	doc := keyDoc(t, nil)
	g := NewKeyGraph(tenant, doc)
	for _, in := range []EventInput{premium("P1", "1"), premium("P2", "1")} {
		if r := g.AddEvent(in); r != Accepted {
			t.Fatal(g.Exceptions)
		}
	}
	if rel := g.Related(keyRef(t, g, "POLICY_NO", "POLICY_NO", "P1")); !reflect.DeepEqual(rel, map[string][]string{"TERM_NO": {"P1/1"}}) {
		t.Errorf("P1 must not meet P2 through term 1: %v", rel)
	}
	if _, err := g.Ref("TERM_NO", map[string]string{"TERM_NO": "1"}); err == nil {
		t.Error("a scoped key needs its scope")
	}
	// Escaping keeps qualified values unambiguous when values contain the separator.
	free := keyDoc(t, func(c map[string]any) {
		c["keyProfiles"] = []any{map[string]any{"code": "POLICY_NO"}, map[string]any{"code": "TERM_NO", "scope": []any{"POLICY_NO"}},
			map[string]any{"code": "INVOICE_RUN", "scope": []any{"TERM_NO"}}, map[string]any{"code": "INVOICE_NO"},
			map[string]any{"code": "RECEIPT_NO"}, map[string]any{"code": "ALLOCATION_ID", "scope": []any{"RECEIPT_NO"}}}
	})
	h := NewKeyGraph(tenant, free)
	x := keyRef(t, h, "TERM_NO", "POLICY_NO", "A/B", "TERM_NO", "1")
	y := keyRef(t, h, "TERM_NO", "POLICY_NO", "A", "TERM_NO", "B/1")
	if x == y || x.Value != `A\/B/1` || y.Value != `A/B\/1` {
		t.Errorf("%v %v", x, y)
	}
}

func TestKeyProfileValidation(t *testing.T) {
	_, fs := parse(t, p1Catalog("POLICY"))
	if !hasCode(fs, "MISSING_KEY_PROFILES") || len(errorsOf(fs)) > 0 {
		t.Errorf("no profiles is a warning, not an error: %+v", fs)
	}
	edit := func(f func(c map[string]any, ps []any)) map[string]any {
		c := p1Catalog("POLICY")
		ps := keyProfiles()
		f(c, ps)
		c["keyProfiles"] = ps
		return c
	}
	prof := func(ps []any, code string) map[string]any {
		for _, p := range ps {
			if p.(map[string]any)["code"] == code {
				return p.(map[string]any)
			}
		}
		panic(code)
	}
	cases := map[string]map[string]any{
		"INVALID_CODE":    edit(func(c map[string]any, ps []any) { prof(ps, "RECEIPT_NO")["code"] = "receipt" }),
		"DUPLICATE_KEY":   edit(func(c map[string]any, ps []any) { prof(ps, "RECEIPT_NO")["code"] = "POLICY_NO" }),
		"INVALID_PATTERN": edit(func(c map[string]any, ps []any) { prof(ps, "RECEIPT_NO")["pattern"] = "R[0-9" }),
		"INVALID_SCOPE":   edit(func(c map[string]any, ps []any) { prof(ps, "POLICY_NO")["scope"] = []any{"TERM_NO"} }),
		"INVALID_LINK": edit(func(c map[string]any, ps []any) {
			prof(ps, "RECEIPT_NO")["links"] = []any{map[string]any{"to": "POLICY_NO", "cardinality": "SOME"}}
		}),
		"UNKNOWN_KEY": edit(func(c map[string]any, ps []any) {
			prof(ps, "INVOICE_NO")["links"] = []any{map[string]any{"to": "CLAIM_NO", "cardinality": "MANY_TO_ONE"}}
		}),
		"ECONOMIC_KEY_NOT_CARRIED": edit(func(c map[string]any, ps []any) {
			c["events"].([]any)[2].(map[string]any)["keys"] = []any{"POLICY_NO"}
		}),
		"SCOPE_NOT_CARRIED": edit(func(c map[string]any, ps []any) {
			c["events"].([]any)[0].(map[string]any)["economicKey"] = []any{"TERM_NO"}
		}),
	}
	for code, c := range cases {
		_, fs := parse(t, c)
		if !hasCode(errorsOf(fs), code) {
			t.Errorf("%s: %+v", code, fs)
		}
	}
	selfLink := edit(func(c map[string]any, ps []any) {
		prof(ps, "RECEIPT_NO")["links"] = []any{map[string]any{"to": "RECEIPT_NO", "cardinality": "MANY_TO_ONE"}}
	})
	if _, fs := parse(t, selfLink); !hasCode(errorsOf(fs), "INVALID_LINK") {
		t.Errorf("self link: %+v", fs)
	}
	unprofiled := edit(func(c map[string]any, ps []any) {
		c["events"].([]any)[1].(map[string]any)["lists"].([]any)[0].(map[string]any)["keys"] = []any{"CLAIM_NO"}
	})
	if _, fs := parse(t, unprofiled); !hasCode(errorsOf(fs), "UNKNOWN_KEY") {
		t.Errorf("list key without a profile: %+v", fs)
	}
}
