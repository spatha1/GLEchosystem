package logic

import (
	"sort"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// ootbMeasures returns the global, read-only OOTB measure catalog (US-006). It is the same
// for every tenant; clients never change it — they override in their own configuration.
func ootbMeasures(c *call) modulesdk.Response {
	return jsonResponse(200, map[string]any{"catalogVersion": model.OOTBCatalogVersion(), "measures": model.OOTBMeasures()})
}

// versionMeasures returns the measure catalog in force for one configuration version: the
// OOTB measures with that version's overrides applied, plus its custom measures.
func versionMeasures(c *call) modulesdk.Response {
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
	doc, _ := model.Parse(v.Content)
	resolved := model.ResolveMeasures(doc)
	out := make([]model.Measure, 0, len(resolved))
	for _, m := range resolved {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return jsonResponse(200, map[string]any{
		"configId": configID, "version": n, "contentDigest": v.ContentDigest,
		"ootbCatalogVersion": model.OOTBCatalogVersion(), "measures": out,
	})
}

// ootbDimensions returns the global, read-only OOTB dimension catalog (US-008). Clients add
// values and normalisations in their own configuration, or define custom dimensions.
func ootbDimensions(c *call) modulesdk.Response {
	return jsonResponse(200, map[string]any{"catalogVersion": model.OOTBDimensionCatalogVersion(), "dimensions": model.OOTBDimensions()})
}

// ootbEvents returns the global, read-only insurance event catalog: templates a BA starts an
// event from, with the business keys they use.
func ootbEvents(c *call) modulesdk.Response {
	return jsonResponse(200, model.OOTBEventCatalog())
}
