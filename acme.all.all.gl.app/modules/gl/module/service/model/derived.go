package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"gl/formula"
)

// US-007 — derived measures: GROSS = PREMIUM + TAX and similar, expressed in the bounded
// formula language (package formula), checked statically at validation and evaluated with
// full lineage at runtime.

// DerivedMeasure is one item of the derivedMeasures section.
type DerivedMeasure struct {
	Code        string   `json:"code"`
	Description string   `json:"description,omitempty"`
	Formula     string   `json:"formula"`
	ResultType  string   `json:"resultType"`           // AMOUNT | NUMBER | BOOL | STRING | DATE
	EventTypes  []string `json:"eventTypes,omitempty"` // where it applies; default: every event whose fields it can read
}

var resultKinds = map[string]formula.Kind{
	"AMOUNT": formula.KAmount, "NUMBER": formula.KNumber, "BOOL": formula.KBool, "STRING": formula.KString, "DATE": formula.KDate,
}

// FormulaDigest identifies a formula's exact text.
func FormulaDigest(f string) string {
	sum := sha256.Sum256([]byte(f))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// measureKind is how a resolved base measure appears in a formula.
func measureKind(m Measure) formula.Kind {
	if m.DataType == "AMOUNT" {
		return formula.KAmount
	}
	return formula.KNumber
}

// eventSymbols lists what a formula may read for one event type: the event's measures, its
// lists' measures, and every derived measure (by declared type). Later stories add fields
// (dimensions, keys, dates) through symbolProviders.
func eventSymbols(d *Document, e EventType, measures map[string]Measure) formula.Symbols {
	s := formula.Symbols{Fields: map[string]formula.Kind{}, Lists: map[string]map[string]formula.Kind{}, Funcs: map[string]formula.FuncSig{}}
	for _, m := range e.Measures {
		if r, ok := measures[m]; ok {
			s.Fields[m] = measureKind(r)
		}
	}
	for _, l := range e.Lists {
		fields := map[string]formula.Kind{}
		for _, m := range l.Measures {
			if r, ok := measures[m]; ok {
				fields[m] = measureKind(r)
			}
		}
		s.Lists[l.Code] = fields
	}
	for _, dm := range d.DerivedMeasures {
		if k, ok := resultKinds[dm.ResultType]; ok {
			s.Fields[dm.Code] = k
		}
	}
	for _, p := range symbolProviders {
		p(d, e, &s)
	}
	return s
}

// symbolProviders let later stories add readable fields to an event's symbols.
var symbolProviders []func(d *Document, e EventType, s *formula.Symbols)

func init() { validators = append(validators, validateDerived) }

func validateDerived(d *Document) []Finding {
	var fs []Finding
	measures := ResolveMeasures(d)
	events := map[string]EventType{}
	for _, e := range d.Events {
		events[e.Code] = e
	}
	seen := map[string]bool{}
	deps := map[string][]string{}
	for i, dm := range d.DerivedMeasures {
		p := fmt.Sprintf("derivedMeasures/%d", i)
		if !isCode(dm.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "derived measure code %q must be an upper-case code", dm.Code))
		}
		if seen[dm.Code] {
			fs = append(fs, errf(p, "DUPLICATE_MEASURE", "derived measure %s is declared twice", dm.Code))
		}
		seen[dm.Code] = true
		if _, clash := measures[dm.Code]; clash {
			fs = append(fs, errf(p, "AMBIGUOUS_MEASURE", "%s is already a base measure; a derived measure needs its own code", dm.Code))
		}
		want, ok := resultKinds[dm.ResultType]
		if !ok {
			fs = append(fs, errf(p, "INVALID_RESULT_TYPE", "resultType %q must be one of AMOUNT, NUMBER, BOOL, STRING, DATE", dm.ResultType))
		}
		node, perr := formula.Parse(dm.Formula)
		if perr != nil {
			fs = append(fs, errf(p, perr.Code, "formula %q: %s", dm.Formula, perr.Message))
			continue
		}
		for _, id := range formula.Identifiers(node) {
			if _, isDerived := findDerived(d, id); isDerived {
				deps[dm.Code] = append(deps[dm.Code], id)
			}
		}
		// Check against each event the measure applies to.
		var scope []EventType
		if len(dm.EventTypes) > 0 {
			for _, code := range dm.EventTypes {
				e, ok := events[code]
				if !ok {
					fs = append(fs, errf(p, "UNKNOWN_EVENT", "derived measure %s names event %s, which is not in the catalog", dm.Code, code))
					continue
				}
				scope = append(scope, e)
			}
		} else {
			scope = d.Events
		}
		applies := 0
		var firstErrs []*formula.Error
		for _, e := range scope {
			typ, errs := formula.Check(node, eventSymbols(d, e, measures))
			if len(errs) > 0 {
				if len(dm.EventTypes) > 0 {
					for _, ce := range errs {
						fs = append(fs, errf(p, ce.Code, "formula %q for event %s: %s", dm.Formula, e.Code, ce.Message))
					}
				} else if firstErrs == nil {
					firstErrs = errs
				}
				continue
			}
			applies++
			if ok && typ.Kind != want && !(want == formula.KAmount && typ.Kind == formula.KNumber && typ.Untyped) && typ.Kind != formula.KNull {
				fs = append(fs, errf(p, "RESULT_TYPE_MISMATCH", "formula %q yields %s for event %s but resultType is %s", dm.Formula, typ.Kind, e.Code, dm.ResultType))
			}
		}
		if len(dm.EventTypes) == 0 && applies == 0 {
			msg := "it reads fields no event carries"
			if len(firstErrs) > 0 {
				msg = firstErrs[0].Message
			}
			code := "UNRESOLVABLE_FORMULA"
			if len(firstErrs) > 0 {
				code = firstErrs[0].Code
			}
			fs = append(fs, errf(p, code, "derived measure %s applies to no event: %s", dm.Code, msg))
		}
	}
	for _, cyc := range cycles(deps) {
		fs = append(fs, errf("derivedMeasures", "CYCLE", "derived measures depend on each other in a cycle: %s", strings.Join(cyc, " → ")))
	}
	return fs
}

func findDerived(d *Document, code string) (DerivedMeasure, bool) {
	for _, dm := range d.DerivedMeasures {
		if dm.Code == code {
			return dm, true
		}
	}
	return DerivedMeasure{}, false
}

// cycles returns each dependency cycle once, as a path that starts and ends at the same node.
func cycles(deps map[string][]string) [][]string {
	var out [][]string
	state := map[string]int{} // 0 new, 1 on stack, 2 done
	var stack []string
	reported := map[string]bool{}
	var visit func(n string)
	visit = func(n string) {
		state[n] = 1
		stack = append(stack, n)
		for _, m := range deps[n] {
			switch state[m] {
			case 0:
				visit(m)
			case 1:
				i := len(stack) - 1
				for stack[i] != m {
					i--
				}
				cyc := append(append([]string{}, stack[i:]...), m)
				key := canonicalCycle(cyc)
				if !reported[key] {
					reported[key] = true
					out = append(out, cyc)
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = 2
	}
	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if state[n] == 0 {
			visit(n)
		}
	}
	return out
}

func canonicalCycle(c []string) string {
	ring := c[:len(c)-1]
	min := 0
	for i := range ring {
		if ring[i] < ring[min] {
			min = i
		}
	}
	return strings.Join(append(append([]string{}, ring[min:]...), ring[:min]...), ">")
}

// ── evaluation ───────────────────────────────────────────────────────────────

// ConfigRef identifies the configuration version an evaluation runs under; it is stamped on
// every lineage record so a result can always be traced to the exact approved rules.
type ConfigRef struct {
	ConfigID      string `json:"configId"`
	Version       int    `json:"configVersion"`
	ContentDigest string `json:"contentDigest"`
}

// EventInput is the data of one event for evaluation: measure values as decimal strings in
// the event currency, and list items (a list item may carry "@currency" when it differs).
type EventInput struct {
	EventType       string                         `json:"eventType"`
	Currency        string                         `json:"currency"`
	TransactionDate string                         `json:"transactionDate,omitempty"`
	EffectiveDate   string                         `json:"effectiveDate,omitempty"`
	RecordedAt      string                         `json:"recordedAt,omitempty"` // RFC 3339 instant
	AsOf            string                         `json:"asOf,omitempty"`       // processing date for period decisions (default: the transaction date)
	Measures        map[string]string              `json:"measures"`
	Dimensions      map[string]string              `json:"dimensions,omitempty"` // canonical values (normalised at intake)
	Lists           map[string][]map[string]string `json:"lists,omitempty"`
}

// Evaluation is everything evaluating one event produces: its resolved dimensions (with the
// lookups that derived them), its period decision, and its derived measures, with lineage.
type Evaluation struct {
	Dimensions []ResolvedDimension `json:"dimensions"`
	Period     *PeriodDecision     `json:"period,omitempty"`
	Derived    []Derived           `json:"derived"`
}

func dateOrNull(s string) formula.Value {
	if s == "" {
		return formula.Null
	}
	return formula.Date(s)
}

// Derived is one evaluated derived measure with its lineage.
type Derived struct {
	Code          string        `json:"code"`
	Value         string        `json:"value"` // "null" when null; amounts as "<decimal> <CCY>"
	Formula       string        `json:"formula"`
	FormulaDigest string        `json:"formulaDigest"`
	Config        ConfigRef     `json:"config"`
	Trace         formula.Trace `json:"trace"`
	value         formula.Value
}

// Val is the typed value.
func (d Derived) Val() formula.Value { return d.value }

type inputEnv struct {
	vals  map[string]formula.Value
	lists map[string][]formula.Item
	calls func(name string, args []formula.Value) (formula.Value, bool, error)
}

type item map[string]formula.Value

func (it item) Lookup(f string) (formula.Value, bool) { v, ok := it[f]; return v, ok }

func (e *inputEnv) Lookup(n string) (formula.Value, bool) { v, ok := e.vals[n]; return v, ok }
func (e *inputEnv) List(n string) ([]formula.Item, bool)  { l, ok := e.lists[n]; return l, ok }
func (e *inputEnv) Call(n string, a []formula.Value) (formula.Value, bool, error) {
	if e.calls == nil {
		return formula.Null, false, nil
	}
	return e.calls(n, a)
}

func toValue(m Measure, s, currency string) (formula.Value, error) {
	if s == "" {
		return formula.Null, nil
	}
	r, err := formula.ParseDecimal(s)
	if err != nil {
		return formula.Null, err
	}
	if m.DataType == "AMOUNT" {
		return formula.Amount(r, currency), nil
	}
	return formula.Number(r), nil
}

// EvaluateDerived evaluates every derived measure that applies to the input's event type, in
// dependency order, and returns each value with its lineage. An unknown measure in the
// input, a bad number, or any formula error is returned as an error — never skipped.
func EvaluateDerived(d *Document, ref ConfigRef, in EventInput) ([]Derived, error) {
	e, err := EvaluateEvent(d, ref, in)
	if err != nil {
		return nil, err
	}
	return e.Derived, nil
}

// EvaluateEvent resolves an event's dimensions (lookups included) and evaluates its derived
// measures, in dependency order, with lineage.
func EvaluateEvent(d *Document, ref ConfigRef, in EventInput) (Evaluation, error) {
	var ev EventType
	found := false
	for _, e := range d.Events {
		if e.Code == in.EventType {
			ev, found = e, true
		}
	}
	if !found {
		return Evaluation{}, &formula.Error{Code: "UNKNOWN_EVENT", Message: in.EventType + " is not in the event catalog"}
	}
	if !currencyRe(in.Currency) {
		return Evaluation{}, &formula.Error{Code: "INVALID_CURRENCY", Message: fmt.Sprintf("currency %q must be an ISO 4217 code", in.Currency)}
	}
	measures := ResolveMeasures(d)
	env := &inputEnv{vals: map[string]formula.Value{}, lists: map[string][]formula.Item{}}
	for _, code := range ev.Measures {
		v, err := toValue(measures[code], in.Measures[code], in.Currency)
		if err != nil {
			return Evaluation{}, &formula.Error{Code: "INVALID_INPUT", Message: fmt.Sprintf("%s: %v", code, err)}
		}
		env.vals[code] = v
	}
	for k := range in.Measures {
		if !oneOf(k, ev.Measures) {
			return Evaluation{}, &formula.Error{Code: "UNKNOWN_IDENTIFIER", Message: fmt.Sprintf("event %s does not carry measure %s", ev.Code, k)}
		}
	}
	for _, l := range ev.Lists {
		var items []formula.Item
		for i, raw := range in.Lists[l.Code] {
			ccy := in.Currency
			if c, ok := raw["@currency"]; ok {
				ccy = c
			}
			it := item{}
			for _, m := range l.Measures {
				v, err := toValue(measures[m], raw[m], ccy)
				if err != nil {
					return Evaluation{}, &formula.Error{Code: "INVALID_INPUT", Message: fmt.Sprintf("%s item %d %s: %v", l.Code, i, m, err)}
				}
				it[m] = v
			}
			items = append(items, it)
		}
		env.lists[l.Code] = items
	}

	dates := EventDates{TransactionDate: in.TransactionDate, EffectiveDate: in.EffectiveDate, RecordedAt: in.RecordedAt}
	for name, s := range map[string]string{"transactionDate": in.TransactionDate, "effectiveDate": in.EffectiveDate} {
		if _, ok := ParseDay(s); s != "" && !ok {
			return Evaluation{}, &formula.Error{Code: "INVALID_INPUT", Message: name + " must be a real date (YYYY-MM-DD)"}
		}
	}
	// The date that selects effective-dated lookups is the policy's lookupDate (default the
	// effective date); without dates, only entries with open windows can match.
	day, _ := lookupDay(d, ev.Code, dates)
	resolved, err := ResolveDimensions(d, ref, ev, in.Dimensions, day)
	if err != nil {
		return Evaluation{}, err
	}
	for _, code := range eventDimensions(d, ev) {
		env.vals[code] = formula.Null
	}
	for _, r := range resolved {
		env.vals[r.Dimension] = formula.String(r.Value)
	}
	env.vals["TRANSACTION_DATE"] = dateOrNull(in.TransactionDate)
	env.vals["EFFECTIVE_DATE"] = dateOrNull(in.EffectiveDate)

	// Period decision (US-009): book date, accounting period, booking class.
	var period *PeriodDecision
	if _, has := policyFor(d, ev.Code); has {
		for _, k := range []string{"BOOK_DATE", "ACCOUNTING_PERIOD", "BOOKING_CLASS"} {
			env.vals[k] = formula.Null
		}
		if in.TransactionDate != "" && in.EffectiveDate != "" {
			asOf := in.AsOf
			if asOf == "" {
				asOf = in.TransactionDate
			}
			at, ok := ParseDay(asOf)
			if !ok {
				return Evaluation{}, &formula.Error{Code: "INVALID_INPUT", Message: "asOf must be a real date (YYYY-MM-DD)"}
			}
			dec, err := DecidePeriod(d, ev.Code, dates, at)
			if err != nil {
				return Evaluation{}, err
			}
			period = &dec
			env.vals["BOOK_DATE"] = formula.Date(dec.BookDate)
			env.vals["ACCOUNTING_PERIOD"] = formula.String(dec.AccountingPeriod)
			if dec.BookingClass != "" {
				env.vals["BOOKING_CLASS"] = formula.String(dec.BookingClass)
				resolved = append(resolved, ResolvedDimension{Dimension: "BOOKING_CLASS", Value: dec.BookingClass, Via: "DATE_POLICY", Lookup: dec.Policy, Config: ref})
			}
		}
	}
	if resolved == nil {
		resolved = []ResolvedDimension{}
	}

	syms := eventSymbols(d, ev, measures)
	order, err := topoOrder(d)
	if err != nil {
		return Evaluation{}, err
	}
	var out []Derived
	for _, dm := range order {
		if len(dm.EventTypes) > 0 && !oneOf(ev.Code, dm.EventTypes) {
			continue
		}
		node, perr := formula.Parse(dm.Formula)
		if perr != nil {
			return Evaluation{}, perr
		}
		if _, errs := formula.Check(node, syms); len(errs) > 0 {
			continue // does not apply to this event (validation guarantees it applies somewhere)
		}
		v, tr, err := formula.Eval(node, env, formula.Options{})
		if err != nil {
			return Evaluation{}, fmt.Errorf("%s: %w", dm.Code, err)
		}
		v, err = formula.Coerce(v, resultKinds[dm.ResultType], in.Currency)
		if err != nil {
			return Evaluation{}, fmt.Errorf("%s: %w", dm.Code, err)
		}
		env.vals[dm.Code] = v
		out = append(out, Derived{Code: dm.Code, Value: v.String(), Formula: dm.Formula, FormulaDigest: FormulaDigest(dm.Formula), Config: ref, Trace: tr, value: v})
	}
	return Evaluation{Dimensions: resolved, Period: period, Derived: out}, nil
}

// topoOrder orders derived measures so each comes after those it reads.
func topoOrder(d *Document) ([]DerivedMeasure, error) {
	var out []DerivedMeasure
	state := map[string]int{}
	var visit func(dm DerivedMeasure) error
	visit = func(dm DerivedMeasure) error {
		switch state[dm.Code] {
		case 1:
			return &formula.Error{Code: "CYCLE", Message: "derived measure " + dm.Code + " depends on itself"}
		case 2:
			return nil
		}
		state[dm.Code] = 1
		node, perr := formula.Parse(dm.Formula)
		if perr != nil {
			return perr
		}
		for _, id := range formula.Identifiers(node) {
			if dep, ok := findDerived(d, id); ok {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		state[dm.Code] = 2
		out = append(out, dm)
		return nil
	}
	for _, dm := range d.DerivedMeasures {
		if err := visit(dm); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func currencyRe(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}
