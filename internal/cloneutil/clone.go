package cloneutil

func DeepValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return DeepMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, DeepValue(item))
		}
		return out
	default:
		return v
	}
}

func DeepMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = DeepValue(value)
	}
	return out
}
