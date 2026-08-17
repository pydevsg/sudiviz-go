package fix

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSelection parses a fix-number spec ("1", "1,3", "1-3") into 1-based
// indices. An empty spec means "all" (nil map). n is the number of actions.
func ParseSelection(spec string, n int) (map[int]bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	selected := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			a, b, ok := strings.Cut(part, "-")
			if !ok {
				return nil, fmt.Errorf("invalid fix number format: %q", spec)
			}
			start, err1 := strconv.Atoi(strings.TrimSpace(a))
			end, err2 := strconv.Atoi(strings.TrimSpace(b))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("invalid fix number format: %q", spec)
			}
			for i := start; i <= end; i++ {
				selected[i] = true
			}
			continue
		}
		i, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid fix number format: %q", spec)
		}
		selected[i] = true
	}
	var invalid []int
	for i := range selected {
		if i < 1 || i > n {
			invalid = append(invalid, i)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid fix number(s): %v. Valid range: 1-%d", invalid, n)
	}
	return selected, nil
}
