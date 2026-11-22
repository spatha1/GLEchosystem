package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gl/formula"
)

// Simulation scenarios (groundwork for US-030).
//
// A scenario is a named synthetic event and, optionally, the journal it is expected to
// produce. Scenarios live in the configuration (section `scenarios`), so a rule change and
// the evidence that it still posts correctly are versioned, reviewed and approved together,
// and the studio never hard-codes sample events or expected results. Running a scenario is
// a posting preview: nothing is stored or posted.

// Scenario is one item of the scenarios section.
type Scenario struct {
	Code        string          `json:"code"`
	Description string          `json:"description,omitempty"`
	EventType   string          `json:"eventType"`
	Input       EventInput      `json:"input"`
	Expect      *ScenarioExpect `json:"expect,omitempty"`
}

// ScenarioExpect is what a scenario must produce: a journal (the rule and legs), or a
// refusal with a given code.
type ScenarioExpect struct {
	Rule    string        `json:"rule,omitempty"`
	Legs    []ExpectedLeg `json:"legs,omitempty"`
	Refused string        `json:"refused,omitempty"`
}

// ExpectedLeg is one expected journal leg. Entity and account are checked when given.
type ExpectedLeg struct {
	Class   string `json:"class"`
	Side    string `json:"side"`
	Amount  string `json:"amount"`
	Entity  string `json:"entity,omitempty"`
	Account string `json:"account,omitempty"`
}

func (l ExpectedLeg) String() string {
	s := fmt.Sprintf("%s %s %s", l.Class, l.Side, l.Amount)
	if l.Entity != "" {
		s += " in " + l.Entity
	}
	if l.Account != "" {
		s += " to " + l.Account
	}
	return s
}

func init() { validators = append(validators, validateScenarios) }

func validateScenarios(d *Document) []Finding {
	var fs []Finding
	events := map[string]bool{}
	for _, e := range d.Events {
		events[e.Code] = true
	}
	rules := map[string]string{}
	for _, r := range d.PostingRules {
		rules[r.Code] = r.EventType
	}
	accounts := map[string]bool{}
	for _, a := range d.Accounts {
		accounts[a.Code] = true
	}
	seen := map[string]bool{}
	for i, s := range d.Scenarios {
		p := fmt.Sprintf("scenarios/%d", i)
		if !isCode(s.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "scenario code %q must be an upper-case code", s.Code))
		}
		if seen[s.Code] {
			fs = append(fs, errf(p, "DUPLICATE_SCENARIO", "scenario %s is declared twice", s.Code))
		}
		seen[s.Code] = true
		if !events[s.EventType] {
			fs = append(fs, errf(p, "UNKNOWN_EVENT", "scenario %s is for event %q, which is not in the catalog", s.Code, s.EventType))
		}
		if s.Input.EventType != "" && s.Input.EventType != s.EventType {
			fs = append(fs, errf(p, "SCENARIO_EVENT_MISMATCH", "scenario %s is for %s but its input is a %s", s.Code, s.EventType, s.Input.EventType))
		}
		if s.Expect == nil {
			continue
		}
		if s.Expect.Refused != "" && len(s.Expect.Legs) > 0 {
			fs = append(fs, errf(p, "INVALID_EXPECTATION", "scenario %s expects both a refusal and legs", s.Code))
		}
		if s.Expect.Rule != "" && rules[s.Expect.Rule] != s.EventType {
			fs = append(fs, errf(p, "UNKNOWN_RULE", "scenario %s expects rule %s, which is not a posting rule for %s", s.Code, s.Expect.Rule, s.EventType))
		}
		for j, l := range s.Expect.Legs {
			lp := fmt.Sprintf("%s/expect/legs/%d", p, j)
			if _, ok := ClassOf(d, l.Class); !ok {
				fs = append(fs, errf(lp, "UNKNOWN_CLASS", "expected leg class %q is not an accounting class", l.Class))
			}
			if l.Side != "D" && l.Side != "C" {
				fs = append(fs, errf(lp, "INVALID_SIDE", "expected leg side %q must be D or C", l.Side))
			}
			if _, err := formula.ParseDecimal(l.Amount); err != nil {
				fs = append(fs, errf(lp, "INVALID_AMOUNT", "expected leg amount %q must be a plain decimal", l.Amount))
			}
			if l.Account != "" && !accounts[l.Account] {
				fs = append(fs, errf(lp, "UNKNOWN_ACCOUNT", "expected leg account %s is not in the chart of accounts", l.Account))
			}
		}
	}
	return fs
}

// ErrorCode is the stable code of an engine error ("" when it has none).
func ErrorCode(err error) string {
	var ce *CodeError
	var pe *PeriodError
	var ae *AuthorityError
	var fe *formula.Error
	switch {
	case errors.As(err, &ce):
		return ce.Code
	case errors.As(err, &pe):
		return pe.Code
	case errors.As(err, &ae):
		return ae.Code
	case errors.As(err, &fe):
		return fe.Code
	}
	return ""
}

// ScenarioResult is one scenario run. Status is PASSED or FAILED against the expectation,
// RAN when there is none, REFUSED when the engine refused an event that had no expectation,
// and LIFECYCLE when the scenario reads earlier postings (OPEN_BALANCE), so it can only be
// checked as a step of a lifecycle, after the scenarios it depends on.
type ScenarioResult struct {
	Code        string         `json:"code"`
	Description string         `json:"description,omitempty"`
	EventType   string         `json:"eventType"`
	Status      string         `json:"status"`
	Plan        *PostingPlan   `json:"plan,omitempty"`
	Error       *ScenarioError `json:"error,omitempty"`
	Mismatches  []string       `json:"mismatches"`
}

// ScenarioError is the refusal of a scenario's event.
type ScenarioError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FindScenario returns a scenario by code.
func FindScenario(d *Document, code string) (Scenario, bool) {
	for _, s := range d.Scenarios {
		if s.Code == code {
			return s, true
		}
	}
	return Scenario{}, false
}

// RunScenario previews a scenario's event on its own and checks its expectation.
func RunScenario(d *Document, ref ConfigRef, s Scenario) ScenarioResult {
	in := s.Input
	in.EventType = s.EventType
	plan, err := PreviewPosting(d, ref, in)
	if ErrorCode(err) == "LEDGER_STATE_UNAVAILABLE" {
		return ScenarioResult{Code: s.Code, Description: s.Description, EventType: s.EventType, Status: "LIFECYCLE", Mismatches: []string{},
			Error: &ScenarioError{Code: "LEDGER_STATE_UNAVAILABLE", Message: "this scenario reads earlier postings: check it in a lifecycle profile, after the scenarios it depends on"}}
	}
	return CheckScenario(s, plan, err)
}

// CheckScenario checks a plan (or a refusal) against a scenario's expectation: on its own, or
// as one step of a lifecycle preview or posting run (ALTB-08).
func CheckScenario(s Scenario, plan *PostingPlan, err error) ScenarioResult {
	res := ScenarioResult{Code: s.Code, Description: s.Description, EventType: s.EventType, Mismatches: []string{}}
	res.Plan = plan
	exp := s.Expect
	if err != nil {
		code := ErrorCode(err)
		if code == "" {
			code = "PLAN_FAILED"
		}
		res.Error = &ScenarioError{Code: code, Message: strings.TrimPrefix(err.Error(), code+": ")}
		switch {
		case exp == nil:
			res.Status = "REFUSED"
		case exp.Refused == code:
			res.Status = "PASSED"
		case exp.Refused != "":
			res.Status = "FAILED"
			res.Mismatches = append(res.Mismatches, fmt.Sprintf("expected refusal %s, got %s: %s", exp.Refused, code, res.Error.Message))
		default:
			res.Status = "FAILED"
			res.Mismatches = append(res.Mismatches, fmt.Sprintf("expected a journal, but the event was refused: %s %s", code, res.Error.Message))
		}
		return res
	}
	if exp == nil {
		res.Status = "RAN"
		return res
	}
	if exp.Refused != "" {
		res.Status = "FAILED"
		res.Mismatches = append(res.Mismatches, "expected refusal "+exp.Refused+", but the event posts")
		return res
	}
	if exp.Rule != "" && exp.Rule != plan.Rule {
		res.Mismatches = append(res.Mismatches, fmt.Sprintf("expected rule %s, got %s", exp.Rule, plan.Rule))
	}
	res.Mismatches = append(res.Mismatches, compareLegs(exp.Legs, plan.Lines)...)
	res.Status = "PASSED"
	if len(res.Mismatches) > 0 {
		res.Status = "FAILED"
	}
	return res
}

// compareLegs matches expected legs to planned lines one to one, in any order.
func compareLegs(want []ExpectedLeg, got []PlannedLine) []string {
	var out []string
	used := make([]bool, len(got))
	for _, w := range want {
		wa, _ := formula.ParseDecimal(w.Amount)
		found := false
		for i, g := range got {
			if used[i] || g.Class != w.Class || g.Side != w.Side || (w.Entity != "" && g.Entity != w.Entity) || (w.Account != "" && g.Account != w.Account) {
				continue
			}
			if ga, err := formula.ParseDecimal(g.Amount); err != nil || wa == nil || ga.Cmp(wa) != 0 {
				continue
			}
			used[i], found = true, true
			break
		}
		if !found {
			out = append(out, "missing leg "+w.String())
		}
	}
	for i, g := range got {
		if !used[i] {
			out = append(out, fmt.Sprintf("unexpected leg %s %s %s in %s to %s", g.Class, g.Side, g.Amount, g.Entity, g.Account))
		}
	}
	return out
}

// RunScenarios runs the given scenarios (all when codes is empty), in configuration order.
func RunScenarios(d *Document, ref ConfigRef, codes []string) []ScenarioResult {
	out := []ScenarioResult{}
	for _, s := range d.Scenarios {
		if len(codes) > 0 && !oneOf(s.Code, codes) {
			continue
		}
		out = append(out, RunScenario(d, ref, s))
	}
	return out
}

// LegSummary is a plan's legs as sorted text, for comparing two runs.
func LegSummary(p *PostingPlan) []string {
	if p == nil {
		return []string{}
	}
	out := []string{}
	for _, l := range p.Lines {
		out = append(out, fmt.Sprintf("%s %s %s %s %s", l.Entity, l.Account, l.Class, l.Side, l.Amount))
	}
	sort.Strings(out)
	return out
}
