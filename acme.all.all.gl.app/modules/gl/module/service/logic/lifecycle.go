package logic

import (
	"fmt"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// maxLifecycleSteps bounds one lifecycle preview: a synthetic policy's events, not a book.
const maxLifecycleSteps = 100

// previewLifecycle (ALTB-04) — POST …/versions/{n}/lifecycle {steps}: a synthetic policy's
// events run in order through the posting engine against a temporary ledger, so a later event
// can read what earlier ones posted (OPEN_BALANCE). Each step names one of the version's
// scenarios or gives a synthetic event. Nothing is stored or posted: the ledger is thrown
// away with the response, and no journal gets an identity.
func previewLifecycle(c *call) modulesdk.Response {
	configID, n, e := versionParams(c)
	if e != nil {
		return e.response()
	}
	var body struct {
		Steps []struct {
			Scenario string            `json:"scenario,omitempty"`
			Code     string            `json:"code,omitempty"`
			Input    *model.EventInput `json:"input,omitempty"`
		} `json:"steps"`
	}
	if e := decodeBody(c.req, &body); e != nil {
		return e.response()
	}
	if len(body.Steps) == 0 {
		return errInvalid("steps", "give at least one step: a scenario code or a synthetic event").response()
	}
	if len(body.Steps) > maxLifecycleSteps {
		return errInvalid("steps", fmt.Sprintf("a lifecycle preview takes at most %d steps", maxLifecycleSteps)).response()
	}
	for i, s := range body.Steps {
		if (s.Scenario == "") == (s.Input == nil) {
			return errInvalid(fmt.Sprintf("steps/%d", i), "each step names a scenario or gives an input, not both").response()
		}
	}
	doc, v, e := loadValidDocument(c, configID, n)
	if e != nil {
		return e.response()
	}
	events := make([]model.LifecycleEvent, 0, len(body.Steps))
	for i, s := range body.Steps {
		if s.Scenario == "" {
			events = append(events, model.LifecycleEvent{Code: s.Code, Input: *s.Input})
			continue
		}
		sc, ok := model.FindScenario(doc, s.Scenario)
		if !ok {
			return (&apiError{Status: 404, Code: "NOT_FOUND", Message: "scenario " + s.Scenario + " is not in this version", Field: fmt.Sprintf("steps/%d/scenario", i)}).response()
		}
		in := sc.Input
		in.EventType = sc.EventType
		events = append(events, model.LifecycleEvent{Code: firstNonEmpty(s.Code, sc.Code), Input: in})
	}
	res := model.PreviewLifecycle(doc, model.ConfigRef{ConfigID: configID, Version: n, ContentDigest: v.ContentDigest}, events)
	return jsonResponse(200, map[string]any{"configId": configID, "version": n, "status": v.Status, "lifecycle": res})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
