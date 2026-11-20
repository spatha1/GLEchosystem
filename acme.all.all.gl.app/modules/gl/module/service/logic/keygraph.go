package logic

import (
	"errors"
	"fmt"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// maxGraphEvents bounds one key-graph request; the simulator is for trying a configuration on
// sample data, not for bulk processing.
const maxGraphEvents = 1000

type keyQuery struct {
	Key    string            `json:"key"`
	Values map[string]string `json:"values"`
}

// keyGraphVersion (US-010) applies sample events to a key graph under one configuration
// version and answers rebuild and investigation queries. Like the simulator it is read-only:
// nothing is stored. Events belong to the caller's tenant; an event naming another tenant is
// refused without being read.
func keyGraphVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	var body struct {
		Events  []model.EventInput `json:"events"`
		Rebuild []keyQuery         `json:"rebuild,omitempty"`
		Related []keyQuery         `json:"related,omitempty"`
	}
	if e := decodeBody(c.req, &body); e != nil {
		return e.response()
	}
	if len(body.Events) > maxGraphEvents {
		return errInvalid("events", fmt.Sprintf("at most %d events per request", maxGraphEvents)).response()
	}
	doc, v, e := loadValidDocument(c, configID, n)
	if e != nil {
		return e.response()
	}
	g := model.NewKeyGraph(c.id.Tenant, doc)
	results := make([]map[string]any, 0, len(body.Events))
	for i, in := range body.Events {
		if in.TenantID == "" {
			in.TenantID = c.id.Tenant // the caller's tenant; another tenant's events are refused
		}
		results = append(results, map[string]any{"index": i, "sourceSystem": in.SourceSystem, "sourceEventId": in.SourceEventID,
			"sourceEventVersion": in.SourceEventVersion, "result": g.AddEvent(in)})
	}
	ref := func(field string, q keyQuery) (model.KeyRef, *apiError) {
		r, err := g.Ref(q.Key, q.Values)
		var ce *model.CodeError
		if errors.As(err, &ce) {
			return r, &apiError{Status: 422, Code: ce.Code, Message: ce.Message, Field: field}
		}
		return r, nil
	}
	rebuild := make([]model.RebuildScope, 0, len(body.Rebuild))
	for i, q := range body.Rebuild {
		r, e := ref(fmt.Sprintf("rebuild/%d", i), q)
		if e != nil {
			return e.response()
		}
		rebuild = append(rebuild, g.Rebuild(r))
	}
	related := make([]map[string]any, 0, len(body.Related))
	for i, q := range body.Related {
		r, e := ref(fmt.Sprintf("related/%d", i), q)
		if e != nil {
			return e.response()
		}
		related = append(related, map[string]any{"start": r, "keys": g.Related(r)})
	}
	exceptions := g.Exceptions
	if exceptions == nil {
		exceptions = []model.KeyException{}
	}
	return jsonResponse(200, map[string]any{"configId": configID, "version": n, "status": v.Status,
		"results": results, "exceptions": exceptions, "rebuild": rebuild, "related": related})
}

// loadValidDocument loads a version the caller may read and parses it; a version with
// blocking findings cannot be simulated.
func loadValidDocument(c *call, configID string, n int) (*model.Document, VersionView, *apiError) {
	v, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return nil, v, e
	}
	doc, fs := model.Parse(v.Content)
	fs = append(fs, model.Validate(doc)...)
	for _, f := range fs {
		if f.Severity == "error" {
			return nil, v, &apiError{Status: 422, Code: "VALIDATION_FAILED", Message: "this version has blocking findings; validate it to see them", Detail: f}
		}
	}
	return doc, v, nil
}
