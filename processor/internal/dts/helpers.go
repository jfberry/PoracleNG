package dts

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	raymond "github.com/mailgun/raymond/v2"
)

var helpersOnce sync.Once

// RegisterHelpers registers all custom Handlebars helpers globally.
// It is safe to call multiple times; registration happens only once.
func RegisterHelpers() {
	helpersOnce.Do(func() {
		// Disable HTML escaping — DTS output is JSON for Discord/Telegram, not HTML.
		// Users can explicitly escape with the {{escape}} helper if needed.
		raymond.EscapeFunc = func(s string) string { return s }

		registerComparisonHelpers()
		registerMathHelpers()
		registerStringHelpers()
		registerArrayHelpers()
		registerFormattingHelpers()

		// escape — explicitly HTML-escape a value (for building URLs with user content)
		raymond.RegisterHelper("escape", func(s string) raymond.SafeString {
			return raymond.SafeString(raymond.DefaultEscape(s))
		})
	})
}

// ---------------------------------------------------------------------------
// Type coercion utilities
// ---------------------------------------------------------------------------

// toFloat converts an interface{} to float64.
// Handles int (all widths), uint (all widths), float32/64, string, bool, nil.
// boolResult handles comparison helpers that work both as block helpers
// ({{#eq a b}}X{{else}}Y{{/eq}}) and as subexpressions ({{#if (eq a b)}}).
// In subexpression mode, returns a boolean for the outer helper to evaluate.
// In block mode, renders Fn() or Inverse().
func boolResult(result bool, options *raymond.Options) interface{} {
	if options.IsSubExpression() {
		return result
	}
	if result {
		return options.Fn()
	}
	return options.Inverse()
}

func toFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(val.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(val.Uint())
	case reflect.Float32, reflect.Float64:
		return val.Float()
	case reflect.Bool:
		if val.Bool() {
			return 1
		}
		return 0
	case reflect.String:
		f, err := strconv.ParseFloat(val.String(), 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

// toBool returns Handlebars truthiness: 0, "", nil, false, empty array/map → false.
func toBool(v interface{}) bool {
	return raymond.IsTrue(v)
}

// toString converts any value to its string representation.
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// ---------------------------------------------------------------------------
// Comparison helpers (block helpers)
// ---------------------------------------------------------------------------

func registerComparisonHelpers() {
	// Comparison helpers work in two modes:
	// 1. Block mode: {{#eq level 8}}...{{else}}...{{/eq}} — renders Fn() or Inverse()
	// 2. Subexpression mode: {{#if (eq level 8)}} — returns boolean for outer helper
	//
	// We detect subexpression mode by checking if the helper has a block body.
	// When options.FnBody() is empty, we're in subexpression mode and return bool.

	// eq — true if a == b (normalized via Sprintf)
	raymond.RegisterHelper("eq", func(a, b interface{}, options *raymond.Options) interface{} {
		result := fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
		return boolResult(result, options)
	})

	// ne — true if a != b
	raymond.RegisterHelper("ne", func(a, b interface{}, options *raymond.Options) interface{} {
		result := fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b)
		return boolResult(result, options)
	})

	// isnt — alias for ne
	raymond.RegisterHelper("isnt", func(a, b interface{}, options *raymond.Options) interface{} {
		result := fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b)
		return boolResult(result, options)
	})

	// compare — supports ==, !=, <, >, <=, >=
	raymond.RegisterHelper("compare", func(a interface{}, op string, b interface{}, options *raymond.Options) interface{} {
		result := evalCompare(a, op, b)
		return boolResult(result, options)
	})

	// gt — a > b (numeric)
	raymond.RegisterHelper("gt", func(a, b interface{}, options *raymond.Options) interface{} {
		return boolResult(toFloat(a) > toFloat(b), options)
	})

	// gte — a >= b (numeric)
	raymond.RegisterHelper("gte", func(a, b interface{}, options *raymond.Options) interface{} {
		return boolResult(toFloat(a) >= toFloat(b), options)
	})

	// lt — a < b (numeric)
	raymond.RegisterHelper("lt", func(a, b interface{}, options *raymond.Options) interface{} {
		return boolResult(toFloat(a) < toFloat(b), options)
	})

	// lte — a <= b (numeric)
	raymond.RegisterHelper("lte", func(a, b interface{}, options *raymond.Options) interface{} {
		return boolResult(toFloat(a) <= toFloat(b), options)
	})

	// and — all args truthy (variadic)
	raymond.RegisterHelper("and", func(options *raymond.Options) interface{} {
		result := true
		for _, p := range options.Params() {
			if !toBool(p) {
				result = false
				break
			}
		}
		return boolResult(result, options)
	})

	// or — any arg truthy (variadic)
	raymond.RegisterHelper("or", func(options *raymond.Options) interface{} {
		result := false
		for _, p := range options.Params() {
			if toBool(p) {
				result = true
				break
			}
		}
		return boolResult(result, options)
	})

	// not — logical negation (block helper)
	raymond.RegisterHelper("not", func(value interface{}, options *raymond.Options) interface{} {
		if !toBool(value) {
			return options.Fn()
		}
		return options.Inverse()
	})

	// contains — string contains or slice includes
	raymond.RegisterHelper("contains", func(collection, value interface{}, options *raymond.Options) interface{} {
		if evalContains(collection, value) {
			return options.Fn()
		}
		return options.Inverse()
	})

	// default — inline helper: return value if truthy, else defaultValue
	raymond.RegisterHelper("default", func(value, defaultValue interface{}) interface{} {
		if toBool(value) {
			return toString(value)
		}
		return toString(defaultValue)
	})
}

// evalCompare evaluates a comparison with the given operator.
func evalCompare(a interface{}, op string, b interface{}) bool {
	switch op {
	case "==":
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	case "!=":
		return fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b)
	case "<":
		return toFloat(a) < toFloat(b)
	case ">":
		return toFloat(a) > toFloat(b)
	case "<=":
		return toFloat(a) <= toFloat(b)
	case ">=":
		return toFloat(a) >= toFloat(b)
	default:
		return false
	}
}

// evalContains checks if collection contains value.
func evalContains(collection, value interface{}) bool {
	if collection == nil {
		return false
	}
	// String contains
	colStr, colIsStr := collection.(string)
	if colIsStr {
		return strings.Contains(colStr, toString(value))
	}
	// Slice/array contains
	val := reflect.ValueOf(collection)
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		needle := toString(value)
		for i := 0; i < val.Len(); i++ {
			if toString(val.Index(i).Interface()) == needle {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Math helpers (inline)
// ---------------------------------------------------------------------------

func registerMathHelpers() {
	raymond.RegisterHelper("round", func(n interface{}) interface{} {
		return math.Round(toFloat(n))
	})

	raymond.RegisterHelper("floor", func(n interface{}) interface{} {
		return math.Floor(toFloat(n))
	})

	raymond.RegisterHelper("ceil", func(n interface{}) interface{} {
		return math.Ceil(toFloat(n))
	})

	raymond.RegisterHelper("add", func(a, b interface{}) interface{} {
		return toFloat(a) + toFloat(b)
	})

	raymond.RegisterHelper("plus", func(a, b interface{}) interface{} {
		return toFloat(a) + toFloat(b)
	})

	raymond.RegisterHelper("subtract", func(a, b interface{}) interface{} {
		return toFloat(a) - toFloat(b)
	})

	raymond.RegisterHelper("minus", func(a, b interface{}) interface{} {
		return toFloat(a) - toFloat(b)
	})

	raymond.RegisterHelper("multiply", func(a, b interface{}) interface{} {
		return toFloat(a) * toFloat(b)
	})

	raymond.RegisterHelper("divide", func(a, b interface{}) interface{} {
		bv := toFloat(b)
		if bv == 0 {
			return float64(0)
		}
		return toFloat(a) / bv
	})

	raymond.RegisterHelper("toFixed", func(n, decimals interface{}) interface{} {
		d := int(toFloat(decimals))
		return strconv.FormatFloat(toFloat(n), 'f', d, 64)
	})

	raymond.RegisterHelper("toInt", func(n interface{}) interface{} {
		return int(toFloat(n))
	})
}

// ---------------------------------------------------------------------------
// String helpers (inline)
// ---------------------------------------------------------------------------

func registerStringHelpers() {
	raymond.RegisterHelper("uppercase", func(s interface{}) interface{} {
		return strings.ToUpper(toString(s))
	})

	raymond.RegisterHelper("lowercase", func(s interface{}) interface{} {
		return strings.ToLower(toString(s))
	})

	raymond.RegisterHelper("capitalize", func(s interface{}) interface{} {
		str := toString(s)
		if str == "" {
			return ""
		}
		r, size := utf8.DecodeRuneInString(str)
		return string(unicode.ToUpper(r)) + str[size:]
	})

	raymond.RegisterHelper("replace", func(s, old, new interface{}) interface{} {
		return strings.ReplaceAll(toString(s), toString(old), toString(new))
	})

	// truncate — truncate with suffix. Usage: {{truncate s 8}} or {{truncate s 8 suffix="--"}}
	raymond.RegisterHelper("truncate", func(s, length interface{}, options *raymond.Options) interface{} {
		str := toString(s)
		maxLen := int(toFloat(length))
		sfx := "..."
		if h := options.HashProp("suffix"); h != nil {
			sfx = toString(h)
		}
		if len(str) <= maxLen {
			return str
		}
		if maxLen <= len(sfx) {
			return sfx[:maxLen]
		}
		return str[:maxLen-len(sfx)] + sfx
	})

	raymond.RegisterHelper("concat", func(args ...interface{}) interface{} {
		var sb strings.Builder
		for _, a := range args {
			sb.WriteString(toString(a))
		}
		return sb.String()
	})
}

// ---------------------------------------------------------------------------
// Array helpers
// ---------------------------------------------------------------------------

// eachContextWithMeta injects isFirst/isLast into the iteration context.
// For map contexts: adds the keys directly. For non-map contexts: wraps in
// an eachWrapper that implements FieldResolver so {{this}} still works.
func eachContextWithMeta(ctx interface{}, index, length int) interface{} {
	isFirst := index == 0
	isLast := index == length-1

	// If ctx is a map, inject directly
	if m, ok := ctx.(map[string]any); ok {
		m["isFirst"] = isFirst
		m["isLast"] = isLast
		return m
	}
	if m, ok := ctx.(map[string]interface{}); ok {
		m["isFirst"] = isFirst
		m["isLast"] = isLast
		return m
	}

	// Wrap non-map contexts so isFirst/isLast are accessible alongside {{this}}
	return &eachWrapper{value: ctx, isFirst: isFirst, isLast: isLast}
}

// eachWrapper implements raymond.FieldResolver to provide isFirst/isLast
// while preserving the original value for {{this}}.
type eachWrapper struct {
	value   interface{}
	isFirst bool
	isLast  bool
}

func (w *eachWrapper) GetField(name string) (interface{}, bool) {
	switch name {
	case "isFirst":
		return w.isFirst, true
	case "isLast":
		return w.isLast, true
	}
	return nil, false
}

// String implements fmt.Stringer so {{this}} renders the wrapped value.
func (w *eachWrapper) String() string {
	return fmt.Sprintf("%v", w.value)
}

func registerArrayHelpers() {
	// forEach — block helper with @isFirst, @isLast, @total
	// Override built-in each to inject isFirst/isLast into context for PoracleJS compat.
	// Standard Handlebars provides @first/@last (data variables) but PoracleJS templates
	// use isFirst/isLast as context properties (bare names without @).
	raymond.RemoveHelper("each")
	raymond.RegisterHelper("each", func(context interface{}, options *raymond.Options) interface{} {
		if !raymond.IsTrue(context) {
			return options.Inverse()
		}

		val := reflect.ValueOf(context)
		switch val.Kind() {
		case reflect.Slice, reflect.Array:
			length := val.Len()
			if length == 0 {
				return options.Inverse()
			}
			var sb strings.Builder
			for i := 0; i < length; i++ {
				data := options.NewDataFrame()
				data.Set("index", i)
				data.Set("first", i == 0)
				data.Set("last", i == length-1)

				ctx := eachContextWithMeta(val.Index(i).Interface(), i, length)
				sb.WriteString(options.FnCtxData(ctx, data))
			}
			return sb.String()
		case reflect.Map:
			keys := val.MapKeys()
			length := len(keys)
			if length == 0 {
				return options.Inverse()
			}
			var sb strings.Builder
			for i, key := range keys {
				data := options.NewDataFrame()
				data.Set("index", i)
				data.Set("key", key.Interface())
				data.Set("first", i == 0)
				data.Set("last", i == length-1)

				ctx := eachContextWithMeta(val.MapIndex(key).Interface(), i, length)
				sb.WriteString(options.FnCtxData(ctx, data))
			}
			return sb.String()
		default:
			return options.Inverse()
		}
	})

	raymond.RegisterHelper("forEach", func(context interface{}, options *raymond.Options) interface{} {
		if !raymond.IsTrue(context) {
			return options.Inverse()
		}

		val := reflect.ValueOf(context)
		if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
			return options.Inverse()
		}

		length := val.Len()
		if length == 0 {
			return options.Inverse()
		}

		var sb strings.Builder
		for i := 0; i < length; i++ {
			data := options.NewDataFrame()
			data.Set("index", i)
			data.Set("first", i == 0)
			data.Set("last", i == length-1)
			data.Set("total", length)

			ctx := eachContextWithMeta(val.Index(i).Interface(), i, length)
			sb.WriteString(options.FnCtxData(ctx, data))
		}
		return sb.String()
	})

	// first — inline, first N elements. Usage: {{first arr}} or {{first arr n=2}}
	raymond.RegisterHelper("first", func(arr interface{}, options *raymond.Options) interface{} {
		if arr == nil {
			return ""
		}
		val := reflect.ValueOf(arr)
		if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
			return ""
		}
		if val.Len() == 0 {
			return ""
		}
		n := 1
		if h := options.HashProp("n"); h != nil {
			n = int(toFloat(h))
		}
		if n <= 0 {
			return ""
		}
		if n >= val.Len() {
			n = val.Len()
		}
		if n == 1 {
			return val.Index(0).Interface()
		}
		result := make([]interface{}, n)
		for i := 0; i < n; i++ {
			result[i] = val.Index(i).Interface()
		}
		return result
	})

	// last — inline, last N elements. Usage: {{last arr}} or {{last arr n=2}}
	raymond.RegisterHelper("last", func(arr interface{}, options *raymond.Options) interface{} {
		if arr == nil {
			return ""
		}
		val := reflect.ValueOf(arr)
		if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
			return ""
		}
		if val.Len() == 0 {
			return ""
		}
		n := 1
		if h := options.HashProp("n"); h != nil {
			n = int(toFloat(h))
		}
		if n <= 0 {
			return ""
		}
		if n >= val.Len() {
			n = val.Len()
		}
		start := val.Len() - n
		if n == 1 {
			return val.Index(start).Interface()
		}
		result := make([]interface{}, n)
		for i := 0; i < n; i++ {
			result[i] = val.Index(start + i).Interface()
		}
		return result
	})

	// length — inline, works on arrays and strings
	raymond.RegisterHelper("length", func(v interface{}) interface{} {
		if v == nil {
			return 0
		}
		val := reflect.ValueOf(v)
		switch val.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
			return val.Len()
		default:
			return 0
		}
	})

	// join — inline, joins array elements with separator
	raymond.RegisterHelper("join", func(arr interface{}, sep interface{}) interface{} {
		if arr == nil {
			return ""
		}
		val := reflect.ValueOf(arr)
		if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
			return toString(arr)
		}
		parts := make([]string, val.Len())
		for i := 0; i < val.Len(); i++ {
			parts[i] = toString(val.Index(i).Interface())
		}
		return strings.Join(parts, toString(sep))
	})
}

// ---------------------------------------------------------------------------
// Formatting helpers (inline)
// ---------------------------------------------------------------------------

func registerFormattingHelpers() {
	// numberFormat — format with N decimal places. Usage: {{numberFormat n 2}}
	raymond.RegisterHelper("numberFormat", func(value, decimals interface{}) interface{} {
		d := int(toFloat(decimals))
		return strconv.FormatFloat(toFloat(value), 'f', d, 64)
	})

	// pad0 — zero-pad to width characters. Usage: {{pad0 n 3}}
	raymond.RegisterHelper("pad0", func(value, width interface{}) interface{} {
		w := int(toFloat(width))
		if w <= 0 {
			w = 3
		}
		return fmt.Sprintf("%0*d", w, int(toFloat(value)))
	})

	// replaceFirst — replace first occurrence only. Usage: {{replaceFirst "Mr. Mime" ". " "_"}} → "Mr_Mime"
	raymond.RegisterHelper("replaceFirst", func(s, old, new interface{}) interface{} {
		return strings.Replace(toString(s), toString(old), toString(new), 1)
	})

	// addCommas — format number with thousand separators. Usage: {{addCommas 12345}} → "12,345"
	raymond.RegisterHelper("addCommas", func(value interface{}) interface{} {
		n := int64(toFloat(value))
		if n == 0 {
			return "0"
		}
		sign := ""
		if n < 0 {
			sign = "-"
			n = -n
		}
		s := strconv.FormatInt(n, 10)
		// Insert commas from right
		var result []byte
		for i, c := range s {
			if i > 0 && (len(s)-i)%3 == 0 {
				result = append(result, ',')
			}
			result = append(result, byte(c))
		}
		return sign + string(result)
	})
}
