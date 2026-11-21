package model

// ALTB-07 — versioned synthetic lifecycle profiles. A profile is a synthetic policy's events in
// order: a named list of the version's scenarios. It is versioned, reviewed and approved with
// the rules it exercises, like the scenarios themselves, and a lifecycle preview or a sandbox
// book can run it by name. It holds no data of its own: only scenario codes.

import (
	"fmt"
)

// LifecycleProfile is a named sequence of scenarios.
type LifecycleProfile struct {
	Code        string   `json:"code"`
	Description string   `json:"description,omitempty"`
	Steps       []string `json:"steps"` // scenario codes, in order
}

// FindProfile is the profile with this code.
func FindProfile(d *Document, code string) (LifecycleProfile, bool) {
	for _, p := range d.LifecycleProfiles {
		if p.Code == code {
			return p, true
		}
	}
	return LifecycleProfile{}, false
}

// ProfileEvents is a profile's steps as lifecycle events.
func ProfileEvents(d *Document, p LifecycleProfile) ([]LifecycleEvent, error) {
	out := make([]LifecycleEvent, 0, len(p.Steps))
	for _, code := range p.Steps {
		s, ok := FindScenario(d, code)
		if !ok {
			return nil, &CodeError{"UNKNOWN_SCENARIO", fmt.Sprintf("profile %s names scenario %s, which is not in this version", p.Code, code)}
		}
		in := s.Input
		in.EventType = s.EventType
		out = append(out, LifecycleEvent{Code: s.Code, Input: in})
	}
	return out, nil
}

func init() { validators = append(validators, validateProfiles) }

func validateProfiles(d *Document) []Finding {
	var fs []Finding
	scenarios := map[string]bool{}
	for _, s := range d.Scenarios {
		scenarios[s.Code] = true
	}
	seen := map[string]bool{}
	for i, p := range d.LifecycleProfiles {
		path := fmt.Sprintf("lifecycleProfiles/%d", i)
		if !isCode(p.Code) {
			fs = append(fs, errf(path, "INVALID_CODE", "profile code %q must be an upper-case code", p.Code))
		}
		if seen[p.Code] {
			fs = append(fs, errf(path, "DUPLICATE_PROFILE", "profile %s is declared twice", p.Code))
		}
		seen[p.Code] = true
		if len(p.Steps) == 0 {
			fs = append(fs, errf(path, "EMPTY_PROFILE", "profile %s has no steps: give the scenarios of one synthetic policy, in order", p.Code))
		}
		if len(p.Steps) > 100 {
			fs = append(fs, errf(path, "PROFILE_TOO_LONG", "profile %s has %d steps; a lifecycle takes at most 100", p.Code, len(p.Steps)))
		}
		for j, s := range p.Steps {
			if !scenarios[s] {
				fs = append(fs, errf(fmt.Sprintf("%s/steps/%d", path, j), "UNKNOWN_SCENARIO", "profile %s names scenario %q, which is not in this version", p.Code, s))
			}
		}
	}
	return fs
}
