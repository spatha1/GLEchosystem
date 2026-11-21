package model

import (
	"reflect"
	"testing"
)

func withProfiles(c map[string]any, profiles ...map[string]any) map[string]any {
	var ps []any
	for _, p := range profiles {
		ps = append(ps, p)
	}
	c["lifecycleProfiles"] = ps
	return c
}

// ALTB-07: a profile names scenarios of its own version, in order; it is checked like the rest.
func TestLifecycleProfilesAreValidated(t *testing.T) {
	good := map[string]any{"code": "P1_JANUARY", "description": "Policy P1: premium, invoice, payment", "steps": []any{"E1", "E2", "E3"}}
	if fs := validateMap(withProfiles(fixtureMap(t), good)); len(errorsOf(fs)) > 0 {
		t.Fatalf("a valid profile: %+v", errorsOf(fs))
	}
	for name, c := range map[string]struct {
		p    map[string]any
		code string
	}{
		"unknown scenario": {map[string]any{"code": "P1", "steps": []any{"E1", "E9"}}, "UNKNOWN_SCENARIO"},
		"no steps":         {map[string]any{"code": "P1", "steps": []any{}}, "EMPTY_PROFILE"},
		"bad code":         {map[string]any{"code": "p1", "steps": []any{"E1"}}, "INVALID_CODE"},
		"unknown field":    {map[string]any{"code": "P1", "steps": []any{"E1"}, "amount": "1"}, "INVALID_ITEM"},
	} {
		if fs := validateMap(withProfiles(fixtureMap(t), c.p)); !hasCode(fs, c.code) {
			t.Errorf("%s: want %s, got %v", name, c.code, findingCodes(fs))
		}
	}
	if fs := validateMap(withProfiles(fixtureMap(t), good, good)); !hasCode(fs, "DUPLICATE_PROFILE") {
		t.Errorf("a profile declared twice: %v", findingCodes(fs))
	}
}

// ALTB-07: running a profile is running its scenarios in order.
func TestAProfileRunsItsScenariosInOrder(t *testing.T) {
	d := docOf(t, withProfiles(fixtureMap(t), map[string]any{"code": "P1_JANUARY", "steps": []any{"E1", "E2", "E3"}}))
	p, ok := FindProfile(d, "P1_JANUARY")
	if !ok {
		t.Fatal("profile missing")
	}
	events, err := ProfileEvents(d, p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, scenarioEvents(t, d, "E1", "E2", "E3")) {
		t.Errorf("profile events differ from the scenarios")
	}
	if res := PreviewLifecycle(d, fixtureRef, events); !res.Complete || res.Lines != 12 {
		t.Errorf("profile lifecycle: %+v", res.Stopped)
	}
	if _, err := ProfileEvents(d, LifecycleProfile{Code: "X", Steps: []string{"NOPE"}}); ErrorCode(err) != "UNKNOWN_SCENARIO" {
		t.Errorf("a missing scenario: %v", err)
	}
}
