package lyrics

type LyricsValidation struct {
	CoherenceScore      float64             `json:"coherence_score"`
	ViabilityScore      float64             `json:"viability_score"`
	StructureScore      float64             `json:"structure_score"`
	IsPlausibleSong     bool                `json:"is_plausible_song"`
	CoherenceIssues     []CoherenceIssue    `json:"coherence_issues"`
	StructureAnalysis   StructureAnalysis   `json:"structure_analysis"`
	SemanticConsistency SemanticConsistency `json:"semantic_consistency"`
	SuspectedErrors     []SuspectedError    `json:"suspected_errors"`
	CleanedLyrics       string              `json:"cleaned_lyrics"`
	Confidence          float64             `json:"confidence"`
}

type CoherenceIssue struct {
	Line  string `json:"line"`
	Issue string `json:"issue"`
}

type StructureAnalysis struct {
	HasRepetition       bool   `json:"has_repetition"`
	HasChorusPattern    bool   `json:"has_chorus_pattern"`
	StructureConsistent bool   `json:"structure_consistent"`
	Notes               string `json:"notes"`
}

type SemanticConsistency struct {
	HasTheme         bool     `json:"has_theme"`
	ThemeDescription string   `json:"theme_description"`
	Contradictions   []string `json:"contradictions"`
}

type SuspectedError struct {
	Original   string `json:"original"`
	Suggestion string `json:"suggestion"`
	Reason     string `json:"reason"`
}
