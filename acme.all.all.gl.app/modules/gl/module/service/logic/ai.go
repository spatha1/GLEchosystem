package logic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gl/ai"
	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// AIDrafter returns the AI provider to draft with, read afresh on each request so a changed
// settings file applies without a restart. main and the dev adapter set it; when it is nil the
// studio reports that AI is not set up, and every other screen works without it.
var AIDrafter func() ai.Drafter

func drafter() ai.Drafter {
	if AIDrafter == nil {
		return ai.Load("")
	}
	return AIDrafter()
}

// aiStatus — GET /ai/status: whether AI drafting is available, with which provider and model.
func aiStatus(c *call) modulesdk.Response {
	return jsonResponse(200, drafter().Status())
}

// draftForAI loads a version that AI steps may work on: a draft.
func draftForAI(c *call) (VersionView, *apiError) {
	configID, n, e := versionParams(c)
	if e != nil {
		return VersionView{}, e
	}
	v, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return v, e
	}
	if v.Status != "draft" {
		return v, errConflict("CONFIG_IMMUTABLE", fmt.Sprintf("version %d is %s: AI steps work on a draft; create a new version", n, v.Status), false)
	}
	return v, nil
}

// aiReadBRD — POST …/versions/{n}/ai/brd {name, data (base64)}: the BRD's text, read from a
// Word, text or Markdown file. Nothing is sent to the AI and nothing is stored.
func aiReadBRD(c *call) modulesdk.Response {
	if _, e := draftForAI(c); e != nil {
		return e.response()
	}
	var b struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if strings.TrimSpace(b.Name) == "" || len(b.Name) > 200 {
		return errInvalid("name", "name is the BRD's file name (at most 200 characters)").response()
	}
	data, err := base64.StdEncoding.DecodeString(b.Data)
	if err != nil {
		return errInvalid("data", "data must be the file's content in base64").response()
	}
	text, err := ai.ExtractText(b.Name, data)
	if err != nil {
		return errInvalid("data", "the BRD could not be read: "+err.Error()).response()
	}
	return jsonResponse(200, map[string]any{"name": b.Name, "chars": len([]rune(text)), "text": text})
}

// aiStep — POST …/versions/{n}/ai/steps/{step} {brd: {name, text}, notes}: the AI's proposal for
// one step (events, measures, formulas, rules), drafted from the BRD against the draft as it is,
// checked by the engine and repaired by the AI within a bound. Nothing is saved: the person
// approves the proposal and the studio saves it to the draft with a reason (PUT, origin AI_STEP).
func aiStep(c *call) modulesdk.Response {
	step := c.params["step"]
	if !ai.ValidStep(step) {
		return errInvalid("step", "step is one of "+strings.Join(ai.Steps, ", ")).response()
	}
	v, e := draftForAI(c)
	if e != nil {
		return e.response()
	}
	var b struct {
		BRD   ai.BRD `json:"brd"`
		Notes string `json:"notes"`
	}
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if strings.TrimSpace(b.BRD.Text) == "" {
		return errInvalid("brd", "brd.text is required: upload the BRD first").response()
	}
	if n := len([]rune(b.BRD.Text)); n > ai.MaxBRDChars {
		return errInvalid("brd", fmt.Sprintf("the BRD has %d characters; the limit is %d", n, ai.MaxBRDChars)).response()
	}
	if len(b.Notes) > 4000 {
		return errInvalid("notes", "notes are limited to 4000 characters").response()
	}
	check := func(content []byte) ([]model.Finding, []byte, error) {
		canonical, _, shape, e := contentOrError(json.RawMessage(content))
		if e != nil {
			return nil, nil, errors.New(e.Message)
		}
		return ValidateContent(canonical, shape).Findings, canonical, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(ai.MaxAttempts)*ai.Timeout)
	defer cancel()
	p, err := ai.RunStep(ctx, drafter(), ai.Request{Step: step, BRD: b.BRD, Notes: b.Notes, Content: v.Content, EffectiveFrom: v.EffectiveFrom,
		Ref: model.ConfigRef{ConfigID: v.ConfigID, Version: v.Version, ContentDigest: v.ContentDigest}}, check)
	if err != nil {
		var pe ai.ErrProvider
		if errors.As(err, &pe) {
			return (&apiError{Status: 503, Code: "AI_UNAVAILABLE", Message: pe.Error(), Retryable: true}).response()
		}
		return errInternal("the AI step failed: " + err.Error()).response()
	}
	return jsonResponse(200, map[string]any{"configId": v.ConfigID, "version": v.Version, "contentDigest": v.ContentDigest, "proposal": p})
}

// Origin says where a saved change came from, for the audit trail.
type Origin struct {
	Kind     string `json:"kind"` // AI_STEP
	Step     string `json:"step"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BRD      string `json:"brd,omitempty"`
}

func (o *Origin) check() *apiError {
	if o == nil {
		return nil
	}
	if o.Kind != "AI_STEP" || !ai.ValidStep(o.Step) {
		return errInvalid("origin", "origin is {kind: AI_STEP, step: one of "+strings.Join(ai.Steps, ", ")+", provider, model, brd}")
	}
	if len(o.Provider) > 40 || len(o.Model) > 100 || len(o.BRD) > 200 {
		return errInvalid("origin", "origin's provider, model and brd are too long")
	}
	return nil
}

// updateDetail is the audit detail of a draft update.
func updateDetail(previous string, changes any, o *Origin) map[string]any {
	d := map[string]any{"previousDigest": previous, "changes": changes}
	if o != nil {
		d["origin"] = o
	}
	return d
}
