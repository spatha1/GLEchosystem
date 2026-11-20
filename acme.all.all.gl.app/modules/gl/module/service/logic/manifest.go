package logic

import (
	"strconv"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// US-014 — the required-field manifest (contract C2), compiled from a configuration version.
//
// The manifest is recompiled from the version's content on every read: it is a pure function
// of the content, so an approved version always yields the same manifest and digest, and a
// new approved version yields its own. Collection (US-015..017) asks for the manifest of the
// version effective on a date, so approving a change regenerates what is requested.

// manifestView is the manifest of one version with its explanation. Complete is false while
// the version has blocking findings: what cannot be resolved is left out, so the manifest
// may then be missing fields.
type manifestView struct {
	ConfigID         string                    `json:"configId"`
	Version          int                       `json:"version"`
	Status           string                    `json:"status"`
	Complete         bool                      `json:"complete"`
	BlockingFindings int                       `json:"blockingFindings"`
	Manifest         model.Manifest            `json:"manifest"`
	Explanation      model.ManifestExplanation `json:"explanation"`
}

func compileView(c *call, v VersionView, eventType string) (manifestView, *apiError) {
	doc, fs := model.Parse(v.Content)
	fs = append(fs, model.Validate(doc)...)
	blocking := 0
	for _, f := range fs {
		if f.Severity == "error" {
			blocking++
		}
	}
	m, x := model.CompileManifest(doc, c.id.Tenant, model.ConfigRef{ConfigID: v.ConfigID, Version: v.Version, ContentDigest: v.ContentDigest})
	if eventType != "" {
		if e := pathCode("eventType", eventType); e != nil {
			return manifestView{}, e
		}
		found := false
		for i := range m.Events {
			if m.Events[i].EventType == eventType {
				m.Events, x.Events = m.Events[i:i+1], x.Events[i:i+1]
				found = true
				break
			}
		}
		if !found {
			return manifestView{}, errNotFound("event " + eventType + " is not in this configuration")
		}
	}
	return manifestView{ConfigID: v.ConfigID, Version: v.Version, Status: v.Status, Complete: blocking == 0, BlockingFindings: blocking,
		Manifest: m, Explanation: x}, nil
}

// versionManifest — GET /configs/{configId}/versions/{version}/manifest[?eventType=]
func versionManifest(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	v, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return e.response()
	}
	out, e := compileView(c, v, c.query("eventType"))
	if e != nil {
		return e.response()
	}
	return jsonResponse(200, out)
}

// effectiveManifest — GET /configs/{configId}/effective/manifest?date=YYYY-MM-DD: the
// manifest of the approved version in force on the date, which is what collection uses.
func effectiveManifest(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	v, e := loadEffective(c, configID, c.query("date"))
	if e != nil {
		return e.response()
	}
	out, e := compileView(c, v, c.query("eventType"))
	if e != nil {
		return e.response()
	}
	return jsonResponse(200, out)
}

// manifestDiff — GET /configs/{configId}/versions/{version}/manifest/diff?against=N: which
// fields version N's manifest adds, drops or changes compared with version `against`, and
// which of those changes need a source mapping added, reviewed or dropped.
func manifestDiff(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	against, err := strconv.Atoi(c.query("against"))
	if err != nil || against < 1 {
		return errInvalid("against", "against must be the version number to compare with").response()
	}
	to, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return e.response()
	}
	from, e := loadVersion(c.host, c.id, configID, against, true)
	if e != nil {
		return e.response()
	}
	a, e := compileView(c, from, "")
	if e != nil {
		return e.response()
	}
	b, e := compileView(c, to, "")
	if e != nil {
		return e.response()
	}
	return jsonResponse(200, model.DiffManifests(a.Manifest, b.Manifest))
}
