package ai

import (
	"context"
	"encoding/json"

	"gl/model"
)

// StepScore is how one step went in an evaluation run.
type StepScore struct {
	Step          string   `json:"step"`
	Attempts      int      `json:"attempts"`
	Items         int      `json:"items"`
	Questions     int      `json:"questions"`
	Gaps          int      `json:"gaps"`
	Problems      []string `json:"problems"`
	Quotes        int      `json:"quotes"`
	QuotesFound   int      `json:"quotesFound"`
	Cases         int      `json:"cases"`
	CasesBalanced int      `json:"casesBalanced"`
	Tokens        int      `json:"tokens"`
	Error         string   `json:"error,omitempty"`
}

// Report is one evaluation run: the four steps on one BRD.
type Report struct {
	BRD      string      `json:"brd"`
	Model    string      `json:"model"`
	Steps    []StepScore `json:"steps"`
	Events   []string    `json:"events"` // the events configured at the end
	OK       bool        `json:"ok"`     // every step without problems, every test case balanced
	Findings int         `json:"blockingFindings"`
}

// Evaluate runs the four steps on a BRD, accepting every item as a person approving all of
// it would. It measures the AI; it is never how the studio saves anything (there a person
// chooses). Content is the reference configuration to start from.
func Evaluate(ctx context.Context, d Drafter, brd BRD, content []byte, effectiveFrom string, check Checker) Report {
	rep := Report{BRD: brd.Name, Model: d.Status().Model, Steps: []StepScore{}, Events: []string{}, OK: true}
	for _, step := range Steps {
		sc := StepScore{Step: step, Problems: []string{}}
		p, err := RunStep(ctx, d, Request{Step: step, BRD: brd, Content: content, EffectiveFrom: effectiveFrom, Ref: model.ConfigRef{ConfigID: "AI_EVAL", Version: 1}}, check)
		if err != nil {
			sc.Error = err.Error()
			rep.Steps = append(rep.Steps, sc)
			rep.OK = false
			break
		}
		sc.Attempts, sc.Items, sc.Questions, sc.Gaps, sc.Problems = p.Attempts, len(p.Items), len(p.Questions), len(p.Gaps), p.Problems
		sc.Tokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
		for _, it := range p.Items {
			for _, e := range it.Evidence {
				sc.Quotes++
				if e.Found {
					sc.QuotesFound++
				}
			}
			for _, pt := range it.Parts {
				for _, e := range pt.Evidence {
					sc.Quotes++
					if e.Found {
						sc.QuotesFound++
					}
				}
			}
		}
		for _, c := range p.Cases {
			sc.Cases++
			if c.Balanced {
				sc.CasesBalanced++
			}
		}
		if len(p.Problems) > 0 || sc.CasesBalanced < sc.Cases {
			rep.OK = false
		}
		rep.Steps = append(rep.Steps, sc)
		next, err := Apply(content, p.Items)
		if err != nil {
			rep.OK = false
			break
		}
		content = next
	}
	fs, canonical, err := check(content)
	if err == nil {
		for _, f := range fs {
			if f.Severity == "error" && !personCodes[f.Code] {
				rep.Findings++
			}
		}
		doc, _ := model.Parse(canonical)
		for _, e := range doc.Events {
			rep.Events = append(rep.Events, e.Code)
		}
	}
	if rep.Findings > 0 {
		rep.OK = false
	}
	return rep
}

// ReferenceOnly keeps a configuration's reference data (sources, measures, dimensions,
// lookups, keys, calendars, date policies, chart of accounts) and drops its events and
// everything built on them, so an evaluation starts as a new client's configuration would.
func ReferenceOnly(content []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(content, &m); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for _, k := range []string{"sourceSystems", "baseMeasures", "dimensions", "lookups", "keyProfiles", "calendars", "datePolicies", "accountingClasses", "accounts", "accountAssignmentRules"} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return json.Marshal(out)
}
