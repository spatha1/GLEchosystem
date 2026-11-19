package logic

import (
	"errors"

	"gl/formula"
	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// evaluateVersion is the simulator (US-007, US-008): resolve a configuration version's
// dimensions (lookups included) and evaluate its derived measures for one sample event and return each value with its lineage —
// formula, formula digest, the configuration version and every input read. It is read-only:
// nothing is stored and nothing is posted, so a BA can try a draft before validating it.
func evaluateVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	var in model.EventInput
	if e := decodeBody(c.req, &in); e != nil {
		return e.response()
	}
	v, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return e.response()
	}
	doc, fs := model.Parse(v.Content)
	fs = append(fs, model.Validate(doc)...)
	for _, f := range fs {
		if f.Severity == "error" {
			return (&apiError{Status: 422, Code: "VALIDATION_FAILED", Message: "this version has blocking findings; validate it to see them", Detail: f}).response()
		}
	}
	out, err := model.EvaluateEvent(doc, model.ConfigRef{ConfigID: configID, Version: n, ContentDigest: v.ContentDigest}, in)
	if err != nil {
		code := "EVALUATION_FAILED"
		var fe *formula.Error
		var ce *model.CodeError
		switch {
		case errors.As(err, &fe):
			code = fe.Code
		case errors.As(err, &ce):
			code = ce.Code
		}
		return (&apiError{Status: 422, Code: code, Message: err.Error()}).response()
	}
	if out.Derived == nil {
		out.Derived = []model.Derived{}
	}
	return jsonResponse(200, map[string]any{"configId": configID, "version": n, "status": v.Status, "eventType": in.EventType,
		"dimensions": out.Dimensions, "derived": out.Derived})
}
