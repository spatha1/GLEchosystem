package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every insurance event template is consistent with the standard catalogs: its measures are
// standard measures at the right grain and from its kind of source, its dimensions are
// standard dimensions, its keys are catalog keys, and its area is a catalog area.
func TestInsuranceEventCatalogIsConsistent(t *testing.T) {
	c := OOTBEventCatalog()
	if c.CatalogVersion != "gl.events.ootb/v1" || c.Status != "PROPOSED" || len(c.Events) < 15 {
		t.Fatalf("catalog: %s %s %d events", c.CatalogVersion, c.Status, len(c.Events))
	}
	measures := ootbByCode()
	areas := map[string]bool{}
	for _, a := range c.Areas {
		areas[a.Code] = true
	}
	keys := map[string]bool{}
	for _, k := range c.Keys {
		keys[k.Code] = true
	}
	seen := map[string]bool{}
	for _, e := range c.Events {
		if seen[e.Code] || !isCode(e.Code) || e.Name == "" || e.Purpose == "" || !areas[e.Area] || !oneOf(e.SourceKind, SourceKinds) {
			t.Errorf("%s: incomplete or duplicate template %+v", e.Code, e)
		}
		seen[e.Code] = true
		check := func(code, grain string) {
			m, ok := measures[code]
			switch {
			case !ok:
				t.Errorf("%s: measure %s is not standard", e.Code, code)
			case m.Grain != "ANY" && m.Grain != grain:
				t.Errorf("%s: measure %s has grain %s, used at %s", e.Code, code, m.Grain, grain)
			case !oneOf(e.SourceKind, m.SourceScope):
				t.Errorf("%s: measure %s does not come from %s sources (%v)", e.Code, code, e.SourceKind, m.SourceScope)
			}
		}
		for _, m := range e.Measures {
			check(m, "EVENT")
		}
		for _, l := range e.Lists {
			for _, m := range l.Measures {
				check(m, "ITEM")
			}
		}
		for _, x := range e.Dimensions {
			if !isOOTBDim(x) {
				t.Errorf("%s: dimension %s is not standard", e.Code, x)
			}
		}
		for _, k := range c.KeysFor(e) {
			if !keys[k.Code] {
				t.Errorf("%s: key %s is not in the catalog", e.Code, k.Code)
			}
		}
	}
	for _, want := range []string{"NEW_BUSINESS", "RENEWAL", "ENDORSEMENT", "CANCELLATION", "INVOICED", "PAYMENT_RECEIVED", "CLAIM_PAID", "RI_PREMIUM_CEDED", "PREMIUM_EARNED"} {
		if !seen[want] {
			t.Errorf("the catalog has no %s", want)
		}
	}
}

// A configuration made only of catalog templates — each with the keys and dimensions it
// needs, a source system of its kind and the authority for it — passes validation: the catalog is usable as
// it is, before the client adds rules.
func TestEveryTemplateMakesAValidEvent(t *testing.T) {
	c := OOTBEventCatalog()
	var events, auth, srcs []any
	keyAt := map[string]bool{}
	var keys []any
	kinds := map[string]bool{}
	dimAt := map[string]bool{}
	var dims []any
	for _, e := range c.Events {
		b, _ := json.Marshal(e.Event())
		var ev map[string]any
		_ = json.Unmarshal(b, &ev)
		events = append(events, ev)
		for _, k := range c.KeysFor(e) {
			if !keyAt[k.Code] {
				keyAt[k.Code] = true
				b, _ := json.Marshal(k)
				var km map[string]any
				_ = json.Unmarshal(b, &km)
				keys = append(keys, km)
			}
		}
		for _, x := range e.Dimensions {
			if !dimAt[x] {
				dimAt[x] = true
				d := map[string]any{"code": x}
				for _, def := range OOTBDimensions() {
					if def.Code == x && !def.Open {
						d["values"] = []any{"SAMPLE"} // a closed dimension takes the client's values
					}
				}
				dims = append(dims, d)
			}
		}
		if !kinds[e.SourceKind] {
			kinds[e.SourceKind] = true
			srcs = append(srcs, map[string]any{"code": e.SourceKind, "kind": e.SourceKind})
		}
		auth = append(auth, map[string]any{"eventType": e.Code, "measure": "*", "sourceSystem": e.SourceKind, "intakeMode": "RAW_EVENT"})
	}
	content := map[string]any{"sourceSystems": srcs, "events": events, "sourceAuthority": auth, "keyProfiles": keys, "dimensions": dims}
	b, _ := json.Marshal(content)
	d, fs := Parse(b)
	fs = append(fs, Validate(d)...)
	for _, f := range fs {
		if f.Severity == "error" {
			t.Errorf("%s %s: %s", f.Path, f.Code, f.Message)
		} else if f.Code != "MISSING_RULE" && f.Code != "MISSING_POSTING_RULES" && !strings.HasPrefix(f.Code, "MISSING_") {
			t.Logf("warning %s %s: %s", f.Path, f.Code, f.Message)
		}
	}
}
