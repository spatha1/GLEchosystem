package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// US-013 — account assignment: accounting class + dimensions → account, by ranked match rules.
//
// An assignment rule maps a class to an account when the line's dimensions match: each match
// entry is an exact value or "*" (any value, but one must be present); dimensions a rule does
// not name do not matter. ENTITY and CURRENCY match the line's legal entity and currency.
//
// Precedence: lower priority first; then more exact matches; then more wildcards; then the
// later effectiveFrom (a dated override beats a standing rule). Two rules that could match the
// same line with the same precedence are refused at validation (AMBIGUOUS_ASSIGNMENT), and
// again at runtime — never a random pick. The chosen account then passes the US-011 posting
// gate; an unsuitable account is refused, never replaced by another.

// AssignmentRule is one item of the accountAssignmentRules section.
type AssignmentRule struct {
	Code          string            `json:"code"`
	Description   string            `json:"description,omitempty"`
	Class         string            `json:"class"`
	Match         map[string]string `json:"match,omitempty"`
	Account       string            `json:"account"`
	Ledger        string            `json:"ledger,omitempty"`
	Priority      int               `json:"priority,omitempty"`
	EffectiveFrom string            `json:"effectiveFrom,omitempty"`
	EffectiveTo   string            `json:"effectiveTo,omitempty"` // exclusive
	Source        *SourceRef        `json:"source,omitempty"`      // Stage 1: the source row it came from
}

const wildcard = "*"

// pseudoDimensions are matched from the line itself, not from a source field: its legal entity,
// its currency and (ALTB-03) the type of the event that produced it.
var pseudoDimensions = []string{"ENTITY", "CURRENCY", "EVENT_TYPE"}

func (r AssignmentRule) ledger() string {
	if r.Ledger == "" {
		return defaultLedger
	}
	return r.Ledger
}

func (r AssignmentRule) counts() (exact, wild int) {
	for _, v := range r.Match {
		if v == wildcard {
			wild++
		} else {
			exact++
		}
	}
	return
}

// window is the rule's effective window; open ends are the zero time.
func (r AssignmentRule) window() (from, to time.Time) {
	from, _ = ParseDay(r.EffectiveFrom)
	to, _ = ParseDay(r.EffectiveTo)
	return
}

func (r AssignmentRule) activeOn(day time.Time) bool {
	from, to := r.window()
	return (from.IsZero() || !day.Before(from)) && (to.IsZero() || day.Before(to))
}

// matches says whether the rule matches a line's values, and why not.
func (r AssignmentRule) matches(vals map[string]string) (bool, []string, string) {
	var why []string
	for _, k := range sortedKeys(r.Match) {
		want, got := r.Match[k], vals[k]
		switch {
		case want == wildcard && got != "":
			why = append(why, fmt.Sprintf("%s = %s (wildcard)", k, got))
		case want == wildcard:
			return false, nil, fmt.Sprintf("%s has no value (the wildcard needs one)", k)
		case want == got:
			why = append(why, fmt.Sprintf("%s = %s (exact)", k, got))
		default:
			return false, nil, fmt.Sprintf("%s is %q, not %q", k, got, want)
		}
	}
	if len(why) == 0 {
		why = []string{"default for class " + r.Class + " (matches any line)"}
	}
	return true, why, ""
}

// precedes orders two matching rules: negative if a goes first, 0 if they tie.
func precedes(a, b AssignmentRule) int {
	if a.Priority != b.Priority {
		return a.Priority - b.Priority
	}
	ae, aw := a.counts()
	be, bw := b.counts()
	if ae != be {
		return be - ae
	}
	if aw != bw {
		return bw - aw
	}
	af, _ := a.window()
	bf, _ := b.window()
	switch {
	case af.After(bf):
		return -1
	case bf.After(af):
		return 1
	}
	return 0
}

// overlap says whether two rules can both be in force on some day.
func overlap(a, b AssignmentRule) bool {
	af, at := a.window()
	bf, bt := b.window()
	aEndsBeforeB := !at.IsZero() && !bf.IsZero() && !at.After(bf)
	bEndsBeforeA := !bt.IsZero() && !af.IsZero() && !bt.After(af)
	return !aEndsBeforeB && !bEndsBeforeA
}

// compatible says whether some line could match both rules' criteria.
func compatible(a, b AssignmentRule) bool {
	for k, av := range a.Match {
		if bv, ok := b.Match[k]; ok && av != wildcard && bv != wildcard && av != bv {
			return false
		}
	}
	return true
}

func init() { validators = append(validators, validateAssignments) }

func validateAssignments(d *Document) []Finding {
	var fs []Finding
	idx := dims(d)
	byClass := map[string][]int{}
	seen := map[string]bool{}
	for i, r := range d.AssignmentRules {
		p := fmt.Sprintf("accountAssignmentRules/%d", i)
		if !isCode(r.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "assignment rule code %q must be an upper-case code", r.Code))
		}
		if seen[r.Code] {
			fs = append(fs, errf(p, "DUPLICATE_RULE", "assignment rule %s is declared twice", r.Code))
		}
		seen[r.Code] = true
		if _, ok := ClassOf(d, r.Class); !ok {
			fs = append(fs, errf(p, "UNKNOWN_CLASS", "rule %s assigns class %q, which is not an accounting class", r.Code, r.Class))
		}
		byClass[r.Class] = append(byClass[r.Class], i)
		var acct *Account
		for j := range d.Accounts {
			if d.Accounts[j].Code == r.Account && d.Accounts[j].LedgerOf() == r.ledger() {
				acct = &d.Accounts[j]
			}
		}
		switch {
		case acct == nil:
			fs = append(fs, errf(p, "UNKNOWN_ACCOUNT", "rule %s points to account %s, which is not in ledger %s", r.Code, r.Account, r.ledger()))
		case !d.serves(*acct, r.Class):
			fs = append(fs, errf(p, "CLASS_MISMATCH", "rule %s assigns class %s to account %s, which serves class %s", r.Code, r.Class, r.Account, strings.Join(acct.declaredClasses(), ", ")))
		}
		from, fromOK := ParseDay(r.EffectiveFrom)
		to, toOK := ParseDay(r.EffectiveTo)
		if r.EffectiveFrom != "" && !fromOK {
			fs = append(fs, errf(p, "INVALID_DATE", "rule %s effectiveFrom %q must be a date (YYYY-MM-DD)", r.Code, r.EffectiveFrom))
		}
		if r.EffectiveTo != "" && !toOK {
			fs = append(fs, errf(p, "INVALID_DATE", "rule %s effectiveTo %q must be a date (YYYY-MM-DD)", r.Code, r.EffectiveTo))
		}
		if fromOK && toOK && !to.After(from) {
			fs = append(fs, errf(p, "INVALID_WINDOW", "rule %s is effective from %s to %s: the end must be after the start", r.Code, r.EffectiveFrom, r.EffectiveTo))
		}
		if acct != nil && acct.Class == r.Class && fromOK == (r.EffectiveFrom != "") && toOK == (r.EffectiveTo != "") {
			af, _ := ParseDay(acct.ActiveFrom)
			at, _ := ParseDay(acct.ActiveTo)
			never := (!at.IsZero() && !from.IsZero() && !at.After(from)) || (!to.IsZero() && !af.IsZero() && !to.After(af))
			if never {
				fs = append(fs, errf(p, "INACTIVE_ACCOUNT", "rule %s points to account %s, which is never active while the rule is", r.Code, r.Account))
			}
		}
		for _, k := range sortedKeys(r.Match) {
			v := r.Match[k]
			if strings.TrimSpace(v) == "" {
				fs = append(fs, errf(p, "INVALID_MATCH", "rule %s matches %s on an empty value; use \"*\" or a value", r.Code, k))
				continue
			}
			switch {
			case k == "CURRENCY":
				if v != wildcard && !currencyRe(v) {
					fs = append(fs, errf(p, "INVALID_CURRENCY", "rule %s matches CURRENCY %q, which is not an ISO 4217 code", r.Code, v))
				}
			case k == "ENTITY":
				if c, ok := idx["COMPANY"]; ok && v != wildcard && !idx.open("COMPANY") && len(c.Values) > 0 && !oneOf(v, c.Values) {
					fs = append(fs, errf(p, "UNKNOWN_DIMENSION_VALUE", "rule %s matches ENTITY %q, which is not a COMPANY value", r.Code, v))
				}
			case k == "EVENT_TYPE":
				if v != wildcard && !hasEvent(d, v) {
					fs = append(fs, errf(p, "UNKNOWN_EVENT_TYPE", "rule %s matches EVENT_TYPE %q, which is not an event type of this configuration", r.Code, v))
				} else if v != wildcard && !eventPostsClass(d, v, r.Class) {
					fs = append(fs, warnf(p, "ASSIGNMENT_NEVER_APPLIES", "rule %s matches EVENT_TYPE %s, but no posting rule of %s has a %s line, so it never applies", r.Code, v, v, r.Class))
				}
			default:
				x, ok := idx[k]
				if !ok && k != "BOOKING_CLASS" {
					fs = append(fs, errf(p, "UNKNOWN_DIMENSION", "rule %s matches on %s, which is not a configured dimension (or ENTITY, CURRENCY, EVENT_TYPE)", r.Code, k))
				} else if ok && v != wildcard && !idx.open(k) && len(x.Values) > 0 && !oneOf(v, x.Values) {
					fs = append(fs, errf(p, "UNKNOWN_DIMENSION_VALUE", "rule %s matches %s = %q, which is not one of its values (%s)", r.Code, k, v, strings.Join(x.Values, ", ")))
				}
			}
		}
	}
	// Ties: two rules of a class that some line could match, on some day, with equal precedence.
	classes := make([]string, 0, len(byClass))
	for c := range byClass {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	for _, c := range classes {
		is := byClass[c]
		for a := 0; a < len(is); a++ {
			for b := a + 1; b < len(is); b++ {
				ra, rb := d.AssignmentRules[is[a]], d.AssignmentRules[is[b]]
				if ra.ledger() == rb.ledger() && precedes(ra, rb) == 0 && compatible(ra, rb) && overlap(ra, rb) {
					fs = append(fs, errf(fmt.Sprintf("accountAssignmentRules/%d", is[b]), "AMBIGUOUS_ASSIGNMENT",
						"rules %s and %s can match the same %s line with equal precedence; make one more specific or give it a priority", ra.Code, rb.Code, c))
				}
			}
		}
	}
	// Classes posted to without any assignment rule, and rules that read what an event lacks.
	for i, r := range d.PostingRules {
		var ev *EventType
		for j := range d.Events {
			if d.Events[j].Code == r.EventType {
				ev = &d.Events[j]
			}
		}
		if ev == nil {
			continue
		}
		carried := eventDimensions(d, *ev)
		if _, ok := policyFor(d, ev.Code); ok {
			carried = append(carried, "BOOKING_CLASS")
		}
		for j, l := range r.Lines {
			lp := fmt.Sprintf("postingRules/%d/lines/%d", i, j)
			if _, ok := ClassOf(d, l.Class); ok && len(byClass[l.Class]) == 0 {
				fs = append(fs, warnf(lp, "MISSING_ASSIGNMENT", "no assignment rule for class %s: line %s of rule %s is refused (MISSING_ACCOUNT)", l.Class, l.Code, r.Code))
			}
			for _, k := range byClass[l.Class] {
				ar := d.AssignmentRules[k]
				for _, dim := range sortedKeys(ar.Match) {
					if !oneOf(dim, pseudoDimensions) && !oneOf(dim, carried) {
						fs = append(fs, warnf(lp, "ASSIGNMENT_DIMENSION_NOT_CARRIED",
							"assignment rule %s reads %s, which event %s does not carry: line %s of rule %s can never match it", ar.Code, dim, ev.Code, l.Code, r.Code))
					}
				}
			}
		}
	}
	return fs
}

// AssignmentMatch is one rule considered for a line.
type AssignmentMatch struct {
	Rule     string `json:"rule"`
	Account  string `json:"account"`
	Priority int    `json:"priority"`
	Matched  bool   `json:"matched"`
	Reason   string `json:"reason"`
}

// Assignment is how a line got its account: the rule, the configuration version, and why.
type Assignment struct {
	Rule       string            `json:"rule"`
	Account    string            `json:"account"`
	Ledger     string            `json:"ledger"`
	Config     ConfigRef         `json:"config"`
	Why        []string          `json:"why"`
	Considered []AssignmentMatch `json:"considered"`
}

// AssignAccount picks the account for a class from a line's values (its dimensions plus
// ENTITY and CURRENCY) on a book date.
func AssignAccount(d *Document, ref ConfigRef, class string, vals map[string]string, day time.Time) (Assignment, error) {
	a := Assignment{Config: ref, Considered: []AssignmentMatch{}}
	var cands []AssignmentRule
	var whys [][]string
	for _, r := range d.AssignmentRules {
		if r.Class != class {
			continue
		}
		m := AssignmentMatch{Rule: r.Code, Account: r.Account, Priority: r.Priority}
		ok, why, reason := r.matches(vals)
		switch {
		case !r.activeOn(day):
			m.Reason = fmt.Sprintf("not effective on %s", day.Format(time.DateOnly))
		case !ok:
			m.Reason = reason
		default:
			m.Matched, m.Reason = true, strings.Join(why, ", ")
			cands = append(cands, r)
			whys = append(whys, why)
		}
		a.Considered = append(a.Considered, m)
	}
	if len(cands) == 0 {
		return a, &CodeError{"MISSING_ACCOUNT", fmt.Sprintf("no assignment rule for class %s matches this line on %s", class, day.Format(time.DateOnly))}
	}
	best := 0
	for i := 1; i < len(cands); i++ {
		if precedes(cands[i], cands[best]) < 0 {
			best = i
		}
	}
	for i := range cands {
		if i != best && precedes(cands[i], cands[best]) == 0 {
			return a, &CodeError{"AMBIGUOUS_ASSIGNMENT", fmt.Sprintf("rules %s and %s both match with equal precedence; refusing to choose", cands[best].Code, cands[i].Code)}
		}
	}
	r := cands[best]
	a.Rule, a.Account, a.Ledger, a.Why = r.Code, r.Account, r.ledger(), whys[best]
	if len(cands) > 1 {
		a.Why = append(a.Why, fmt.Sprintf("chosen over %d other matching rule(s) by priority, specificity, then latest effective date", len(cands)-1))
	}
	return a, nil
}

// AssignAccounts gives every planned line its account and passes it through the posting gate
// (US-011). Every line is annotated; the first refusal is returned as the error.
func AssignAccounts(d *Document, ref ConfigRef, p *PostingPlan, bookDate time.Time) error {
	var first error
	for i := range p.Lines {
		l := &p.Lines[i]
		vals := map[string]string{"ENTITY": l.Entity, "CURRENCY": l.Currency}
		for k, v := range l.Dimensions {
			vals[k] = v
		}
		vals["EVENT_TYPE"] = p.EventType // ALTB-03: always the event's own type, never a dimension of that name
		a, err := AssignAccount(d, ref, l.Class, vals, bookDate)
		l.Assignment = &a
		if err == nil {
			l.Account, l.Ledger = a.Account, a.Ledger
			_, err = CheckPosting(d, PostingLine{Account: a.Account, Ledger: a.Ledger, Class: l.Class, BookDate: bookDate,
				Entity: l.Entity, Currency: l.Currency, Keys: l.Keys, Dimensions: l.Dimensions})
		}
		if err != nil {
			l.Problem = err.Error()
			if first == nil {
				if ce, ok := err.(*CodeError); ok {
					first = &CodeError{ce.Code, fmt.Sprintf("line %d (%s %s): %s", l.LineNo, l.Class, l.Side, ce.Message)}
				} else {
					first = err
				}
			}
		}
	}
	return first
}

// PreviewPosting plans an event's legs (US-012) and assigns their accounts (US-013) on the
// book date: the date policy's, else the transaction date.
func PreviewPosting(d *Document, ref ConfigRef, in EventInput) (*PostingPlan, error) {
	return PreviewPostingWith(d, ref, in, nil)
}

// PreviewPostingWith is PreviewPosting reading ledger state from led (ALTB-04): a lifecycle
// preview passes its temporary ledger, a posting run will pass the client ledger.
func PreviewPostingWith(d *Document, ref ConfigRef, in EventInput, led LedgerReader) (*PostingPlan, error) {
	p, err := planPosting(d, ref, in, led)
	if err != nil {
		return p, err
	}
	day := in.TransactionDate
	if p.Period != nil {
		day = p.Period.BookDate
	}
	t, ok := ParseDay(day)
	if !ok {
		return p, &CodeError{"MISSING_BOOK_DATE", "a book date is needed to assign accounts: give a transactionDate"}
	}
	p.BookDate = day
	return p, AssignAccounts(d, ref, p, t)
}

// hasEvent says whether the configuration declares an event type.
func hasEvent(d *Document, code string) bool {
	for _, e := range d.Events {
		if e.Code == code {
			return true
		}
	}
	return false
}

// eventPostsClass says whether some posting rule of the event has a line of the class.
func eventPostsClass(d *Document, event, class string) bool {
	for _, r := range d.PostingRules {
		if r.EventType != event {
			continue
		}
		for _, l := range r.Lines {
			if l.Class == class {
				return true
			}
		}
	}
	return false
}
