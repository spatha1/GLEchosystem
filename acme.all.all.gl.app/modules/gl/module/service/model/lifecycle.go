package model

// ALTB-04 — lifecycle preview. A synthetic policy's events run in order through the same
// engine a posting run uses (PreviewPostingWith), against a temporary ledger, so a later event
// can read what earlier ones posted. Nothing is stored: the ledger is thrown away with the
// result, and no journal gets an identity.

import (
	"fmt"
	"strings"
)

// LifecycleEvent is one event of a lifecycle, with the scenario it came from, if any.
type LifecycleEvent struct {
	Code  string     `json:"code,omitempty"`
	Input EventInput `json:"input"`
}

// LifecycleStep is one event's preview and the temporary ledger after it.
type LifecycleStep struct {
	Step     int             `json:"step"`
	Code     string          `json:"code,omitempty"`
	Plan     *PostingPlan    `json:"plan,omitempty"`
	Error    *ScenarioError  `json:"error,omitempty"`
	Balances []LedgerBalance `json:"balances"`
}

// LifecycleResult is a whole lifecycle preview.
type LifecycleResult struct {
	Config   ConfigRef       `json:"config"`
	Steps    []LifecycleStep `json:"steps"`
	Complete bool            `json:"complete"`          // every event planned, balanced and assigned
	Stopped  string          `json:"stopped,omitempty"` // why the preview stopped early
	Lines    int             `json:"lines"`
	Entries  int             `json:"entries"` // one per event, legal entity and currency
	Posted   bool            `json:"posted"`  // always false: a preview never posts
}

// PreviewLifecycle runs events in order against a temporary ledger. It stops at the first event
// that cannot be posted (refused, unbalanced, or a line without a valid account), because every
// later event may depend on it.
func PreviewLifecycle(d *Document, ref ConfigRef, events []LifecycleEvent) LifecycleResult {
	return RunLifecycle(d, ref, events, NewTempLedger(d))
}

// RunLifecycle is PreviewLifecycle on a given ledger: an empty one for a preview, or one loaded
// with a book's posted lines for a posting run (ALTB-05). Both go through this one loop.
func RunLifecycle(d *Document, ref ConfigRef, events []LifecycleEvent, led *TempLedger) LifecycleResult {
	res := LifecycleResult{Config: ref, Steps: []LifecycleStep{}}
	for i, e := range events {
		st := LifecycleStep{Step: i + 1, Code: e.Code}
		p, err := PreviewPostingWith(d, ref, e.Input, led)
		st.Plan = p
		switch {
		case err != nil:
			// Includes a plan with no lines (every amount zero): the engine refuses it, and a
			// posting run would too, so the preview does not pass over it.
			st.Error = scenarioError(err)
		case !p.Balanced:
			st.Error = &ScenarioError{Code: "UNBALANCED", Message: "the plan does not balance for every legal entity and currency"}
		}
		if st.Error == nil {
			if perr := led.Post(i+1, p); perr != nil {
				st.Error = scenarioError(perr)
			}
		}
		st.Balances = led.Balances()
		res.Steps = append(res.Steps, st)
		if st.Error != nil {
			res.Stopped = fmt.Sprintf("step %d (%s) cannot be posted: %s %s", i+1, firstOf(e.Code, e.Input.EventType), st.Error.Code, st.Error.Message)
			return res
		}
		res.Lines += len(p.Lines)
		res.Entries += len(p.Totals)
	}
	res.Complete = len(events) > 0
	return res
}

func scenarioError(err error) *ScenarioError {
	code := ErrorCode(err)
	if code == "" {
		code = "PLAN_FAILED"
	}
	return &ScenarioError{Code: code, Message: strings.TrimPrefix(err.Error(), code+": ")}
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
