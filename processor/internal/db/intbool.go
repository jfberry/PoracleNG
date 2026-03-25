package db

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

// IntBool is a bool that serializes as 0/1 in JSON (matching the legacy JS API)
// and scans from MySQL TINYINT. This maintains backwards compatibility for API
// consumers that expect integer booleans.
type IntBool bool

func (b IntBool) MarshalJSON() ([]byte, error) {
	if b {
		return []byte("1"), nil
	}
	return []byte("0"), nil
}

func (b *IntBool) UnmarshalJSON(data []byte) error {
	s := string(data)
	switch s {
	case "true":
		*b = true
	case "false", "null":
		*b = false
	default:
		// Handle any integer (0 = false, non-zero = true)
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("intbool: cannot unmarshal %s", s)
		}
		*b = IntBool(n != 0)
	}
	return nil
}

// Scan implements sql.Scanner for MySQL TINYINT / BOOL columns.
func (b *IntBool) Scan(src any) error {
	switch v := src.(type) {
	case bool:
		*b = IntBool(v)
	case int64:
		*b = v != 0
	case []byte:
		*b = len(v) > 0 && v[0] != '0'
	case nil:
		*b = false
	default:
		return fmt.Errorf("intbool: cannot scan %T", src)
	}
	return nil
}

// Value implements driver.Valuer for DB writes.
func (b IntBool) Value() (driver.Value, error) {
	if b {
		return int64(1), nil
	}
	return int64(0), nil
}
