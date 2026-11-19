package model

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

// US-006 — the out-of-the-box (OOTB) base measure catalog and client overrides.
//
// A base measure is a quantity a source reports (premium, tax, cash received, …). It is NOT a
// posting: it has no account and no debit/credit side; posting rules (US-012) decide how a
// measure is booked. The OOTB catalog is global and read-only; a client configuration adopts
// it implicitly, may override descriptive attributes of an OOTB measure, and may define
// custom measures. Resolution never changes the global catalog.

//go:embed ootb_measures.json
var ootbJSON []byte

// Measure is a resolved base measure.
type Measure struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Definition  string   `json:"definition"`
	DataType    string   `json:"dataType"` // AMOUNT | QUANTITY | RATE | COUNT
	Sign        string   `json:"sign"`     // POSITIVE | NEGATIVE | SIGNED
	Currency    string   `json:"currency"` // EVENT (amount in the event currency) | NONE
	Grain       string   `json:"grain"`    // EVENT | ITEM | ANY
	SourceScope []string `json:"sourceScope"`
	// Origin is OOTB (as shipped), OVERRIDDEN (OOTB with client overrides) or CUSTOM.
	Origin     string   `json:"origin"`
	Overridden []string `json:"overridden,omitempty"`
}

// BaseMeasure is one item of a configuration's baseMeasures section: either an OOTB code
// (optionally overriding name, definition, sign or sourceScope) or, with custom: true, a
// client-defined measure.
type BaseMeasure struct {
	Code        string   `json:"code"`
	Custom      bool     `json:"custom,omitempty"`
	Name        string   `json:"name,omitempty"`
	Category    string   `json:"category,omitempty"`
	Definition  string   `json:"definition,omitempty"`
	DataType    string   `json:"dataType,omitempty"`
	Sign        string   `json:"sign,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	Grain       string   `json:"grain,omitempty"`
	SourceScope []string `json:"sourceScope,omitempty"`
}

var (
	dataTypes  = []string{"AMOUNT", "QUANTITY", "RATE", "COUNT"}
	signs      = []string{"POSITIVE", "NEGATIVE", "SIGNED"}
	currencies = []string{"EVENT", "NONE"}
	grains     = []string{"EVENT", "ITEM", "ANY"}
)

type ootbCatalog struct {
	CatalogVersion string    `json:"catalogVersion"`
	Measures       []Measure `json:"measures"`
}

var ootb = func() ootbCatalog {
	var c ootbCatalog
	if err := json.Unmarshal(ootbJSON, &c); err != nil {
		panic("model: ootb_measures.json: " + err.Error())
	}
	for i := range c.Measures {
		c.Measures[i].Origin = "OOTB"
	}
	return c
}()

func cloneMeasure(m Measure) Measure {
	m.SourceScope = append([]string(nil), m.SourceScope...)
	m.Overridden = append([]string(nil), m.Overridden...)
	return m
}

// OOTBCatalogVersion identifies the shipped catalog.
func OOTBCatalogVersion() string { return ootb.CatalogVersion }

// OOTBMeasures returns copies of the global catalog, sorted by code.
func OOTBMeasures() []Measure {
	out := make([]Measure, 0, len(ootb.Measures))
	for _, m := range ootb.Measures {
		out = append(out, cloneMeasure(m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func ootbByCode() map[string]Measure {
	out := map[string]Measure{}
	for _, m := range OOTBMeasures() {
		out[m.Code] = m
	}
	return out
}

// ResolveMeasures returns the measure catalog in force for a configuration: every OOTB
// measure (with this configuration's overrides applied to a copy) plus its custom measures.
// Invalid items are skipped here; Validate reports them.
func ResolveMeasures(d *Document) map[string]Measure {
	out := ootbByCode()
	for _, b := range d.BaseMeasures {
		if b.Custom {
			if _, clash := out[b.Code]; clash {
				continue
			}
			out[b.Code] = Measure{Code: b.Code, Name: b.Name, Category: b.Category, Definition: b.Definition, DataType: b.DataType,
				Sign: b.Sign, Currency: b.Currency, Grain: b.Grain, SourceScope: append([]string(nil), b.SourceScope...), Origin: "CUSTOM"}
			continue
		}
		m, ok := out[b.Code]
		if !ok {
			continue
		}
		var changed []string
		if b.Name != "" && b.Name != m.Name {
			m.Name, changed = b.Name, append(changed, "name")
		}
		if b.Definition != "" && b.Definition != m.Definition {
			m.Definition, changed = b.Definition, append(changed, "definition")
		}
		if b.Sign != "" && b.Sign != m.Sign {
			m.Sign, changed = b.Sign, append(changed, "sign")
		}
		if len(b.SourceScope) > 0 {
			m.SourceScope, changed = append([]string(nil), b.SourceScope...), append(changed, "sourceScope")
		}
		if len(changed) > 0 {
			m.Origin, m.Overridden = "OVERRIDDEN", changed
		}
		out[b.Code] = m
	}
	return out
}

func init() { validators = append(validators, validateMeasures) }

func validateMeasures(d *Document) []Finding {
	var fs []Finding
	defaults := ootbByCode()
	seen := map[string]bool{}
	for i, b := range d.BaseMeasures {
		p := fmt.Sprintf("baseMeasures/%d", i)
		if !isCode(b.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "measure code %q must be an upper-case code", b.Code))
		}
		if seen[b.Code] {
			fs = append(fs, errf(p, "DUPLICATE_MEASURE", "measure %s is declared twice", b.Code))
		}
		seen[b.Code] = true
		def, isOOTB := defaults[b.Code]
		if b.Custom {
			if isOOTB {
				fs = append(fs, errf(p, "AMBIGUOUS_MEASURE", "custom measure %s has the code of an OOTB measure; override the OOTB measure or choose another code", b.Code))
			}
			if b.Name == "" || b.Definition == "" {
				fs = append(fs, errf(p, "UNDEFINED_MEASURE", "custom measure %s needs a name and a definition", b.Code))
			}
			if !oneOf(b.DataType, dataTypes) || !oneOf(b.Sign, signs) || !oneOf(b.Currency, currencies) || !oneOf(b.Grain, grains) {
				fs = append(fs, errf(p, "UNDEFINED_MEASURE", "custom measure %s needs dataType %v, sign %v, currency %v and grain %v", b.Code, dataTypes, signs, currencies, grains))
			}
			if b.DataType == "AMOUNT" && b.Currency != "EVENT" {
				fs = append(fs, errf(p, "INVALID_CURRENCY", "amount measure %s must be in the event currency", b.Code))
			}
			if len(b.SourceScope) == 0 {
				fs = append(fs, errf(p, "UNDEFINED_MEASURE", "custom measure %s needs a sourceScope", b.Code))
			}
		} else {
			if !isOOTB {
				fs = append(fs, errf(p, "UNKNOWN_MEASURE", "%s is not an OOTB measure; set custom: true and define it", b.Code))
				continue
			}
			for _, x := range []struct{ name, got, want string }{
				{"dataType", b.DataType, def.DataType}, {"currency", b.Currency, def.Currency},
				{"grain", b.Grain, def.Grain}, {"category", b.Category, def.Category},
			} {
				if x.got != "" && x.got != x.want {
					fs = append(fs, errf(p, "OVERRIDE_NOT_ALLOWED", "the %s of OOTB measure %s cannot be overridden (it changes what the measure is); define a custom measure instead", x.name, b.Code))
				}
			}
			if b.Sign != "" && !oneOf(b.Sign, signs) {
				fs = append(fs, errf(p, "INVALID_SIGN", "sign %q must be one of %v", b.Sign, signs))
			}
		}
		for _, k := range b.SourceScope {
			if !oneOf(k, SourceKinds) {
				fs = append(fs, errf(p, "INVALID_KIND", "sourceScope kind %q must be one of %v", k, SourceKinds))
			}
		}
	}

	// Every measure an event carries must be a known, unambiguous measure of the right grain.
	measures := ResolveMeasures(d)
	kinds := map[string]string{}
	for _, s := range d.SourceSystems {
		kinds[s.Code] = s.Kind
	}
	for i, e := range d.Events {
		p := fmt.Sprintf("events/%d", i)
		check := func(m, at string) {
			r, ok := measures[m]
			if !ok {
				fs = append(fs, errf(p, "UNKNOWN_MEASURE", "event %s carries measure %s, which is neither an OOTB nor a custom measure", e.Code, m))
				return
			}
			if (at == "EVENT" && r.Grain == "ITEM") || (at == "ITEM" && r.Grain == "EVENT") {
				fs = append(fs, errf(p, "GRAIN_MISMATCH", "event %s carries %s at %s grain but the measure's grain is %s", e.Code, m, at, r.Grain))
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
	}
	for i, a := range d.SourceAuthority {
		kind := kinds[a.SourceSystem]
		if kind == "" || kind == "CUSTOM" || kind == "SOURCE_JOURNAL" {
			continue
		}
		var ms []string
		if a.Measure == AllMeasures {
			for _, e := range d.Events {
				if e.Code == a.EventType {
					ms = carried(e)
				}
			}
		} else {
			ms = []string{a.Measure}
		}
		for _, m := range ms {
			if r, ok := measures[m]; ok && !oneOf(kind, r.SourceScope) {
				fs = append(fs, warnf(fmt.Sprintf("sourceAuthority/%d", i), "OUT_OF_SOURCE_SCOPE",
					"%s (%s) is authoritative for %s, outside the measure's source scope %v", a.SourceSystem, kind, m, r.SourceScope))
			}
		}
	}
	return fs
}
