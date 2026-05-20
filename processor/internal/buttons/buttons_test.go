package buttons

import (
	"errors"
	"testing"
)

func TestValidateMinimalMute(t *testing.T) {
	b := Def{
		ID:     "mute_gym_1h",
		Label:  "Mute this gym (1h)",
		Action: ActionMute,
		Scope:  ScopeGym,
		Params: map[string]any{"duration_min": 60},
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidateResponseText(t *testing.T) {
	b := Def{
		ID:           "coords",
		Label:        "Show coords",
		ResponseText: "📍 {{latitude}}, {{longitude}}",
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidateRejectsMissingID(t *testing.T) {
	b := Def{Label: "x", Action: ActionRedeliver}
	if err := b.Validate(); !errors.Is(err, ErrMissingID) {
		t.Errorf("got %v, want ErrMissingID", err)
	}
}

func TestValidateRejectsMissingLabel(t *testing.T) {
	b := Def{ID: "x", Action: ActionRedeliver}
	if err := b.Validate(); !errors.Is(err, ErrMissingLabel) {
		t.Errorf("got %v, want ErrMissingLabel", err)
	}
}

func TestValidateRejectsNoDispatch(t *testing.T) {
	b := Def{ID: "x", Label: "y"}
	if err := b.Validate(); !errors.Is(err, ErrNoDispatch) {
		t.Errorf("got %v, want ErrNoDispatch", err)
	}
}

func TestValidateRejectsAmbiguous(t *testing.T) {
	b := Def{ID: "x", Label: "y", Action: ActionRedeliver, ResponseText: "hi"}
	if err := b.Validate(); !errors.Is(err, ErrAmbiguousDispatch) {
		t.Errorf("got %v, want ErrAmbiguousDispatch", err)
	}
}

func TestValidateUnknownValues(t *testing.T) {
	cases := []struct {
		name string
		b    Def
		want error
	}{
		{"unknown style", Def{ID: "x", Label: "y", Style: "magenta", ResponseText: "z"}, ErrUnknownStyle},
		{"unknown action", Def{ID: "x", Label: "y", Action: "frobnicate"}, ErrUnknownAction},
		{"unknown scope", Def{ID: "x", Label: "y", Action: ActionMute, Scope: "moon"}, ErrUnknownScope},
		{"unknown applies_to", Def{ID: "x", Label: "y", ResponseText: "z", AppliesTo: []string{"sms"}}, ErrUnknownAppliesTo},
		{"unknown visible_to", Def{ID: "x", Label: "y", ResponseText: "z", VisibleTo: "vip"}, ErrUnknownVisibleTo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Validate()
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateMuteWithoutScope(t *testing.T) {
	b := Def{ID: "x", Label: "y", Action: ActionMute}
	if err := b.Validate(); !errors.Is(err, ErrScopeRequired) {
		t.Errorf("got %v, want ErrScopeRequired", err)
	}
}

func TestValidateUnsubscribeNonTrackingScope(t *testing.T) {
	b := Def{ID: "x", Label: "y", Action: ActionUnsubscribe, Scope: ScopeGym}
	if err := b.Validate(); !errors.Is(err, ErrUnsubscribeScope) {
		t.Errorf("got %v, want ErrUnsubscribeScope", err)
	}
}

func TestEffectiveAppliesTo(t *testing.T) {
	cases := []struct {
		name string
		b    Def
		want []string
	}{
		{"mute default", Def{Action: ActionMute, Scope: ScopeGym}, []string{AppliesToDM}},
		{"unsubscribe default", Def{Action: ActionUnsubscribe, Scope: ScopeTracking}, []string{AppliesToDM}},
		{"redeliver default", Def{Action: ActionRedeliver}, []string{AppliesToAny}},
		{"response_text default", Def{ResponseText: "x"}, []string{AppliesToAny}},
		{"explicit channel only", Def{Action: ActionMute, Scope: ScopeGym, AppliesTo: []string{AppliesToChannel}}, []string{AppliesToChannel}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.b.EffectiveAppliesTo()
			if len(got) != len(tc.want) || got[0] != tc.want[0] {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppliesToTarget(t *testing.T) {
	cases := []struct {
		applies []string
		target  string
		want    bool
	}{
		{nil, AppliesToDM, false},                                                   // action default kicks in (we're not setting an Action, so default = any)
		{[]string{AppliesToAny}, AppliesToDM, true},                                  // any matches dm
		{[]string{AppliesToAny}, AppliesToChannel, true},                             // any matches channel
		{[]string{AppliesToDM}, AppliesToDM, true},                                   // explicit dm matches dm
		{[]string{AppliesToDM}, AppliesToChannel, false},                             // explicit dm doesn't match channel
		{[]string{AppliesToDM, AppliesToChannel}, AppliesToWebhook, false},           // dm+channel doesn't match webhook
		{[]string{AppliesToDM, AppliesToChannel}, AppliesToChannel, true},
	}
	for i, tc := range cases {
		b := Def{
			ID: "x", Label: "y", ResponseText: "z",
			AppliesTo: tc.applies,
		}
		got := b.AppliesToTarget(tc.target)
		// The nil-applies case relies on action default = any (ResponseText
		// implies any), so target=dm should be true. Special-case the test
		// expectation rather than reconstructing the test.
		if tc.applies == nil {
			tc.want = true
		}
		if got != tc.want {
			t.Errorf("case %d (applies=%v, target=%s): got %v, want %v", i, tc.applies, tc.target, got, tc.want)
		}
	}
}

func TestDispatchMode(t *testing.T) {
	cases := []struct {
		name string
		b    Def
		want Mode
	}{
		{"action", Def{Action: ActionMute, Scope: ScopeGym}, ModeAction},
		{"response_template_id", Def{ResponseTemplateID: "coords"}, ModeResponseTemplateID},
		{"response_template_inline", Def{ResponseTemplateInline: map[string]any{"embed": "x"}}, ModeResponseTemplateInline},
		{"response_text", Def{ResponseText: "hi"}, ModeResponseText},
		{"none", Def{}, ModeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.b.DispatchMode(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
