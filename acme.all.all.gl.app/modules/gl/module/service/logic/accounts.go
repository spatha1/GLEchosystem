package logic

import (
	"errors"
	"time"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// accountView is an account with what its class says about it.
type accountView struct {
	model.Account
	Ledger     string `json:"ledger"`
	Nature     string `json:"nature"`
	NormalSide string `json:"normalSide"`
	Statement  string `json:"statement"`
}

func viewAccount(d *model.Document, a model.Account) accountView {
	c, _ := model.ClassOf(d, a.Class)
	return accountView{Account: a, Ledger: a.LedgerOf(), Nature: c.Nature, NormalSide: c.Side(), Statement: c.Statement()}
}

func versionDocument(c *call) (string, int, *model.Document, VersionView, *apiError) {
	configID := c.params["configId"]
	if e := pathCode("configId", configID); e != nil {
		return "", 0, nil, VersionView{}, e
	}
	n, e := pathVersion(c.params["version"])
	if e != nil {
		return "", 0, nil, VersionView{}, e
	}
	doc, v, e := loadValidDocument(c, configID, n)
	return configID, n, doc, v, e
}

// listAccounts (US-011) is the COA lookup: the accounts of a version, filtered by class,
// ledger, entity, currency and the day a line would book on. With eventType (and list) and
// class it also says whether that event can feed lines of the class: whether it carries the
// subledger keys and dimensions the class's accounts require — checked before any data arrives.
func listAccounts(c *call) modulesdk.Response {
	configID, n, doc, v, e := versionDocument(c)
	if e != nil {
		return e.response()
	}
	q := model.AccountQuery{Class: c.query("class"), Ledger: c.query("ledger"), Entity: c.query("entity"), Currency: c.query("currency")}
	if s := c.query("date"); s != "" {
		day, ok := model.ParseDay(s)
		if !ok {
			return errInvalid("date", "date must be YYYY-MM-DD").response()
		}
		q.Day = day
	}
	accounts := []accountView{}
	for _, a := range model.AccountsFor(doc, q) {
		accounts = append(accounts, viewAccount(doc, a))
	}
	body := map[string]any{"configId": configID, "version": n, "status": v.Status, "accounts": accounts}
	if q.Class != "" {
		body["requirements"] = model.RequirementsOf(doc, q.Class)
	}
	if ev := c.query("eventType"); ev != "" {
		if q.Class == "" {
			return errInvalid("class", "checking an event needs the class its lines would post to").response()
		}
		fs := model.CheckCarrier(doc, ev, c.query("list"), q.Class)
		if fs == nil {
			fs = []model.Finding{}
		}
		body["carrier"] = map[string]any{"eventType": ev, "list": c.query("list"), "class": q.Class, "ok": len(fs) == 0, "findings": fs}
	}
	return jsonResponse(200, body)
}

// checkAccount (US-011) previews the posting gate for one line: may it post to this account?
// The answer is explicit — the account, or the reason it is refused (MISSING_ACCOUNT,
// INACTIVE_ACCOUNT, CLASS_MISMATCH, ENTITY_NOT_ALLOWED, CURRENCY_NOT_ALLOWED,
// MISSING_SUBLEDGER_KEY, MISSING_DIMENSION). Read-only.
func checkAccount(c *call) modulesdk.Response {
	var in struct {
		Account    string            `json:"account"`
		Ledger     string            `json:"ledger,omitempty"`
		Class      string            `json:"class"`
		BookDate   string            `json:"bookDate"`
		Entity     string            `json:"entity,omitempty"`
		Currency   string            `json:"currency"`
		Keys       map[string]string `json:"keys,omitempty"`
		Dimensions map[string]string `json:"dimensions,omitempty"`
	}
	if e := decodeBody(c.req, &in); e != nil {
		return e.response()
	}
	day, ok := model.ParseDay(in.BookDate)
	if !ok {
		return errInvalid("bookDate", "bookDate must be YYYY-MM-DD").response()
	}
	configID, n, doc, v, e := versionDocument(c)
	if e != nil {
		return e.response()
	}
	a, err := model.CheckPosting(doc, model.PostingLine{Account: in.Account, Ledger: in.Ledger, Class: in.Class, BookDate: day,
		Entity: in.Entity, Currency: in.Currency, Keys: in.Keys, Dimensions: in.Dimensions})
	body := map[string]any{"configId": configID, "version": n, "status": v.Status, "allowed": err == nil, "bookDate": day.Format(time.DateOnly)}
	var ce *model.CodeError
	switch {
	case err == nil:
		body["account"] = viewAccount(doc, a)
	case errors.As(err, &ce):
		body["code"], body["message"] = ce.Code, ce.Message
	default:
		return errInternal("posting check failed").response()
	}
	return jsonResponse(200, body)
}
