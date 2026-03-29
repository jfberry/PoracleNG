package dts

import (
	"math"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
)

func init() {
	RegisterHelpers()
}

// render is a test helper that compiles and executes a template with a context.
func render(t *testing.T, source string, ctx interface{}) string {
	t.Helper()
	result, err := raymond.Render(source, ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Comparison helpers
// ---------------------------------------------------------------------------

func TestEq(t *testing.T) {
	ctx := map[string]interface{}{"a": "hello", "b": "hello", "n": 42}

	// Equal strings
	got := render(t, `{{#eq a b}}yes{{else}}no{{/eq}}`, ctx)
	if got != "yes" {
		t.Errorf("eq strings: got %q, want %q", got, "yes")
	}

	// Not equal
	got = render(t, `{{#eq a "world"}}yes{{else}}no{{/eq}}`, ctx)
	if got != "no" {
		t.Errorf("eq not equal: got %q, want %q", got, "no")
	}

	// Number vs string via Sprintf normalization
	got = render(t, `{{#eq n 42}}yes{{else}}no{{/eq}}`, ctx)
	if got != "yes" {
		t.Errorf("eq number: got %q, want %q", got, "yes")
	}
}

func TestIsnt(t *testing.T) {
	ctx := map[string]interface{}{"a": "foo", "b": "bar"}
	got := render(t, `{{#isnt a b}}different{{else}}same{{/isnt}}`, ctx)
	if got != "different" {
		t.Errorf("isnt: got %q, want %q", got, "different")
	}

	got = render(t, `{{#isnt a a}}different{{else}}same{{/isnt}}`, ctx)
	if got != "same" {
		t.Errorf("isnt same: got %q, want %q", got, "same")
	}
}

func TestCompare(t *testing.T) {
	ctx := map[string]interface{}{"x": 10, "y": 20}

	tests := []struct {
		tmpl string
		want string
	}{
		{`{{#compare x "==" 10}}yes{{else}}no{{/compare}}`, "yes"},
		{`{{#compare x "!=" y}}yes{{else}}no{{/compare}}`, "yes"},
		{`{{#compare x "<" y}}yes{{else}}no{{/compare}}`, "yes"},
		{`{{#compare y ">" x}}yes{{else}}no{{/compare}}`, "yes"},
		{`{{#compare x "<=" 10}}yes{{else}}no{{/compare}}`, "yes"},
		{`{{#compare y ">=" 20}}yes{{else}}no{{/compare}}`, "yes"},
		{`{{#compare x ">" y}}yes{{else}}no{{/compare}}`, "no"},
	}
	for _, tt := range tests {
		got := render(t, tt.tmpl, ctx)
		if got != tt.want {
			t.Errorf("compare %q: got %q, want %q", tt.tmpl, got, tt.want)
		}
	}
}

func TestGtGteLtLte(t *testing.T) {
	ctx := map[string]interface{}{"a": 5, "b": 10}

	if got := render(t, `{{#gt b a}}yes{{else}}no{{/gt}}`, ctx); got != "yes" {
		t.Errorf("gt: got %q", got)
	}
	if got := render(t, `{{#gte a 5}}yes{{else}}no{{/gte}}`, ctx); got != "yes" {
		t.Errorf("gte: got %q", got)
	}
	if got := render(t, `{{#lt a b}}yes{{else}}no{{/lt}}`, ctx); got != "yes" {
		t.Errorf("lt: got %q", got)
	}
	if got := render(t, `{{#lte b 10}}yes{{else}}no{{/lte}}`, ctx); got != "yes" {
		t.Errorf("lte: got %q", got)
	}
	// Failing cases
	if got := render(t, `{{#gt a b}}yes{{else}}no{{/gt}}`, ctx); got != "no" {
		t.Errorf("gt fail: got %q", got)
	}
}

func TestAndOr(t *testing.T) {
	ctx := map[string]interface{}{"t": true, "f": false, "s": "hello", "e": ""}

	if got := render(t, `{{#and t s}}yes{{else}}no{{/and}}`, ctx); got != "yes" {
		t.Errorf("and both truthy: got %q", got)
	}
	if got := render(t, `{{#and t f}}yes{{else}}no{{/and}}`, ctx); got != "no" {
		t.Errorf("and one falsy: got %q", got)
	}
	if got := render(t, `{{#or f s}}yes{{else}}no{{/or}}`, ctx); got != "yes" {
		t.Errorf("or one truthy: got %q", got)
	}
	if got := render(t, `{{#or f e}}yes{{else}}no{{/or}}`, ctx); got != "no" {
		t.Errorf("or both falsy: got %q", got)
	}
}

func TestNot(t *testing.T) {
	ctx := map[string]interface{}{"f": false, "t": true}
	if got := render(t, `{{#not f}}yes{{else}}no{{/not}}`, ctx); got != "yes" {
		t.Errorf("not false: got %q", got)
	}
	if got := render(t, `{{#not t}}yes{{else}}no{{/not}}`, ctx); got != "no" {
		t.Errorf("not true: got %q", got)
	}
}

func TestContains(t *testing.T) {
	ctx := map[string]interface{}{
		"s":   "hello world",
		"arr": []string{"apple", "banana", "cherry"},
	}
	if got := render(t, `{{#contains s "world"}}yes{{else}}no{{/contains}}`, ctx); got != "yes" {
		t.Errorf("contains string: got %q", got)
	}
	if got := render(t, `{{#contains s "xyz"}}yes{{else}}no{{/contains}}`, ctx); got != "no" {
		t.Errorf("contains string miss: got %q", got)
	}
	if got := render(t, `{{#contains arr "banana"}}yes{{else}}no{{/contains}}`, ctx); got != "yes" {
		t.Errorf("contains array: got %q", got)
	}
	if got := render(t, `{{#contains arr "grape"}}yes{{else}}no{{/contains}}`, ctx); got != "no" {
		t.Errorf("contains array miss: got %q", got)
	}
}

func TestDefault(t *testing.T) {
	ctx := map[string]interface{}{"val": "hello", "empty": ""}
	if got := render(t, `{{default val "fallback"}}`, ctx); got != "hello" {
		t.Errorf("default truthy: got %q", got)
	}
	if got := render(t, `{{default empty "fallback"}}`, ctx); got != "fallback" {
		t.Errorf("default falsy: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Math helpers
// ---------------------------------------------------------------------------

func TestRoundFloorCeil(t *testing.T) {
	ctx := map[string]interface{}{"n": 3.7}
	if got := render(t, `{{round n}}`, ctx); got != "4" {
		t.Errorf("round: got %q", got)
	}
	if got := render(t, `{{floor n}}`, ctx); got != "3" {
		t.Errorf("floor: got %q", got)
	}
	if got := render(t, `{{ceil n}}`, ctx); got != "4" {
		t.Errorf("ceil: got %q", got)
	}

	ctx = map[string]interface{}{"n": 3.2}
	if got := render(t, `{{round n}}`, ctx); got != "3" {
		t.Errorf("round 3.2: got %q", got)
	}
}

func TestAddSubtract(t *testing.T) {
	ctx := map[string]interface{}{"a": 10, "b": 3.5}
	if got := render(t, `{{add a b}}`, ctx); got != "13.5" {
		t.Errorf("add: got %q", got)
	}
	if got := render(t, `{{plus a b}}`, ctx); got != "13.5" {
		t.Errorf("plus: got %q", got)
	}
	if got := render(t, `{{subtract a b}}`, ctx); got != "6.5" {
		t.Errorf("subtract: got %q", got)
	}
	if got := render(t, `{{minus a b}}`, ctx); got != "6.5" {
		t.Errorf("minus: got %q", got)
	}
}

func TestMultiplyDivide(t *testing.T) {
	ctx := map[string]interface{}{"a": 6, "b": 3}
	if got := render(t, `{{multiply a b}}`, ctx); got != "18" {
		t.Errorf("multiply: got %q", got)
	}
	if got := render(t, `{{divide a b}}`, ctx); got != "2" {
		t.Errorf("divide: got %q", got)
	}
	// Divide by zero
	if got := render(t, `{{divide a 0}}`, ctx); got != "0" {
		t.Errorf("divide by zero: got %q", got)
	}
}

func TestToFixed(t *testing.T) {
	ctx := map[string]interface{}{"n": 3.14159}
	if got := render(t, `{{toFixed n 2}}`, ctx); got != "3.14" {
		t.Errorf("toFixed 2: got %q", got)
	}
	if got := render(t, `{{toFixed n 0}}`, ctx); got != "3" {
		t.Errorf("toFixed 0: got %q", got)
	}
	if got := render(t, `{{toFixed n 4}}`, ctx); got != "3.1416" {
		t.Errorf("toFixed 4: got %q", got)
	}
}

func TestToInt(t *testing.T) {
	ctx := map[string]interface{}{"n": 3.7, "s": "42"}
	if got := render(t, `{{toInt n}}`, ctx); got != "3" {
		t.Errorf("toInt float: got %q", got)
	}
	if got := render(t, `{{toInt s}}`, ctx); got != "42" {
		t.Errorf("toInt string: got %q", got)
	}
}

func TestMathWithNil(t *testing.T) {
	ctx := map[string]interface{}{}
	// Missing vars are nil → should produce 0
	if got := render(t, `{{add missing 5}}`, ctx); got != "5" {
		t.Errorf("add nil: got %q", got)
	}
	if got := render(t, `{{round missing}}`, ctx); got != "0" {
		t.Errorf("round nil: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// String helpers
// ---------------------------------------------------------------------------

func TestUppercaseLowercase(t *testing.T) {
	ctx := map[string]interface{}{"s": "Hello World"}
	if got := render(t, `{{uppercase s}}`, ctx); got != "HELLO WORLD" {
		t.Errorf("uppercase: got %q", got)
	}
	if got := render(t, `{{lowercase s}}`, ctx); got != "hello world" {
		t.Errorf("lowercase: got %q", got)
	}
}

func TestCapitalize(t *testing.T) {
	ctx := map[string]interface{}{"s": "hello"}
	if got := render(t, `{{capitalize s}}`, ctx); got != "Hello" {
		t.Errorf("capitalize: got %q", got)
	}

	// Unicode
	ctx = map[string]interface{}{"s": "über"}
	if got := render(t, `{{capitalize s}}`, ctx); got != "Über" {
		t.Errorf("capitalize unicode: got %q", got)
	}

	// Empty string
	ctx = map[string]interface{}{"s": ""}
	if got := render(t, `{{capitalize s}}`, ctx); got != "" {
		t.Errorf("capitalize empty: got %q", got)
	}
}

func TestReplace(t *testing.T) {
	ctx := map[string]interface{}{"s": "foo bar foo"}
	if got := render(t, `{{replace s "foo" "baz"}}`, ctx); got != "baz bar baz" {
		t.Errorf("replace: got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	ctx := map[string]interface{}{"s": "Hello, World!"}

	// Truncate with default "..." suffix
	if got := render(t, `{{truncate s 8}}`, ctx); got != "Hello..." {
		t.Errorf("truncate: got %q", got)
	}

	// No truncation needed
	if got := render(t, `{{truncate s 50}}`, ctx); got != "Hello, World!" {
		t.Errorf("truncate no-op: got %q", got)
	}

	// Custom suffix via hash
	if got := render(t, `{{truncate s 8 suffix="--"}}`, ctx); got != "Hello,--" {
		t.Errorf("truncate custom suffix: got %q", got)
	}
}

func TestConcat(t *testing.T) {
	ctx := map[string]interface{}{"a": "hello", "b": " ", "c": "world"}
	if got := render(t, `{{concat a b c}}`, ctx); got != "hello world" {
		t.Errorf("concat: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Array helpers
// ---------------------------------------------------------------------------

func TestForEach(t *testing.T) {
	ctx := map[string]interface{}{
		"items": []string{"a", "b", "c"},
	}
	// Basic iteration with @index
	got := render(t, `{{#forEach items}}{{@index}}:{{this}} {{/forEach}}`, ctx)
	if got != "0:a 1:b 2:c " {
		t.Errorf("forEach index: got %q", got)
	}

	// @isFirst and @isLast
	got = render(t, `{{#forEach items}}{{#if @isFirst}}[{{/if}}{{this}}{{#if @isLast}}]{{/if}}{{/forEach}}`, ctx)
	if got != "[abc]" {
		t.Errorf("forEach isFirst/isLast: got %q", got)
	}

	// @total
	got = render(t, `{{#forEach items}}{{@total}}{{/forEach}}`, ctx)
	if got != "333" {
		t.Errorf("forEach total: got %q", got)
	}

	// Empty → inverse
	ctx = map[string]interface{}{"items": []string{}}
	got = render(t, `{{#forEach items}}item{{else}}empty{{/forEach}}`, ctx)
	if got != "empty" {
		t.Errorf("forEach empty: got %q", got)
	}
}

func TestJoin(t *testing.T) {
	ctx := map[string]interface{}{"arr": []string{"a", "b", "c"}}
	if got := render(t, `{{join arr ", "}}`, ctx); got != "a, b, c" {
		t.Errorf("join: got %q", got)
	}
}

func TestLength(t *testing.T) {
	ctx := map[string]interface{}{"arr": []int{1, 2, 3}, "s": "hello"}
	if got := render(t, `{{length arr}}`, ctx); got != "3" {
		t.Errorf("length array: got %q", got)
	}
	if got := render(t, `{{length s}}`, ctx); got != "5" {
		t.Errorf("length string: got %q", got)
	}
}

func TestFirst(t *testing.T) {
	ctx := map[string]interface{}{"arr": []string{"a", "b", "c"}}
	// Default n=1 returns single item
	if got := render(t, `{{first arr}}`, ctx); got != "a" {
		t.Errorf("first default: got %q", got)
	}
}

func TestLast(t *testing.T) {
	ctx := map[string]interface{}{"arr": []string{"a", "b", "c"}}
	if got := render(t, `{{last arr}}`, ctx); got != "c" {
		t.Errorf("last default: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

func TestNumberFormat(t *testing.T) {
	ctx := map[string]interface{}{"n": 1234.5}
	// 2 decimals
	if got := render(t, `{{numberFormat n 2}}`, ctx); got != "1234.50" {
		t.Errorf("numberFormat 2: got %q", got)
	}
	// 0 decimals
	if got := render(t, `{{numberFormat n 0}}`, ctx); got != "1234" {
		t.Errorf("numberFormat 0: got %q", got)
	}
	// 4 decimals
	if got := render(t, `{{numberFormat n 4}}`, ctx); got != "1234.5000" {
		t.Errorf("numberFormat 4: got %q", got)
	}
}

func TestPad0(t *testing.T) {
	ctx := map[string]interface{}{"n": 7}
	// Width 3
	if got := render(t, `{{pad0 n 3}}`, ctx); got != "007" {
		t.Errorf("pad0 3: got %q", got)
	}
	// Width 5
	if got := render(t, `{{pad0 n 5}}`, ctx); got != "00007" {
		t.Errorf("pad0 5: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// toFloat unit tests
// ---------------------------------------------------------------------------

func TestToFloat(t *testing.T) {
	tests := []struct {
		in   interface{}
		want float64
	}{
		{nil, 0},
		{42, 42},
		{3.14, 3.14},
		{"2.5", 2.5},
		{"abc", 0},
		{true, 1},
		{false, 0},
		{uint(10), 10},
	}
	for _, tt := range tests {
		got := toFloat(tt.in)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("toFloat(%v): got %f, want %f", tt.in, got, tt.want)
		}
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		in   interface{}
		want bool
	}{
		{nil, false},
		{false, false},
		{0, false},
		{"", false},
		{true, true},
		{1, true},
		{"hello", true},
		{[]int{1}, true},
		{[]int{}, false},
	}
	for _, tt := range tests {
		got := toBool(tt.in)
		if got != tt.want {
			t.Errorf("toBool(%v): got %v, want %v", tt.in, got, tt.want)
		}
	}
}
