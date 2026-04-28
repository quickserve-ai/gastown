package cmd

import (
	"errors"
	"testing"
)

type fakeOmpInspector struct {
	agents map[string]string
	idle   map[string]bool
	hooks  map[string]string
	errs   map[string]error
}

func (f fakeOmpInspector) AgentRuntime(session string) (string, error) {
	if err := f.errs["agent:"+session]; err != nil {
		return "", err
	}
	return f.agents[session], nil
}

func (f fakeOmpInspector) IsIdle(session string) bool {
	return f.idle[session]
}

func (f fakeOmpInspector) HookedWork(target string) (string, error) {
	if err := f.errs["hook:"+target]; err != nil {
		return "", err
	}
	return f.hooks[target], nil
}

func TestBuildOmpCrewRestartPlanSelectsOnlyIdleUnhookedOmpCrew(t *testing.T) {
	t.Parallel()

	agents := []*AgentSession{
		{Name: "qc-crew-barry", Type: AgentCrew, Rig: "qcore", AgentName: "barry"},
		{Name: "qc-crew-conway", Type: AgentCrew, Rig: "qcore", AgentName: "conway"},
		{Name: "qc-crew-cyril", Type: AgentCrew, Rig: "qcore", AgentName: "cyril"},
		{Name: "qc-polecat-nux", Type: AgentPolecat, Rig: "qcore", AgentName: "nux"},
	}
	plan := buildOmpCrewRestartPlan(agents, fakeOmpInspector{
		agents: map[string]string{
			"qc-crew-barry":  "omp",
			"qc-crew-conway": "omp",
			"qc-crew-cyril":  "claude",
			"qc-polecat-nux": "omp",
		},
		idle: map[string]bool{
			"qc-crew-barry":  true,
			"qc-crew-conway": false,
			"qc-crew-cyril":  true,
			"qc-polecat-nux": true,
		},
		hooks: map[string]string{
			"qcore/crew/barry": "",
		},
	}, "")

	if len(plan.Eligible) != 1 {
		t.Fatalf("Eligible length = %d, want 1: %#v", len(plan.Eligible), plan.Eligible)
	}
	got := plan.Eligible[0]
	if got.SessionName != "qc-crew-barry" || got.Rig != "qcore" || got.CrewName != "barry" {
		t.Fatalf("eligible[0] = %#v, want qcore/crew/barry", got)
	}

	if !planSkipped(plan, "qc-crew-conway", "busy") {
		t.Fatalf("missing busy skip in %#v", plan.Skipped)
	}
	if planSkipped(plan, "qc-crew-cyril", "") {
		t.Fatalf("non-OMP crew should be ignored, not reported as restart skip: %#v", plan.Skipped)
	}
	if planSkipped(plan, "qc-polecat-nux", "") {
		t.Fatalf("polecat should be ignored, not reported as crew skip: %#v", plan.Skipped)
	}
}

func TestBuildOmpCrewRestartPlanSkipsHookedCrew(t *testing.T) {
	t.Parallel()

	agents := []*AgentSession{{Name: "qc-crew-barry", Type: AgentCrew, Rig: "qcore", AgentName: "barry"}}
	plan := buildOmpCrewRestartPlan(agents, fakeOmpInspector{
		agents: map[string]string{"qc-crew-barry": "omp"},
		idle:   map[string]bool{"qc-crew-barry": true},
		hooks:  map[string]string{"qcore/crew/barry": "hq-wisp-123"},
	}, "")

	if len(plan.Eligible) != 0 {
		t.Fatalf("Eligible = %#v, want none", plan.Eligible)
	}
	if !planSkipped(plan, "qc-crew-barry", "hooked:hq-wisp-123") {
		t.Fatalf("missing hooked skip in %#v", plan.Skipped)
	}
}

func TestBuildOmpCrewRestartPlanFailsClosedWhenHookLookupErrors(t *testing.T) {
	t.Parallel()

	agents := []*AgentSession{{Name: "qc-crew-barry", Type: AgentCrew, Rig: "qcore", AgentName: "barry"}}
	plan := buildOmpCrewRestartPlan(agents, fakeOmpInspector{
		agents: map[string]string{"qc-crew-barry": "omp"},
		idle:   map[string]bool{"qc-crew-barry": true},
		errs:   map[string]error{"hook:qcore/crew/barry": errors.New("dolt unavailable")},
	}, "")

	if len(plan.Eligible) != 0 {
		t.Fatalf("Eligible = %#v, want none", plan.Eligible)
	}
	if !planSkipped(plan, "qc-crew-barry", "hook-check-failed") {
		t.Fatalf("missing fail-closed skip in %#v", plan.Skipped)
	}
}

func TestBuildOmpCrewRestartPlanHonorsRigFilter(t *testing.T) {
	t.Parallel()

	agents := []*AgentSession{
		{Name: "qc-crew-barry", Type: AgentCrew, Rig: "qcore", AgentName: "barry"},
		{Name: "gt-crew-woodhouse", Type: AgentCrew, Rig: "gastown", AgentName: "woodhouse"},
	}
	plan := buildOmpCrewRestartPlan(agents, fakeOmpInspector{
		agents: map[string]string{
			"qc-crew-barry":     "omp",
			"gt-crew-woodhouse": "omp",
		},
		idle: map[string]bool{
			"qc-crew-barry":     true,
			"gt-crew-woodhouse": true,
		},
	}, "qcore")

	if len(plan.Eligible) != 1 || plan.Eligible[0].SessionName != "qc-crew-barry" {
		t.Fatalf("Eligible = %#v, want only qcore/barry", plan.Eligible)
	}
}

func TestParseOmpUpdateCheckOutput(t *testing.T) {
	t.Parallel()

	current, next, update := parseOmpUpdateCheckOutput("Current version: 14.2.1\nNew version available: 14.5.3\n")
	if !update || current != "14.2.1" || next != "14.5.3" {
		t.Fatalf("parse update = current %q next %q update %v", current, next, update)
	}

	current, next, update = parseOmpUpdateCheckOutput("Current version: 14.5.3\nAlready up to date.\n")
	if update || current != "14.5.3" || next != "" {
		t.Fatalf("parse no update = current %q next %q update %v", current, next, update)
	}
}

func planSkipped(plan ompCrewRestartPlan, session, reason string) bool {
	for _, skipped := range plan.Skipped {
		if skipped.SessionName != session {
			continue
		}
		if reason == "" || skipped.Reason == reason {
			return true
		}
	}
	return false
}
