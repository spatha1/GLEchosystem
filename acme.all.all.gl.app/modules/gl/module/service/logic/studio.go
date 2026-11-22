package logic

import (
	"encoding/json"
	"fmt"
	"strconv"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// The configuration studio's API: read models over one stored version, computed from its
// content on every read (nothing here is stored), so what the studio shows is exactly what
// the engine will do with that version.

func versionParams(c *call) (string, int, *apiError) {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return "", 0, e
	}
	n, e := pathVersion(c.params["version"])
	return configID, n, e
}

// parsed loads a version and parses it, with all its findings.
func parsed(c *call) (VersionView, *model.Document, []model.Finding, model.ConfigRef, *apiError) {
	configID, n, e := versionParams(c)
	if e != nil {
		return VersionView{}, nil, nil, model.ConfigRef{}, e
	}
	v, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return v, nil, nil, model.ConfigRef{}, e
	}
	doc, fs := model.Parse(v.Content)
	fs = append(fs, model.Validate(doc)...)
	return v, doc, fs, model.ConfigRef{ConfigID: configID, Version: n, ContentDigest: v.ContentDigest}, nil
}

func blocking(fs []model.Finding) int {
	n := 0
	for _, f := range fs {
		if f.Severity == "error" {
			n++
		}
	}
	return n
}

// versionEvents — GET …/versions/{n}/events: every event type with what is configured for it.
func versionEvents(c *call) modulesdk.Response {
	v, doc, fs, ref, e := parsed(c)
	if e != nil {
		return e.response()
	}
	if fs == nil {
		fs = []model.Finding{}
	}
	return jsonResponse(200, map[string]any{"configId": v.ConfigID, "version": v.Version, "status": v.Status, "contentDigest": v.ContentDigest,
		"blockingFindings": blocking(fs), "findings": fs, "events": model.SummarizeEvents(doc, fs, c.id.Tenant, ref)})
}

// versionEvent — GET …/versions/{n}/events/{eventType}: one event type explained.
func versionEvent(c *call) modulesdk.Response {
	eventType := c.params["eventType"]
	if e := pathCode("eventType", eventType); e != nil {
		return e.response()
	}
	v, doc, fs, ref, e := parsed(c)
	if e != nil {
		return e.response()
	}
	view, ok := model.ExplainEvent(doc, fs, c.id.Tenant, ref, eventType)
	if !ok {
		return errNotFound("event " + eventType + " is not in this configuration").response()
	}
	return jsonResponse(200, map[string]any{"configId": v.ConfigID, "version": v.Version, "status": v.Status, "contentDigest": v.ContentDigest,
		"blockingFindings": blocking(fs), "view": view})
}

// runScenarios — POST …/versions/{n}/scenarios/run {codes?}: posting previews of the
// version's scenarios, checked against their expectations. Nothing is stored or posted.
func runScenarios(c *call) modulesdk.Response {
	configID, n, e := versionParams(c)
	if e != nil {
		return e.response()
	}
	var body struct {
		Codes []string `json:"codes"`
	}
	if len(c.req.Body) > 0 {
		if e := decodeBody(c.req, &body); e != nil {
			return e.response()
		}
	}
	doc, v, e := loadValidDocument(c, configID, n)
	if e != nil {
		return e.response()
	}
	for _, code := range body.Codes {
		if _, ok := model.FindScenario(doc, code); !ok {
			return errNotFound("scenario " + code + " is not in this version").response()
		}
	}
	results := model.RunScenarios(doc, model.ConfigRef{ConfigID: configID, Version: n, ContentDigest: v.ContentDigest}, body.Codes)
	summary := map[string]int{}
	for _, r := range results {
		summary[r.Status]++
	}
	return jsonResponse(200, map[string]any{"configId": configID, "version": n, "status": v.Status, "summary": summary, "results": results})
}

// versionImpact — GET …/versions/{n}/impact[?against=m]: what changing from version m (by
// default the version this one is based on, else the previous one) to n affects.
func versionImpact(c *call) modulesdk.Response {
	to, toDoc, _, toRef, e := parsed(c)
	if e != nil {
		return e.response()
	}
	against := 0
	switch q := c.query("against"); {
	case q != "":
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 {
			return errInvalid("against", "against must be a version number").response()
		}
		against = n
	case to.BasedOnVersion != nil:
		against = *to.BasedOnVersion
	default:
		against = to.Version - 1
	}
	if against < 1 || against == to.Version {
		return errInvalid("against", "there is no earlier version to compare with; give against").response()
	}
	from, e := loadVersion(c.host, c.id, to.ConfigID, against, true)
	if e != nil {
		return e.response()
	}
	fromDoc, _ := model.Parse(from.Content)
	fromRef := model.ConfigRef{ConfigID: from.ConfigID, Version: from.Version, ContentDigest: from.ContentDigest}
	return jsonResponse(200, model.ComputeImpact(fromDoc, toDoc, from.Content, to.Content, c.id.Tenant, fromRef, toRef))
}

// checkDraft — POST …/versions/{n}/check {content}: validate edited content without saving
// it, and say which items it changes against the stored version.
func checkDraft(c *call) modulesdk.Response {
	configID, n, e := versionParams(c)
	if e != nil {
		return e.response()
	}
	var body struct {
		Content json.RawMessage `json:"content"`
	}
	if e := decodeBody(c.req, &body); e != nil {
		return e.response()
	}
	cur, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return e.response()
	}
	canonical, digest, shape, e := contentOrError(body.Content)
	if e != nil {
		return e.response()
	}
	rep := ValidateContent(canonical, shape)
	return jsonResponse(200, map[string]any{"ok": rep.OK, "findings": rep.Findings, "contentDigest": digest,
		"changes": model.DiffContent(cur.Content, canonical)})
}

// versionPipeline — GET …/versions/{n}/pipeline: where the version stands in the GL flow,
// stage by stage, from configuration to reports. Stages that are not built say so.
func versionPipeline(c *call) modulesdk.Response {
	v, doc, fs, ref, e := parsed(c)
	if e != nil {
		return e.response()
	}
	nb := blocking(fs)
	rules := 0
	for _, r := range doc.PostingRules {
		if r.IsActive() {
			rules++
		}
	}
	stages := []model.Stage{{Stage: "configured", Label: "Configured", Status: "DONE",
		Detail: fmt.Sprintf("%d events, %d active posting rules; version %d by %s", len(doc.Events), rules, v.Version, v.CreatedBy)}}

	val := model.Stage{Stage: "validated", Label: "Validated"}
	switch {
	case v.Status != "draft":
		val.Status, val.Detail = "DONE", "validated by "+deref(v.ValidatedBy)
	case nb > 0:
		val.Status, val.Detail = "FAILED", fmt.Sprintf("%d blocking finding(s) to fix", nb)
	default:
		val.Status, val.Detail = "PENDING", "no blocking findings: ready to validate"
	}
	stages = append(stages, val)

	app := model.Stage{Stage: "approved", Label: "Approved"}
	switch v.Status {
	case "approved":
		app.Status, app.Detail = "DONE", "approved by "+deref(v.ApprovedBy)+"; immutable"
	case "retired":
		app.Status, app.Detail = "DONE", "approved by "+deref(v.ApprovedBy)+", since retired"
	default:
		app.Status, app.Detail = "PENDING", "awaiting an approver who did not edit it (maker-checker)"
	}
	stages = append(stages, app)

	m, _ := model.CompileManifest(doc, c.id.Tenant, ref)
	src, der := 0, 0
	for _, ev := range m.Events {
		for _, f := range ev.Fields {
			if f.Kind == "SOURCE" {
				src++
			} else {
				der++
			}
		}
	}
	fields := model.Stage{Stage: "fields", Label: "Required fields", Story: "US-014", Status: "DONE",
		Detail: fmt.Sprintf("%d source fields and %d derived values across %d events", src, der, len(m.Events))}
	if nb > 0 {
		fields.Status, fields.Detail = "FAILED", "incomplete while the version has blocking findings"
	}
	stages = append(stages, fields,
		model.Stage{Stage: "mapped", Label: "Data Workbench mapping", Status: "NOT_IMPLEMENTED", Story: "US-015, US-016",
			Detail: fmt.Sprintf("the Data Workbench integration is not built: %d source fields have no mapping yet", src)},
		model.Stage{Stage: "collected", Label: "Data collection", Status: "NOT_IMPLEMENTED", Story: "US-017",
			Detail: "no data is collected yet: simulation runs on the version's scenarios"})

	sim := model.Stage{Stage: "simulated", Label: "Simulated", Story: "US-012, US-013"}
	switch {
	case nb > 0:
		sim.Status, sim.Detail = "FAILED", "cannot simulate while the version has blocking findings"
	case len(doc.Scenarios) == 0:
		sim.Status, sim.Detail = "PENDING", "no scenarios in this version"
	default:
		pass, bad, life := 0, 0, 0
		for _, r := range model.RunScenarios(doc, ref, nil) {
			switch r.Status {
			case "PASSED", "RAN":
				pass++
			case "LIFECYCLE":
				life++
			default:
				bad++
			}
		}
		sim.Status, sim.Detail = "DONE", fmt.Sprintf("%d of %d scenarios hold", pass, pass+bad)
		if life > 0 {
			sim.Detail += fmt.Sprintf("; %d read earlier postings and are checked in lifecycle profiles", life)
		}
		if bad > 0 {
			sim.Status = "FAILED"
		}
	}
	stages = append(stages, sim)
	stages = append(stages, model.NotImplementedStages()...)
	return jsonResponse(200, map[string]any{"configId": v.ConfigID, "version": v.Version, "status": v.Status, "stages": stages})
}
