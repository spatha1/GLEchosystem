package model

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gl/formula"
)

// US-008 — dimensions, canonical value normalisation, lookups (derived dimensions) and filters.
//
// A dimension is a classifying attribute (LOB, company, product, …). Its canonical values are
// client configuration; each source's own spellings are normalised to them. A value no rule
// maps is UNKNOWN_CODE — never silently defaulted. Lookups derive one dimension from another
// (BANK_ACCOUNT → BANKING_COMPANY); they live inside the versioned configuration, so every
// change is versioned, maker-checked and audited by the configuration store.

//go:embed ootb_dimensions.json
var ootbDimJSON []byte

// DimensionDef is an OOTB dimension definition.
type DimensionDef struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Open        bool   `json:"open"`
}

var ootbDims = func() (c struct {
	CatalogVersion string         `json:"catalogVersion"`
	Dimensions     []DimensionDef `json:"dimensions"`
}) {
	if err := json.Unmarshal(ootbDimJSON, &c); err != nil {
		panic("model: ootb_dimensions.json: " + err.Error())
	}
	return c
}()

// OOTBDimensions returns copies of the global dimension catalog.
func OOTBDimensions() []DimensionDef { return append([]DimensionDef(nil), ootbDims.Dimensions...) }

// OOTBDimensionCatalogVersion identifies the shipped dimension catalog.
func OOTBDimensionCatalogVersion() string { return ootbDims.CatalogVersion }

// Dimension is one item of the dimensions section: an OOTB code (adding the client's values
// and normalisations) or, with custom: true, a client dimension.
type Dimension struct {
	Code           string          `json:"code"`
	Custom         bool            `json:"custom,omitempty"`
	Description    string          `json:"description,omitempty"`
	Open           *bool           `json:"open,omitempty"`   // any value allowed (identifiers); default from the OOTB definition
	Values         []string        `json:"values,omitempty"` // the canonical values of a closed dimension
	Normalizations []Normalization `json:"normalizations,omitempty"`
	DerivedBy      *Derivation     `json:"derivedBy,omitempty"`
}

// Normalization maps one source system's spellings to canonical values.
type Normalization struct {
	SourceSystem    string            `json:"sourceSystem"`
	CaseInsensitive bool              `json:"caseInsensitive,omitempty"`
	Map             map[string]string `json:"map"`
}

// Derivation says how a derived dimension gets its value.
type Derivation struct {
	Lookup string `json:"lookup"`
}

// Lookup derives the Output dimension from the Input dimension.
type Lookup struct {
	Code        string        `json:"code"`
	Description string        `json:"description,omitempty"`
	Input       string        `json:"input"`
	Output      string        `json:"output"`
	Entries     []LookupEntry `json:"entries"`
}

// LookupEntry is one key → value row, optionally effective-dated [from, to).
type LookupEntry struct {
	Key           string `json:"key"`
	Value         string `json:"value"`
	EffectiveFrom string `json:"effectiveFrom,omitempty"`
	EffectiveTo   string `json:"effectiveTo,omitempty"`
}

// ResolvedDimension is a dimension value as one event carries it, with how it was obtained.
type ResolvedDimension struct {
	Dimension string    `json:"dimension"`
	Value     string    `json:"value"`
	Via       string    `json:"via"` // EVENT or LOOKUP
	Lookup    string    `json:"lookup,omitempty"`
	Key       string    `json:"key,omitempty"`
	Config    ConfigRef `json:"config"`
}

// dimIndex is the configured dimensions by code, with OOTB openness applied.
type dimIndex map[string]Dimension

func (d dimIndex) open(code string) bool {
	x := d[code]
	if x.Open != nil {
		return *x.Open
	}
	for _, o := range ootbDims.Dimensions {
		if o.Code == code {
			return o.Open
		}
	}
	return false
}

func dims(d *Document) dimIndex {
	out := dimIndex{}
	for _, x := range d.Dimensions {
		out[x.Code] = x
	}
	return out
}

func isOOTBDim(code string) bool {
	for _, o := range ootbDims.Dimensions {
		if o.Code == code {
			return true
		}
	}
	return false
}

func init() {
	validators = append(validators, validateDimensions)
	symbolProviders = append(symbolProviders, dimensionSymbols)
}

// dimensionSymbols makes an event's dimensions, and the dimensions derivable from them,
// readable (as strings) in its formulas and filters.
func dimensionSymbols(d *Document, e EventType, s *formula.Symbols) {
	for _, code := range eventDimensions(d, e) {
		s.Fields[code] = formula.KString
	}
}

// eventDimensions lists the dimensions an event carries plus those derivable from them.
func eventDimensions(d *Document, e EventType) []string {
	out := append([]string{}, e.Dimensions...)
	have := map[string]bool{}
	for _, x := range out {
		have[x] = true
	}
	for changed := true; changed; {
		changed = false
		for _, x := range d.Dimensions {
			if x.DerivedBy == nil || have[x.Code] {
				continue
			}
			for _, l := range d.Lookups {
				if l.Code == x.DerivedBy.Lookup && have[l.Input] {
					out = append(out, x.Code)
					have[x.Code] = true
					changed = true
				}
			}
		}
	}
	return out
}

func validateDimensions(d *Document) []Finding {
	var fs []Finding
	idx := dims(d)
	seen := map[string]bool{}
	systems := map[string]bool{}
	for _, s := range d.SourceSystems {
		systems[s.Code] = true
	}
	lookups := map[string]Lookup{}
	for _, l := range d.Lookups {
		lookups[l.Code] = l
	}
	for i, x := range d.Dimensions {
		p := fmt.Sprintf("dimensions/%d", i)
		if !isCode(x.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "dimension code %q must be an upper-case code", x.Code))
		}
		if seen[x.Code] {
			fs = append(fs, errf(p, "DUPLICATE_DIMENSION", "dimension %s is declared twice", x.Code))
		}
		seen[x.Code] = true
		switch {
		case x.Custom && isOOTBDim(x.Code):
			fs = append(fs, errf(p, "AMBIGUOUS_DIMENSION", "custom dimension %s has the code of an OOTB dimension", x.Code))
		case !x.Custom && !isOOTBDim(x.Code):
			fs = append(fs, errf(p, "UNKNOWN_DIMENSION", "%s is not an OOTB dimension; set custom: true and define it", x.Code))
		}
		values := map[string]bool{}
		for _, v := range x.Values {
			if strings.TrimSpace(v) == "" || values[v] {
				fs = append(fs, errf(p, "INVALID_VALUE", "dimension %s: values must be non-empty and unique (%q)", x.Code, v))
			}
			values[v] = true
		}
		open := idx.open(x.Code)
		if !open && x.DerivedBy == nil && len(x.Values) == 0 {
			fs = append(fs, errf(p, "MISSING_VALUES", "closed dimension %s needs its canonical values (or open: true for identifiers)", x.Code))
		}
		for j, n := range x.Normalizations {
			np := fmt.Sprintf("%s/normalizations/%d", p, j)
			if !systems[n.SourceSystem] {
				fs = append(fs, errf(np, "UNKNOWN_SOURCE", "normalisation for unknown source system %q", n.SourceSystem))
			}
			folded := map[string]string{}
			keys := sortedKeys(n.Map)
			for _, raw := range keys {
				target := n.Map[raw]
				if !open && !values[target] {
					fs = append(fs, errf(np, "UNKNOWN_DIMENSION_VALUE", "%s maps %q to %q, which is not a value of %s", n.SourceSystem, raw, target, x.Code))
				}
				if n.CaseInsensitive {
					k := strings.ToUpper(raw)
					if prev, dup := folded[k]; dup && prev != target {
						fs = append(fs, errf(np, "AMBIGUOUS_NORMALIZATION", "%q maps to both %q and %q when case is ignored", raw, prev, target))
					}
					folded[k] = target
				}
			}
		}
		if x.DerivedBy != nil {
			l, ok := lookups[x.DerivedBy.Lookup]
			switch {
			case !ok:
				fs = append(fs, errf(p, "UNKNOWN_LOOKUP", "dimension %s is derived by lookup %q, which is not configured", x.Code, x.DerivedBy.Lookup))
			case l.Output != x.Code:
				fs = append(fs, errf(p, "LOOKUP_MISMATCH", "lookup %s produces %s, not %s", l.Code, l.Output, x.Code))
			}
		}
	}
	seenL := map[string]bool{}
	for i, l := range d.Lookups {
		p := fmt.Sprintf("lookups/%d", i)
		if !isCode(l.Code) || seenL[l.Code] {
			fs = append(fs, errf(p, "INVALID_CODE", "lookup code %q must be a unique upper-case code", l.Code))
		}
		seenL[l.Code] = true
		_, inOK := idx[l.Input]
		out, outOK := idx[l.Output]
		if !inOK {
			fs = append(fs, errf(p, "UNKNOWN_DIMENSION", "lookup %s reads dimension %s, which is not configured", l.Code, l.Input))
		}
		if !outOK {
			fs = append(fs, errf(p, "UNKNOWN_DIMENSION", "lookup %s produces dimension %s, which is not configured", l.Code, l.Output))
		}
		outValues := map[string]bool{}
		for _, v := range out.Values {
			outValues[v] = true
		}
		byKey := map[string][]LookupEntry{}
		for j, en := range l.Entries {
			ep := fmt.Sprintf("%s/entries/%d", p, j)
			if strings.TrimSpace(en.Key) == "" {
				fs = append(fs, errf(ep, "INVALID_VALUE", "lookup entries need a key"))
			}
			if outOK && !idx.open(l.Output) && !outValues[en.Value] {
				fs = append(fs, errf(ep, "UNKNOWN_DIMENSION_VALUE", "lookup %s maps %q to %q, which is not a value of %s", l.Code, en.Key, en.Value, l.Output))
			}
			if _, ok := window(SourceAuthority{EffectiveFrom: en.EffectiveFrom, EffectiveTo: en.EffectiveTo}); !ok {
				fs = append(fs, errf(ep, "INVALID_WINDOW", "effectiveFrom/effectiveTo must be real dates with effectiveTo after effectiveFrom"))
			}
			for _, prev := range byKey[en.Key] {
				if overlaps(SourceAuthority{EffectiveFrom: prev.EffectiveFrom, EffectiveTo: prev.EffectiveTo}, SourceAuthority{EffectiveFrom: en.EffectiveFrom, EffectiveTo: en.EffectiveTo}) {
					fs = append(fs, errf(ep, "AMBIGUOUS_LOOKUP", "lookup %s has two entries for key %q in overlapping windows", l.Code, en.Key))
				}
			}
			byKey[en.Key] = append(byKey[en.Key], en)
		}
	}
	// Events may only carry configured dimensions.
	for i, e := range d.Events {
		for _, code := range e.Dimensions {
			if _, ok := idx[code]; !ok {
				fs = append(fs, errf(fmt.Sprintf("events/%d", i), "UNKNOWN_DIMENSION", "event %s carries dimension %s, which is not configured", e.Code, code))
			}
		}
	}
	// Filters and formulas may only compare a closed dimension with values it has.
	for i, dm := range d.DerivedMeasures {
		fs = append(fs, checkDimensionLiterals(fmt.Sprintf("derivedMeasures/%d", i), dm.Formula, idx)...)
	}
	return fs
}

// checkDimensionLiterals rejects `LOB == 'MOTR'`: a comparison of a closed dimension with a
// value that dimension does not have can never be true and is almost always a typo.
func checkDimensionLiterals(path, src string, idx dimIndex) []Finding {
	node, perr := formula.Parse(src)
	if perr != nil {
		return nil // reported by the formula's own validation
	}
	var fs []Finding
	for _, c := range formula.StringComparisons(node) {
		dim, ok := idx[c.Field]
		if !ok || idx.open(c.Field) || len(dim.Values) == 0 {
			continue
		}
		for _, v := range c.Values {
			if !oneOf(v, dim.Values) {
				fs = append(fs, errf(path, "UNKNOWN_DIMENSION_VALUE", "%q is not a value of %s (values: %s)", v, c.Field, strings.Join(dim.Values, ", ")))
			}
		}
	}
	return fs
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CodeError is a runtime dimension or lookup failure; it becomes an UNKNOWN_CODE exception.
type CodeError struct {
	Code    string
	Message string
}

func (e *CodeError) Error() string { return e.Code + ": " + e.Message }

// Normalize turns one source's raw value of a dimension into its canonical value. A value
// that is already canonical is kept; otherwise the source's normalisation map applies; an
// open dimension keeps the raw value; anything else is UNKNOWN_CODE.
func Normalize(d *Document, dimension, sourceSystem, raw string) (string, error) {
	idx := dims(d)
	x, ok := idx[dimension]
	if !ok {
		return "", &CodeError{"UNKNOWN_DIMENSION", dimension + " is not configured"}
	}
	if oneOf(raw, x.Values) {
		return raw, nil
	}
	for _, n := range x.Normalizations {
		if n.SourceSystem != sourceSystem {
			continue
		}
		if v, ok := n.Map[raw]; ok {
			return v, nil
		}
		if n.CaseInsensitive {
			for _, k := range sortedKeys(n.Map) {
				if strings.EqualFold(k, raw) {
					return n.Map[k], nil
				}
			}
		}
	}
	if idx.open(dimension) && strings.TrimSpace(raw) != "" {
		return raw, nil
	}
	return "", &CodeError{"UNKNOWN_CODE", fmt.Sprintf("%s value %q from %s is not a known value and no normalisation maps it", dimension, raw, sourceSystem)}
}

// ResolveDimensions checks an event's (canonical) dimension values and derives the
// dimensions its lookups provide, on the event's effective date. Every derived value records
// the lookup, the key and the configuration version that produced it.
func ResolveDimensions(d *Document, ref ConfigRef, e EventType, values map[string]string, day time.Time) ([]ResolvedDimension, error) {
	idx := dims(d)
	have := map[string]string{}
	var out []ResolvedDimension
	for _, code := range e.Dimensions {
		v, ok := values[code]
		if !ok || v == "" {
			continue // a missing dimension is null to formulas; filters decide what that means
		}
		x := idx[code]
		if !idx.open(code) && !oneOf(v, x.Values) {
			return nil, &CodeError{"UNKNOWN_CODE", fmt.Sprintf("%s value %q is not one of %v", code, v, x.Values)}
		}
		have[code] = v
		out = append(out, ResolvedDimension{Dimension: code, Value: v, Via: "EVENT", Config: ref})
	}
	for k := range values {
		if !oneOf(k, e.Dimensions) {
			return nil, &CodeError{"UNKNOWN_DIMENSION", fmt.Sprintf("event %s does not carry dimension %s", e.Code, k)}
		}
	}
	for _, code := range eventDimensions(d, e) {
		x := idx[code]
		if x.DerivedBy == nil {
			continue
		}
		var l Lookup
		for _, c := range d.Lookups {
			if c.Code == x.DerivedBy.Lookup {
				l = c
			}
		}
		key, ok := have[l.Input]
		if !ok {
			continue
		}
		var hits []LookupEntry
		for _, en := range l.Entries {
			if en.Key == key && covers(SourceAuthority{EffectiveFrom: en.EffectiveFrom, EffectiveTo: en.EffectiveTo}, day) {
				hits = append(hits, en)
			}
		}
		switch len(hits) {
		case 0:
			return nil, &CodeError{"UNKNOWN_CODE", fmt.Sprintf("lookup %s has no entry for %s %q on %s", l.Code, l.Input, key, day.Format("2006-01-02"))}
		case 1:
		default:
			return nil, &CodeError{"AMBIGUOUS_LOOKUP", fmt.Sprintf("lookup %s has %d entries for %q on %s", l.Code, len(hits), key, day.Format("2006-01-02"))}
		}
		have[code] = hits[0].Value
		out = append(out, ResolvedDimension{Dimension: code, Value: hits[0].Value, Via: "LOOKUP", Lookup: l.Code, Key: key, Config: ref})
	}
	return out, nil
}
