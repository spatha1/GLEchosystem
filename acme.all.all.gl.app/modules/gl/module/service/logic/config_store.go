package logic

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// ── views ────────────────────────────────────────────────────────────────────

// VersionView is a configuration version as the API returns it (spec C1 envelope).
type VersionView struct {
	TenantID         string          `json:"tenantId"`
	ConfigID         string          `json:"configId"`
	Version          int             `json:"version"`
	Status           string          `json:"status"`
	SchemaVersion    string          `json:"schemaVersion"`
	EffectiveFrom    string          `json:"effectiveFrom"`
	EffectiveTo      *string         `json:"effectiveTo"`
	ContentDigest    string          `json:"contentDigest"`
	BasedOnVersion   *int            `json:"basedOnVersion,omitempty"`
	CreatedBy        string          `json:"createdBy"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedBy        string          `json:"updatedBy"`
	UpdatedAt        string          `json:"updatedAt"`
	ValidatedBy      *string         `json:"validatedBy,omitempty"`
	ValidatedAt      *string         `json:"validatedAt,omitempty"`
	ApprovedBy       *string         `json:"approvedBy,omitempty"`
	ApprovedAt       *string         `json:"approvedAt,omitempty"`
	RetiredBy        *string         `json:"retiredBy,omitempty"`
	RetiredAt        *string         `json:"retiredAt,omitempty"`
	Content          json.RawMessage `json:"content,omitempty"`
	ValidationReport json.RawMessage `json:"validationReport,omitempty"`
}

func ts(col string) string {
	return "coalesce(to_char(" + col + " at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'), '')"
}

// versionCols is the select list scanVersion reads, in order.
var versionCols = strings.Join([]string{
	"tenant_id", "config_id", "version::text", "status", "schema_version", "effective_from::text",
	"coalesce(effective_to::text, '')", "content_digest", "coalesce(based_on_version::text, '')",
	"created_by", ts("created_at"), "updated_by", ts("updated_at"),
	"coalesce(validated_by, '')", ts("validated_at"), "coalesce(approved_by, '')", ts("approved_at"),
	"coalesce(retired_by, '')", ts("retired_at"),
}, ", ")

const withContentCols = ", content::text, coalesce(validation_report::text, '')"

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func scanVersion(r modulesdk.Row, withContent bool) (VersionView, error) {
	c := r.Cells
	want := 19
	if withContent {
		want += 2
	}
	if len(c) != want {
		return VersionView{}, fmt.Errorf("version row has %d cells, want %d", len(c), want)
	}
	v, err := strconv.Atoi(c[2])
	if err != nil {
		return VersionView{}, err
	}
	out := VersionView{
		TenantID: c[0], ConfigID: c[1], Version: v, Status: c[3], SchemaVersion: c[4], EffectiveFrom: c[5],
		EffectiveTo: opt(c[6]), ContentDigest: c[7], CreatedBy: c[9], CreatedAt: c[10], UpdatedBy: c[11],
		UpdatedAt: c[12], ValidatedBy: opt(c[13]), ValidatedAt: opt(c[14]), ApprovedBy: opt(c[15]),
		ApprovedAt: opt(c[16]), RetiredBy: opt(c[17]), RetiredAt: opt(c[18]),
	}
	if c[8] != "" {
		b, err := strconv.Atoi(c[8])
		if err != nil {
			return VersionView{}, err
		}
		out.BasedOnVersion = &b
	}
	if withContent {
		out.Content = json.RawMessage(c[19])
		if c[20] != "" {
			out.ValidationReport = json.RawMessage(c[20])
		}
	}
	return out, nil
}

// ── storage helpers ──────────────────────────────────────────────────────────

// guarded runs f inside a savepoint. A statement that fails for a business reason (a
// duplicate, an immutability trigger, an effective-date clash) is rolled back to the
// savepoint, so the request's transaction stays usable and the caller receives a precise
// 4xx instead of a failed commit.
func guarded(h modulesdk.Host, f func() error) error {
	if _, err := h.Exec("savepoint gl_guard", nil); err != nil {
		return err
	}
	if err := f(); err != nil {
		if _, rb := h.Exec("rollback to savepoint gl_guard", nil); rb != nil {
			return fmt.Errorf("%v (rollback to savepoint failed: %v)", err, rb)
		}
		return err
	}
	_, err := h.Exec("release savepoint gl_guard", nil)
	return err
}

// classify maps database errors raised by the schema's own invariants to API errors.
func classify(err error) *apiError {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "GL_CONFIG_IMMUTABLE"):
		return errConflict("CONFIG_IMMUTABLE", "approved and retired configuration versions cannot be changed; publish a new version", false)
	case strings.Contains(msg, "GL_AUDIT_APPEND_ONLY"):
		return errConflict("AUDIT_APPEND_ONLY", "audit entries cannot be changed", false)
	case strings.Contains(msg, "config_version_one_approved_start"):
		return errConflict("EFFECTIVE_DATE_CONFLICT", "another approved version of this configuration already starts on this date", false)
	case strings.Contains(msg, "config_artifact_pkey"):
		return errConflict("ALREADY_EXISTS", "a configuration with this id already exists for the tenant", false)
	case strings.Contains(msg, "config_version_pkey"):
		return errConflict("CONCURRENT_CHANGE", "another version was created at the same time; retry", true)
	}
	return errInternal("storage error")
}

func audit(h modulesdk.Host, id Identity, configID string, version int, action, reason, from, to, digest string, detail any) error {
	d := "{}"
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		d = string(b)
	}
	v := ""
	if version > 0 {
		v = strconv.Itoa(version)
	}
	_, err := h.Exec(`insert into config_audit (tenant_id, config_id, version, action, actor, reason, from_status, to_status, content_digest, detail)
		values ($1, $2, nullif($3, '')::integer, $4, $5, nullif($6, ''), nullif($7, ''), nullif($8, ''), nullif($9, ''), $10::jsonb)`,
		[]string{id.Tenant, configID, v, action, id.Actor, reason, from, to, digest, d})
	return err
}

func loadVersion(h modulesdk.Host, id Identity, configID string, version int, withContent bool) (VersionView, *apiError) {
	cols := versionCols
	if withContent {
		cols += withContentCols
	}
	rows, err := h.Query("select "+cols+" from config_version where tenant_id = $1 and config_id = $2 and version = $3::integer",
		[]string{id.Tenant, configID, strconv.Itoa(version)})
	if err != nil {
		return VersionView{}, errInternal("storage error")
	}
	if len(rows) == 0 {
		return VersionView{}, errNotFound(fmt.Sprintf("configuration %s version %d not found", configID, version))
	}
	v, err := scanVersion(rows[0], withContent)
	if err != nil {
		return VersionView{}, errInternal("storage error")
	}
	return v, nil
}

func artifactExists(h modulesdk.Host, id Identity, configID string) (bool, *apiError) {
	rows, err := h.Query("select 1 from config_artifact where tenant_id = $1 and config_id = $2", []string{id.Tenant, configID})
	if err != nil {
		return false, errInternal("storage error")
	}
	return len(rows) > 0, nil
}

// ── request bodies ───────────────────────────────────────────────────────────

func decodeBody(req modulesdk.Request, v any) *apiError {
	if len(req.Body) == 0 {
		return errInvalid("", "request body is required")
	}
	dec := json.NewDecoder(strings.NewReader(string(req.Body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errInvalid("", "invalid JSON body: "+err.Error())
	}
	return nil
}

func pathCode(field, s string) *apiError {
	if !codeRe.MatchString(s) {
		return errInvalid(field, field+" must be an upper-case code (A-Z, 0-9, _), starting with a letter")
	}
	return nil
}

func pathVersion(s string) (int, *apiError) {
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return 0, errInvalid("version", "version must be a positive integer")
	}
	return v, nil
}

func contentOrError(raw json.RawMessage) ([]byte, string, map[string][]map[string]any, *apiError) {
	c, d, shape, err := CanonicalContent(raw)
	if err != nil {
		return nil, "", nil, errInvalid("content", err.Error())
	}
	return c, d, shape, nil
}

// ── handlers ─────────────────────────────────────────────────────────────────

type createConfigBody struct {
	ConfigID      string          `json:"configId"`
	Title         string          `json:"title"`
	EffectiveFrom string          `json:"effectiveFrom"`
	EffectiveTo   *string         `json:"effectiveTo"`
	Content       json.RawMessage `json:"content"`
	Reason        string          `json:"reason"`
}

func createConfig(c *call) modulesdk.Response {
	var b createConfigBody
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if e := pathCode("configId", b.ConfigID); e != nil {
		return e.response()
	}
	if len(b.Title) > 200 {
		return errInvalid("title", "title is limited to 200 characters").response()
	}
	if e := checkEffective(b.EffectiveFrom, b.EffectiveTo); e != nil {
		return e.response()
	}
	content, digest, _, e := contentOrError(b.Content)
	if e != nil {
		return e.response()
	}
	err := guarded(c.host, func() error {
		if _, err := c.host.Exec("insert into config_artifact (tenant_id, config_id, title, created_by) values ($1, $2, $3, $4)",
			[]string{c.id.Tenant, b.ConfigID, b.Title, c.id.Actor}); err != nil {
			return err
		}
		_, err := c.host.Exec(`insert into config_version (tenant_id, config_id, version, status, schema_version, effective_from, effective_to,
				content, content_digest, created_by, updated_by)
			values ($1, $2, 1, 'draft', $3, $4::date, nullif($5, '')::date, $6::jsonb, $7, $8, $8)`,
			[]string{c.id.Tenant, b.ConfigID, SchemaVersion, b.EffectiveFrom, deref(b.EffectiveTo), string(content), digest, c.id.Actor})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, b.ConfigID, 0, "create_artifact", b.Reason, "", "", "", map[string]string{"title": b.Title}); err != nil {
		return errInternal("audit write failed").response()
	}
	if err := audit(c.host, c.id, b.ConfigID, 1, "create_version", b.Reason, "", "draft", digest, nil); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, b.ConfigID, 1, true)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(201, v)
}

func listConfigs(c *call) modulesdk.Response {
	rows, err := c.host.Query(`select a.config_id, a.title, a.created_by, `+ts("a.created_at")+`,
			coalesce((select max(v.version) from config_version v where v.tenant_id = a.tenant_id and v.config_id = a.config_id), 0)::text
		from config_artifact a where a.tenant_id = $1 order by a.config_id`, []string{c.id.Tenant})
	if err != nil {
		return errInternal("storage error").response()
	}
	type item struct {
		ConfigID      string `json:"configId"`
		Title         string `json:"title"`
		CreatedBy     string `json:"createdBy"`
		CreatedAt     string `json:"createdAt"`
		LatestVersion int    `json:"latestVersion"`
	}
	out := []item{}
	for _, r := range rows {
		n, _ := strconv.Atoi(r.Cells[4])
		out = append(out, item{r.Cells[0], r.Cells[1], r.Cells[2], r.Cells[3], n})
	}
	return jsonResponse(200, map[string]any{"configs": out})
}

func listVersions(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	ok, ae := artifactExists(c.host, c.id, configID)
	if ae != nil {
		return ae.response()
	}
	if !ok {
		return errNotFound("configuration " + configID + " not found").response()
	}
	rows, err := c.host.Query("select "+versionCols+" from config_version where tenant_id = $1 and config_id = $2 order by version",
		[]string{c.id.Tenant, configID})
	if err != nil {
		return errInternal("storage error").response()
	}
	out := []VersionView{}
	for _, r := range rows {
		v, err := scanVersion(r, false)
		if err != nil {
			return errInternal("storage error").response()
		}
		out = append(out, v)
	}
	return jsonResponse(200, map[string]any{"versions": out})
}

func getVersion(c *call) modulesdk.Response {
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
	return jsonResponse(200, v)
}

type createVersionBody struct {
	BasedOnVersion *int            `json:"basedOnVersion"`
	EffectiveFrom  string          `json:"effectiveFrom"`
	EffectiveTo    *string         `json:"effectiveTo"`
	Content        json.RawMessage `json:"content"`
	Reason         string          `json:"reason"`
}

func createVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	var b createVersionBody
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if e := checkEffective(b.EffectiveFrom, b.EffectiveTo); e != nil {
		return e.response()
	}
	ok, ae := artifactExists(c.host, c.id, configID)
	if ae != nil {
		return ae.response()
	}
	if !ok {
		return errNotFound("configuration " + configID + " not found").response()
	}
	raw := b.Content
	based := ""
	if b.BasedOnVersion != nil {
		src, e := loadVersion(c.host, c.id, configID, *b.BasedOnVersion, true)
		if e != nil {
			return e.response()
		}
		based = strconv.Itoa(*b.BasedOnVersion)
		if len(raw) == 0 {
			raw = src.Content
		}
	}
	content, digest, _, e := contentOrError(raw)
	if e != nil {
		return e.response()
	}
	var created int
	err := guarded(c.host, func() error {
		rows, err := c.host.Query(`insert into config_version (tenant_id, config_id, version, status, schema_version, effective_from, effective_to,
				content, content_digest, based_on_version, created_by, updated_by)
			select $1, $2, coalesce(max(version), 0) + 1, 'draft', $3, $4::date, nullif($5, '')::date, $6::jsonb, $7, nullif($8, '')::integer, $9, $9
			from config_version where tenant_id = $1 and config_id = $2
			returning version::text`,
			[]string{c.id.Tenant, configID, SchemaVersion, b.EffectiveFrom, deref(b.EffectiveTo), string(content), digest, based, c.id.Actor})
		if err != nil {
			return err
		}
		created, err = strconv.Atoi(rows[0].Cells[0])
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, configID, created, "create_version", b.Reason, "", "draft", digest, map[string]string{"basedOnVersion": based}); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, configID, created, true)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(201, v)
}

type updateDraftBody struct {
	BaseDigest       string          `json:"baseDigest"`
	EffectiveFrom    *string         `json:"effectiveFrom"`
	EffectiveTo      *string         `json:"effectiveTo"`
	ClearEffectiveTo bool            `json:"clearEffectiveTo"`
	Content          json.RawMessage `json:"content"`
	Reason           string          `json:"reason"`
}

// updateDraft edits a draft (or a validated version, which returns it to draft). It needs
// the digest the editor started from, so two editors cannot silently overwrite each other.
func updateDraft(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	var b updateDraftBody
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	cur, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return e.response()
	}
	if cur.Status == "approved" || cur.Status == "retired" {
		return errConflict("CONFIG_IMMUTABLE", fmt.Sprintf("version %d is %s and cannot be changed; create a new version", n, cur.Status), false).response()
	}
	if b.BaseDigest == "" {
		return errInvalid("baseDigest", "baseDigest (the contentDigest you edited from) is required").response()
	}
	if b.BaseDigest != cur.ContentDigest {
		return errConflict("STALE_VERSION", "the version changed since you read it; reload and reapply your edit", true).response()
	}
	from := cur.EffectiveFrom
	if b.EffectiveFrom != nil {
		from = *b.EffectiveFrom
	}
	to := cur.EffectiveTo
	if b.EffectiveTo != nil {
		to = b.EffectiveTo
	}
	if b.ClearEffectiveTo {
		to = nil
	}
	if e := checkEffective(from, to); e != nil {
		return e.response()
	}
	raw := b.Content
	if len(raw) == 0 {
		raw = cur.Content
	}
	content, digest, _, e := contentOrError(raw)
	if e != nil {
		return e.response()
	}
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`update config_version set status = 'draft', effective_from = $4::date, effective_to = nullif($5, '')::date,
				content = $6::jsonb, content_digest = $7, validated_by = null, validated_at = null, validation_report = null,
				updated_by = $8, updated_at = now()
			where tenant_id = $1 and config_id = $2 and version = $3::integer`,
			[]string{c.id.Tenant, configID, strconv.Itoa(n), from, deref(to), string(content), digest, c.id.Actor})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, configID, n, "update_draft", b.Reason, cur.Status, "draft", digest,
		map[string]string{"previousDigest": cur.ContentDigest}); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, configID, n, true)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(200, v)
}

type reasonBody struct {
	Reason string `json:"reason"`
}

func optionalReason(req modulesdk.Request) (string, *apiError) {
	if len(strings.TrimSpace(string(req.Body))) == 0 {
		return "", nil
	}
	var b reasonBody
	if e := decodeBody(req, &b); e != nil {
		return "", e
	}
	return b.Reason, nil
}

// validateVersion checks a draft. A failing draft stays a draft; the failure is audited and
// the report returned with 422 so the BA sees exactly what to fix.
func validateVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	reason, e := optionalReason(c.req)
	if e != nil {
		return e.response()
	}
	cur, e := loadVersion(c.host, c.id, configID, n, true)
	if e != nil {
		return e.response()
	}
	if cur.Status != "draft" {
		return errConflict("INVALID_STATE", fmt.Sprintf("only a draft can be validated; version %d is %s", n, cur.Status), false).response()
	}
	_, digest, shape, e := contentOrError(cur.Content)
	if e != nil {
		return e.response()
	}
	if digest != cur.ContentDigest {
		return errInternal("stored content does not match its digest").response()
	}
	rep := ValidateContent(shape)
	repJSON, _ := json.Marshal(rep)
	if !rep.OK {
		if err := audit(c.host, c.id, configID, n, "validation_failed", reason, "draft", "draft", digest, rep); err != nil {
			return errInternal("audit write failed").response()
		}
		ae := &apiError{Status: 422, Code: "VALIDATION_FAILED", Message: "the configuration has blocking findings", Detail: rep}
		return ae.response()
	}
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`update config_version set status = 'validated', validated_by = $4, validated_at = now(),
				validation_report = $5::jsonb, updated_by = $4, updated_at = now()
			where tenant_id = $1 and config_id = $2 and version = $3::integer and status = 'draft'`,
			[]string{c.id.Tenant, configID, strconv.Itoa(n), c.id.Actor, string(repJSON)})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, configID, n, "validate", reason, "draft", "validated", digest, rep); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, configID, n, true)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(200, v)
}

// approveVersion publishes a validated version. From here on the database refuses any
// change to it except retirement.
func approveVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	reason, e := optionalReason(c.req)
	if e != nil {
		return e.response()
	}
	cur, e := loadVersion(c.host, c.id, configID, n, false)
	if e != nil {
		return e.response()
	}
	if cur.Status != "validated" {
		return errConflict("INVALID_STATE", fmt.Sprintf("only a validated version can be approved; version %d is %s", n, cur.Status), false).response()
	}
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`update config_version set status = 'approved', approved_by = $4, approved_at = now(), updated_by = $4, updated_at = now()
			where tenant_id = $1 and config_id = $2 and version = $3::integer and status = 'validated'`,
			[]string{c.id.Tenant, configID, strconv.Itoa(n), c.id.Actor})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, configID, n, "approve", reason, "validated", "approved", cur.ContentDigest, nil); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, configID, n, true)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(200, v)
}

// retireVersion takes a version out of use. An approved version keeps its content; only
// its status changes. A draft or validated version can be retired to abandon it.
func retireVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return e.response()
	}
	reason, e := optionalReason(c.req)
	if e != nil {
		return e.response()
	}
	if strings.TrimSpace(reason) == "" {
		return errInvalid("reason", "a reason is required to retire a version").response()
	}
	cur, e := loadVersion(c.host, c.id, configID, n, false)
	if e != nil {
		return e.response()
	}
	if cur.Status == "retired" {
		return errConflict("INVALID_STATE", fmt.Sprintf("version %d is already retired", n), false).response()
	}
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`update config_version set status = 'retired', retired_by = $4, retired_at = now(), updated_by = $4, updated_at = now()
			where tenant_id = $1 and config_id = $2 and version = $3::integer`,
			[]string{c.id.Tenant, configID, strconv.Itoa(n), c.id.Actor})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, configID, n, "retire", reason, cur.Status, "retired", cur.ContentDigest, nil); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, configID, n, false)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(200, v)
}

// effectiveVersion resolves the approved version in force on a date: among approved
// versions whose window [effectiveFrom, effectiveTo) contains the date, the one with the
// latest effectiveFrom. Runtime uses only this; a caller cannot pick a draft.
func effectiveVersion(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	date := c.query("date")
	if _, e := ParseDate("date", date); e != nil {
		return e.response()
	}
	rows, err := c.host.Query("select "+versionCols+withContentCols+` from config_version
		where tenant_id = $1 and config_id = $2 and status = 'approved'
		  and effective_from <= $3::date and (effective_to is null or $3::date < effective_to)
		order by effective_from desc limit 1`, []string{c.id.Tenant, configID, date})
	if err != nil {
		return errInternal("storage error").response()
	}
	if len(rows) == 0 {
		return (&apiError{Status: 404, Code: "NO_EFFECTIVE_VERSION", Message: fmt.Sprintf("no approved version of %s is effective on %s", configID, date)}).response()
	}
	v, err := scanVersion(rows[0], true)
	if err != nil {
		return errInternal("storage error").response()
	}
	return jsonResponse(200, v)
}

// AuditEntry is one configuration audit record.
type AuditEntry struct {
	AuditID       int64           `json:"auditId"`
	ConfigID      string          `json:"configId"`
	Version       *int            `json:"version,omitempty"`
	Action        string          `json:"action"`
	Actor         string          `json:"actor"`
	Reason        *string         `json:"reason,omitempty"`
	FromStatus    *string         `json:"fromStatus,omitempty"`
	ToStatus      *string         `json:"toStatus,omitempty"`
	ContentDigest *string         `json:"contentDigest,omitempty"`
	Detail        json.RawMessage `json:"detail"`
	At            string          `json:"at"`
}

func listAudit(c *call) modulesdk.Response {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return e.response()
	}
	q := `select audit_id::text, config_id, coalesce(version::text, ''), action, actor, coalesce(reason, ''), coalesce(from_status, ''),
			coalesce(to_status, ''), coalesce(content_digest, ''), detail::text, ` + ts("at") + `
		from config_audit where tenant_id = $1 and config_id = $2`
	params := []string{c.id.Tenant, configID}
	if v := c.query("version"); v != "" {
		n, e := pathVersion(v)
		if e != nil {
			return e.response()
		}
		q += " and version = $3::integer"
		params = append(params, strconv.Itoa(n))
	}
	rows, err := c.host.Query(q+" order by audit_id", params)
	if err != nil {
		return errInternal("storage error").response()
	}
	out := []AuditEntry{}
	for _, r := range rows {
		x := r.Cells
		id, _ := strconv.ParseInt(x[0], 10, 64)
		e := AuditEntry{AuditID: id, ConfigID: x[1], Action: x[3], Actor: x[4], Reason: opt(x[5]), FromStatus: opt(x[6]),
			ToStatus: opt(x[7]), ContentDigest: opt(x[8]), Detail: json.RawMessage(x[9]), At: x[10]}
		if x[2] != "" {
			v, _ := strconv.Atoi(x[2])
			e.Version = &v
		}
		out = append(out, e)
	}
	return jsonResponse(200, map[string]any{"entries": out})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
