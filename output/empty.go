package output

import "reflect"

// DropEmpty returns a copy of v without null members, empty lists, and empty objects, cleaning the
// innermost values first so a member that only held empty values goes too. Strings, numbers, and booleans
// always stay, "", 0, and false included. List entries and v itself are cleaned but never removed, so
// positions and the shape of the root stay stable. A collection keeps its columns: a cell that cleans to
// nothing becomes a missing value. v is not modified.
func DropEmpty(v any) any {
	cleaned, _ := dropEmpty(v)
	return cleaned
}

// dropEmpty also reports whether the cleaned value counts as empty for its parent.
func dropEmpty(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case Object:
		fields := make([]Field, 0, len(x.Fields))
		for _, f := range x.Fields {
			if value, empty := dropEmpty(f.Value); !empty {
				fields = append(fields, Field{Name: f.Name, Value: value})
			}
		}
		return Object{Fields: fields}, len(fields) == 0
	case Collection:
		rows := make([]Row, len(x.Rows))
		for i, row := range x.Rows {
			cleaned := make(Row, len(row))
			for key, value := range row {
				if value, empty := dropEmpty(value); !empty {
					cleaned[key] = value
				}
			}
			rows[i] = cleaned
		}
		return Collection{Columns: x.Columns, Rows: rows}, len(rows) == 0
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		return v, rv.IsNil()
	case reflect.Map:
		if rv.IsNil() {
			return nil, true
		}
		if rv.Type().Key().Kind() != reflect.String {
			return v, rv.Len() == 0
		}
		out := make(map[string]any, rv.Len())
		for iter := rv.MapRange(); iter.Next(); {
			if value, empty := dropEmpty(iter.Value().Interface()); !empty {
				out[iter.Key().String()] = value
			}
		}
		return out, len(out) == 0
	case reflect.Slice, reflect.Array:
		// JSON writes a byte slice as one string, so it is a scalar here, not a list of numbers.
		if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
			return v, rv.IsNil()
		}
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, true
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i], _ = dropEmpty(rv.Index(i).Interface())
		}
		return out, len(out) == 0
	}
	return v, false
}
