package model

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"gl/formula"
)

// US-012 — posting rules and line templates.
//
// A posting rule turns one event into journal legs, as data. It names the event type, an
// optional filter, a rank and line templates. Each template gives an accounting class
// (US-011), a side, an amount formula (US-007), and optionally a list to expand by (one line
// per item, reading the item's measures), a legal-entity override and a condition.
//
// Rules compile to a checked plan: every formula is parsed and type-checked at validation,
// every line's class must be satisfiable by the event (subledger keys and dimensions, US-011),
// and two rules that could both apply at the same rank are refused. At runtime the
// lowest-ranked matching rule applies; a tie is AMBIGUOUS_RULE and no match is MISSING_RULE,
// never a guess. Lines are produced in template order, then item order, so the same input
// always gives the same plan. Accounts are chosen from classes by US-013.

// PostingRule is one item of the postingRules section.
type PostingRule struct {
	Code        string         `json:"code"`
	Description string         `json:"description,omitempty"`
	EventType   string         `json:"eventType"`
	Filter      string         `json:"filter,omitempty"` // BOOL formula; empty: every event of the type
	Rank        int            `json:"rank"`             // among matching rules the lowest rank applies
	Lines       []LineTemplate `json:"lines"`
}

// LineTemplate is one journal leg, or one leg per list item with expandBy.
type LineTemplate struct {
	Code        string `json:"code"`
	Class       string `json:"class"`
	Side        string `json:"side"`               // D | C
	Amount      string `json:"amount"`             // AMOUNT formula; a negative result flips the side
	ExpandBy    string `json:"expandBy,omitempty"` // a list of the event: one line per item
	Entity      string `json:"entity,omitempty"`   // STRING formula overriding the legal entity (default COMPANY)
	When        string `json:"when,omitempty"`     // BOOL formula; the line is produced only when true
	Explanation string `json:"explanation,omitempty"`
}

type compiledLine struct {
	LineTemplate
	list                 *EventList
	amount, entity, when formula.Node
}

type compiledRule struct {
	PostingRule
	path   string
	filter formula.Node
	lines  []compiledLine
}

func init() { validators = append(validators, validatePostingRules) }

// itemSymbols adds a list's item measures to an event's symbols, for an expanded line.
func itemSymbols(base formula.Symbols, l *EventList, measures map[string]Measure) formula.Symbols {
	if l == nil {
		return base
	}
	s := formula.Symbols{Fields: map[string]formula.Kind{}, Lists: base.Lists, Funcs: base.Funcs}
	for k, v := range base.Fields {
		s.Fields[k] = v
	}
	for _, m := range l.Measures {
		if r, ok := measures[m]; ok {
			s.Fields[m] = measureKind(r)
		}
	}
	return s
}

// compileFormula parses and type-checks one formula of a rule.
func compileFormula(path, what, src string, syms formula.Symbols, want formula.Kind, idx dimIndex) (formula.Node, []Finding) {
	node, perr := formula.Parse(src)
	if perr != nil {
		return nil, []Finding{errf(path, perr.Code, "%s %q: %s", what, src, perr.Message)}
	}
	var fs []Finding
	typ, errs := formula.Check(node, syms)
	for _, ce := range errs {
		fs = append(fs, errf(path, ce.Code, "%s %q: %s", what, src, ce.Message))
	}
	if len(errs) == 0 && typ.Kind != want && typ.Kind != formula.KNull && !(want == formula.KAmount && typ.Kind == formula.KNumber && typ.Untyped) {
		fs = append(fs, errf(path, "RESULT_TYPE_MISMATCH", "%s %q yields %s; it must yield %s", what, src, typ.Kind, want))
	}
	fs = append(fs, checkDimensionLiterals(path, src, idx)...)
	return node, fs
}

// compileRules parses and checks every posting rule.
func compileRules(d *Document) ([]compiledRule, []Finding) {
	var fs []Finding
	measures := ResolveMeasures(d)
	idx := dims(d)
	events := map[string]EventType{}
	for _, e := range d.Events {
		events[e.Code] = e
	}
	seen := map[string]bool{}
	var out []compiledRule
	for i, r := range d.PostingRules {
		p := fmt.Sprintf("postingRules/%d", i)
		cr := compiledRule{PostingRule: r, path: p}
		if !isCode(r.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "rule code %q must be an upper-case code", r.Code))
		}
		if seen[r.Code] {
			fs = append(fs, errf(p, "DUPLICATE_RULE", "rule %s is declared twice", r.Code))
		}
		seen[r.Code] = true
		ev, ok := events[r.EventType]
		if !ok {
			fs = append(fs, errf(p, "UNKNOWN_EVENT", "rule %s is for event %q, which is not in the catalog", r.Code, r.EventType))
			continue
		}
		syms := eventSymbols(d, ev, measures)
		if strings.TrimSpace(r.Filter) != "" {
			node, f := compileFormula(p, "filter", r.Filter, syms, formula.KBool, idx)
			fs = append(fs, f...)
			cr.filter = node
		}
		debit, credit := false, false
		lineCodes := map[string]bool{}
		for j, l := range r.Lines {
			lp := fmt.Sprintf("%s/lines/%d", p, j)
			cl := compiledLine{LineTemplate: l}
			if !isCode(l.Code) {
				fs = append(fs, errf(lp, "INVALID_CODE", "line code %q must be an upper-case code", l.Code))
			}
			if lineCodes[l.Code] {
				fs = append(fs, errf(lp, "DUPLICATE_LINE", "rule %s has two lines %s", r.Code, l.Code))
			}
			lineCodes[l.Code] = true
			if _, ok := ClassOf(d, l.Class); !ok {
				fs = append(fs, errf(lp, "UNKNOWN_CLASS", "line %s posts to class %q, which is not an accounting class", l.Code, l.Class))
			}
			switch l.Side {
			case "D":
				debit = true
			case "C":
				credit = true
			default:
				fs = append(fs, errf(lp, "INVALID_SIDE", "line %s side %q must be D or C", l.Code, l.Side))
			}
			if l.ExpandBy != "" {
				for k := range ev.Lists {
					if ev.Lists[k].Code == l.ExpandBy {
						cl.list = &ev.Lists[k]
					}
				}
				if cl.list == nil {
					fs = append(fs, errf(lp, "UNKNOWN_LIST", "line %s expands by %s, which is not a list of event %s", l.Code, l.ExpandBy, ev.Code))
					continue
				}
			}
			lineSyms := itemSymbols(syms, cl.list, measures)
			if strings.TrimSpace(l.Amount) == "" {
				fs = append(fs, errf(lp, "MISSING_AMOUNT", "line %s needs an amount formula", l.Code))
			} else {
				node, f := compileFormula(lp, "amount", l.Amount, lineSyms, formula.KAmount, idx)
				fs = append(fs, f...)
				cl.amount = node
			}
			if strings.TrimSpace(l.Entity) != "" {
				node, f := compileFormula(lp, "entity", l.Entity, lineSyms, formula.KString, idx)
				fs = append(fs, f...)
				cl.entity = node
			} else if !oneOf("COMPANY", eventDimensions(d, ev)) {
				fs = append(fs, errf(lp, "MISSING_ENTITY", "line %s needs a legal entity: event %s carries no COMPANY and the line sets no entity", l.Code, ev.Code))
			}
			if strings.TrimSpace(l.When) != "" {
				node, f := compileFormula(lp, "when", l.When, lineSyms, formula.KBool, idx)
				fs = append(fs, f...)
				cl.when = node
			}
			if _, ok := ClassOf(d, l.Class); ok {
				for _, f := range CheckCarrier(d, ev.Code, l.ExpandBy, l.Class) {
					f.Path = lp
					fs = append(fs, f)
				}
			}
			cr.lines = append(cr.lines, cl)
		}
		if !debit || !credit {
			fs = append(fs, errf(p, "INVALID_LINE_PLAN", "rule %s needs at least one debit and one credit line", r.Code))
		}
		out = append(out, cr)
	}
	return out, fs
}

func validatePostingRules(d *Document) []Finding {
	rules, fs := compileRules(d)
	if len(d.PostingRules) == 0 {
		if len(d.Events) > 0 {
			fs = append(fs, warnf("postingRules", "MISSING_POSTING_RULES", "no posting rules yet: no event produces a journal"))
		}
		return fs
	}
	byEvent := map[string][]compiledRule{}
	for _, r := range rules {
		byEvent[r.EventType] = append(byEvent[r.EventType], r)
	}
	for i, e := range d.Events {
		rs := byEvent[e.Code]
		if len(rs) == 0 {
			fs = append(fs, warnf(fmt.Sprintf("events/%d", i), "MISSING_RULE", "no posting rule for event %s: such events are refused (MISSING_RULE)", e.Code))
		}
		for a := 0; a < len(rs); a++ {
			for b := a + 1; b < len(rs); b++ {
				if rs[a].Rank != rs[b].Rank {
					continue
				}
				fa, fb := strings.TrimSpace(rs[a].Filter), strings.TrimSpace(rs[b].Filter)
				if fa == fb {
					fs = append(fs, errf(rs[b].path, "AMBIGUOUS_RULE", "rules %s and %s both apply to every %s event they match, at rank %d", rs[a].Code, rs[b].Code, e.Code, rs[a].Rank))
				} else {
					fs = append(fs, warnf(rs[b].path, "POSSIBLE_AMBIGUITY", "rules %s and %s share rank %d for %s: an event matching both filters is refused (AMBIGUOUS_RULE)", rs[a].Code, rs[b].Code, rs[a].Rank, e.Code))
				}
			}
		}
	}
	return fs
}

// ── planning ─────────────────────────────────────────────────────────────────

// RuleMatch says whether one rule applied to an event, and why.
type RuleMatch struct {
	Rule    string `json:"rule"`
	Rank    int    `json:"rank"`
	Filter  string `json:"filter,omitempty"`
	Matched bool   `json:"matched"`
	Reason  string `json:"reason"`
}

// PlannedLine is one journal leg of a plan, before account assignment (US-013).
type PlannedLine struct {
	LineNo      int               `json:"lineNo"`
	Template    string            `json:"template"`
	Item        int               `json:"item,omitempty"` // 1-based list item for expanded lines
	Class       string            `json:"class"`
	Side        string            `json:"side"`
	Amount      string            `json:"amount"` // at the currency's minor units
	Currency    string            `json:"currency"`
	Entity      string            `json:"entity"`
	Keys        map[string]string `json:"keys"`
	Dimensions  map[string]string `json:"dimensions"`
	Explanation string            `json:"explanation"`
	Trace       formula.Trace     `json:"trace"`
	Account     string            `json:"account,omitempty"`    // US-013
	Ledger      string            `json:"ledger,omitempty"`     // US-013
	Assignment  *Assignment       `json:"assignment,omitempty"` // US-013: rule, version and why
	Problem     string            `json:"problem,omitempty"`    // why the line cannot post
}

// BalanceTotal is one entity and currency's debits and credits.
type BalanceTotal struct {
	Entity   string `json:"entity"`
	Currency string `json:"currency"`
	Debit    string `json:"debit"`
	Credit   string `json:"credit"`
	Balanced bool   `json:"balanced"`
}

// PostingPlan is what one event posts under one configuration version.
type PostingPlan struct {
	EventType  string              `json:"eventType"`
	Rule       string              `json:"rule"`
	Rank       int                 `json:"rank"`
	Config     ConfigRef           `json:"config"`
	Considered []RuleMatch         `json:"considered"`
	Lines      []PlannedLine       `json:"lines"`
	Skipped    []string            `json:"skipped"`
	Totals     []BalanceTotal      `json:"totals"`
	Balanced   bool                `json:"balanced"`
	Dimensions []ResolvedDimension `json:"dimensions"`
	Period     *PeriodDecision     `json:"period,omitempty"`
	BookDate   string              `json:"bookDate,omitempty"` // US-013: the date accounts were assigned on
}

// scopedEnv reads a list item's fields first, then the event's.
type scopedEnv struct {
	*inputEnv
	it item
}

func (s scopedEnv) Lookup(n string) (formula.Value, bool) {
	if v, ok := s.it[n]; ok {
		return v, true
	}
	return s.inputEnv.Lookup(n)
}

// minorUnits is the number of decimals an amount in a currency posts with.
func minorUnits(ccy string) int {
	switch ccy {
	case "JPY", "KRW", "VND", "CLP", "ISK", "XAF", "XOF":
		return 0
	case "BHD", "JOD", "KWD", "OMR", "TND", "LYD", "IQD":
		return 3
	}
	return 2
}

func other(side string) string {
	if side == "D" {
		return "C"
	}
	return "D"
}

// PlanPosting chooses the posting rule for an event and produces its journal legs, with the
// reason for every choice: which rules matched, which lines were skipped, how each amount was
// computed. An unbalanced plan is returned with an UNBALANCED error.
func PlanPosting(d *Document, ref ConfigRef, in EventInput) (*PostingPlan, error) {
	rules, fs := compileRules(d)
	for _, f := range fs {
		if f.Severity == "error" {
			return nil, &CodeError{"INVALID_RULES", fmt.Sprintf("the posting rules have blocking findings (first: %s %s)", f.Path, f.Message)}
		}
	}
	eval, env, ev, err := evaluate(d, ref, in)
	if err != nil {
		return nil, err
	}
	plan := &PostingPlan{EventType: ev.Code, Config: ref, Considered: []RuleMatch{}, Lines: []PlannedLine{}, Skipped: []string{},
		Totals: []BalanceTotal{}, Dimensions: eval.Dimensions, Period: eval.Period}
	opts := formula.Options{}

	var matched []compiledRule
	for _, r := range rules {
		if r.EventType != ev.Code {
			continue
		}
		m := RuleMatch{Rule: r.Code, Rank: r.Rank, Filter: r.Filter, Matched: true, Reason: "no filter: applies to every " + ev.Code}
		if r.filter != nil {
			v, _, err := formula.Eval(r.filter, env, opts)
			if err != nil {
				return nil, fmt.Errorf("rule %s filter: %w", r.Code, err)
			}
			m.Matched = v.Kind == formula.KBool && v.Bool
			switch {
			case v.Kind == formula.KNull:
				m.Reason = "filter is null (a value it reads is missing): not matched"
			case m.Matched:
				m.Reason = "filter is true"
			default:
				m.Reason = "filter is false"
			}
		}
		plan.Considered = append(plan.Considered, m)
		if m.Matched {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return nil, &CodeError{"MISSING_RULE", fmt.Sprintf("no posting rule applies to this %s event", ev.Code)}
	}
	sort.SliceStable(matched, func(a, b int) bool { return matched[a].Rank < matched[b].Rank })
	if len(matched) > 1 && matched[0].Rank == matched[1].Rank {
		return nil, &CodeError{"AMBIGUOUS_RULE", fmt.Sprintf("rules %s and %s both match at rank %d; refusing to choose", matched[0].Code, matched[1].Code, matched[0].Rank)}
	}
	rule := matched[0]
	plan.Rule, plan.Rank = rule.Code, rule.Rank

	keys := map[string]string{}
	for k, v := range in.Keys {
		keys[k] = strings.TrimSpace(v)
	}
	dimVals := map[string]string{}
	for _, r := range eval.Dimensions {
		dimVals[r.Dimension] = r.Value
	}
	company := ""
	if v, ok := env.vals["COMPANY"]; ok && v.Kind == formula.KString {
		company = v.Str
	}

	for _, l := range rule.lines {
		type scope struct {
			env  formula.Env
			item int
			raw  map[string]string
		}
		scopes := []scope{{env: env}}
		if l.list != nil {
			scopes = nil
			for i, it := range env.lists[l.list.Code] {
				scopes = append(scopes, scope{env: scopedEnv{env, it.(item)}, item: i + 1, raw: in.Lists[l.list.Code][i]})
			}
		}
		for _, sc := range scopes {
			where := fmt.Sprintf("rule %s line %s", rule.Code, l.Code)
			if sc.item > 0 {
				where += fmt.Sprintf(" item %d", sc.item)
			}
			if l.when != nil {
				v, _, err := formula.Eval(l.when, sc.env, opts)
				if err != nil {
					return nil, fmt.Errorf("%s when: %w", where, err)
				}
				if v.Kind != formula.KBool || !v.Bool {
					plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s: condition %q is not true", where, l.When))
					continue
				}
			}
			v, tr, err := formula.Eval(l.amount, sc.env, opts)
			if err != nil {
				return nil, fmt.Errorf("%s amount: %w", where, err)
			}
			if v, err = formula.Coerce(v, formula.KAmount, in.Currency); err != nil {
				return nil, fmt.Errorf("%s amount: %w", where, err)
			}
			if v.Kind == formula.KNull {
				return nil, &CodeError{"NULL_AMOUNT", fmt.Sprintf("%s: amount %q is null (a value it reads is missing)", where, l.Amount)}
			}
			if v.Num.Sign() == 0 {
				plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s: amount is zero", where))
				continue
			}
			side, amt, note := l.Side, new(big.Rat).Set(v.Num), ""
			if amt.Sign() < 0 {
				amt.Neg(amt)
				side = other(l.Side)
				note = " (negative amount: side flipped)"
			}
			s, err := formula.Exact(amt, minorUnits(v.Currency))
			if err != nil {
				return nil, &CodeError{"UNROUNDED_AMOUNT", fmt.Sprintf("%s: %s %s has more decimals than the currency allows; ROUND it in the rule", where, formula.Minimal(amt), v.Currency)}
			}
			entity := company
			if l.entity != nil {
				ent, _, err := formula.Eval(l.entity, sc.env, opts)
				if err != nil {
					return nil, fmt.Errorf("%s entity: %w", where, err)
				}
				entity = ""
				if ent.Kind == formula.KString {
					entity = ent.Str
				}
			}
			if entity == "" {
				return nil, &CodeError{"MISSING_ENTITY", fmt.Sprintf("%s: no legal entity (COMPANY or the line's entity formula is empty)", where)}
			}
			lk := map[string]string{}
			for k, v := range keys {
				lk[k] = v
			}
			if l.list != nil {
				for _, k := range itemKeys(*l.list) {
					lk[k] = strings.TrimSpace(sc.raw[k])
				}
			}
			ld := map[string]string{}
			for k, v := range dimVals {
				ld[k] = v
			}
			expl := l.Explanation
			if expl == "" {
				expl = fmt.Sprintf("%s %s = %s", l.Class, side, l.Amount)
				if sc.item > 0 {
					expl += fmt.Sprintf(" for %s item %d", l.list.Code, sc.item)
				}
			}
			plan.Lines = append(plan.Lines, PlannedLine{LineNo: len(plan.Lines) + 1, Template: l.Code, Item: sc.item, Class: l.Class, Side: side,
				Amount: s, Currency: v.Currency, Entity: entity, Keys: lk, Dimensions: ld, Explanation: expl + note, Trace: tr})
		}
	}

	// Balance per legal entity and currency.
	type ec struct{ entity, ccy string }
	sums := map[ec][2]*big.Rat{}
	var order []ec
	for _, l := range plan.Lines {
		k := ec{l.Entity, l.Currency}
		if _, ok := sums[k]; !ok {
			sums[k] = [2]*big.Rat{new(big.Rat), new(big.Rat)}
			order = append(order, k)
		}
		r, _ := formula.ParseDecimal(l.Amount)
		if l.Side == "D" {
			sums[k][0].Add(sums[k][0], r)
		} else {
			sums[k][1].Add(sums[k][1], r)
		}
	}
	plan.Balanced = len(plan.Lines) > 0
	var off []string
	for _, k := range order {
		s := sums[k]
		ok := s[0].Cmp(s[1]) == 0
		plan.Totals = append(plan.Totals, BalanceTotal{Entity: k.entity, Currency: k.ccy, Debit: s[0].FloatString(minorUnits(k.ccy)), Credit: s[1].FloatString(minorUnits(k.ccy)), Balanced: ok})
		if !ok {
			plan.Balanced = false
			off = append(off, fmt.Sprintf("%s %s debits %s ≠ credits %s", k.entity, k.ccy, s[0].FloatString(minorUnits(k.ccy)), s[1].FloatString(minorUnits(k.ccy))))
		}
	}
	if len(plan.Lines) == 0 {
		return plan, &CodeError{"UNBALANCED", fmt.Sprintf("rule %s produced no lines for this event", rule.Code)}
	}
	if !plan.Balanced {
		return plan, &CodeError{"UNBALANCED", fmt.Sprintf("rule %s does not balance: %s", rule.Code, strings.Join(off, "; "))}
	}
	return plan, nil
}
