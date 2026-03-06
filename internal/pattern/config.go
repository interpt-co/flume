package pattern

// PatternDef defines a single named pattern with a label selector.
type PatternDef struct {
	Name     string   `yaml:"name" json:"name"`
	Selector Selector `yaml:"selector" json:"selector"`
}

// Selector holds the label matching criteria for a pattern.
type Selector struct {
	MatchLabels map[string]string `yaml:"matchLabels" json:"matchLabels"`
}
