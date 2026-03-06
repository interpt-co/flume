package pattern

// Matches returns true if all matchLabels are present in podLabels with the
// same values. An empty or nil matchLabels selector matches everything.
func (s Selector) Matches(podLabels map[string]string) bool {
	for k, v := range s.MatchLabels {
		if podLabels[k] != v {
			return false
		}
	}
	return true
}

// MatchingPatterns returns all patterns whose selectors match the given pod labels.
func MatchingPatterns(patterns []PatternDef, podLabels map[string]string) []PatternDef {
	var matched []PatternDef
	for _, p := range patterns {
		if p.Selector.Matches(podLabels) {
			matched = append(matched, p)
		}
	}
	return matched
}
