package commands

import (
	"reflect"
	"testing"
)

// TestInfoSplitFormArg — `!info <pokemon> form:<x>` accepts the user's
// translated form prefix (e.g. French "forme:") as well as English "form:".
func TestInfoSplitFormArg(t *testing.T) {
	cases := []struct {
		lang     string
		args     []string
		wantForm string
		wantRest []string
	}{
		{"en", []string{"pikachu", "form:alola"}, "alola", []string{"pikachu"}},
		{"fr", []string{"pikachu", "forme:alola"}, "alola", []string{"pikachu"}},
		{"fr", []string{"pikachu", "Form:Alola"}, "alola", []string{"pikachu"}},
		{"en", []string{"pikachu", "forme:alola"}, "", []string{"pikachu", "forme:alola"}},
	}
	for _, tc := range cases {
		ctx, _ := testCtx(t)
		ctx.Language = tc.lang
		form, rest := splitInfoFormArg(ctx, tc.args)
		if form != tc.wantForm || !reflect.DeepEqual(rest, tc.wantRest) {
			t.Errorf("lang=%s args=%q: got (%q, %q), want (%q, %q)", tc.lang, tc.args, form, rest, tc.wantForm, tc.wantRest)
		}
	}
}
