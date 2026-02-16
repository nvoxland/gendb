package generator

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nvoxland/gendb/pkg/schema"
)

var varcharLenRegex = regexp.MustCompile(`\((\d+)\)`)

// coerceRow coerces each value in a row map to the appropriate Go type for PostgreSQL insertion.
func coerceRow(row map[string]any, table *schema.Table) error {
	for _, col := range table.Columns {
		v, ok := row[col.Name]
		if !ok {
			continue
		}
		coerced, err := coerceValue(v, col)
		if err != nil {
			return fmt.Errorf("column %s: %w", col.Name, err)
		}
		row[col.Name] = coerced
	}
	return nil
}

// coerceValue coerces a single JSON-parsed value to the appropriate Go type.
func coerceValue(v any, col *schema.Column) (any, error) {
	if v == nil {
		return nil, nil
	}

	dt := strings.ToLower(col.DataType)

	switch {
	case strings.Contains(dt, "int") || strings.Contains(dt, "serial"):
		return toInt64(v)
	case strings.Contains(dt, "bool"):
		return toBool(v)
	case strings.Contains(dt, "numeric") || strings.Contains(dt, "decimal") || strings.Contains(dt, "money"):
		return toFloat64(v)
	case strings.Contains(dt, "float") || strings.Contains(dt, "double") || strings.Contains(dt, "real"):
		return toFloat64(v)
	case strings.Contains(dt, "timestamp") || strings.Contains(dt, "date"):
		return toTime(v)
	case strings.Contains(dt, "uuid"):
		return fmt.Sprintf("%v", v), nil
	case strings.Contains(dt, "json"):
		return fmt.Sprintf("%v", v), nil
	case strings.Contains(dt, "text") || strings.Contains(dt, "varchar") || strings.Contains(dt, "char"):
		s := fmt.Sprintf("%v", v)
		// Truncate to max length for varchar/char types
		if m := varcharLenRegex.FindStringSubmatch(col.DataType); len(m) == 2 {
			if maxLen, err := strconv.Atoi(m[1]); err == nil {
				runes := []rune(s)
				if len(runes) > maxLen {
					s = string(runes[:maxLen])
				}
			}
		}
		return s, nil
	default:
		return v, nil
	}
}

func toInt64(v any) (int64, error) {
	switch val := v.(type) {
	case float64:
		return int64(math.Round(val)), nil
	case int64:
		return val, nil
	case int:
		return int64(val), nil
	case string:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			// Try parsing as float first
			f, ferr := strconv.ParseFloat(val, 64)
			if ferr != nil {
				return 0, fmt.Errorf("cannot convert %q to int", val)
			}
			return int64(math.Round(f)), nil
		}
		return n, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

func toBool(v any) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		return strconv.ParseBool(val)
	case float64:
		return val != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", v)
	}
}

func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int64:
		return float64(val), nil
	case int:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}

func toTime(v any) (time.Time, error) {
	switch val := v.(type) {
	case time.Time:
		return val, nil
	case string:
		// Try common formats
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
			time.RFC3339Nano,
		}
		for _, f := range formats {
			if t, err := time.Parse(f, val); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse %q as timestamp", val)
	default:
		return time.Time{}, fmt.Errorf("cannot convert %T to time", v)
	}
}
