package lyrics_test

import (
	"encoding/json"
	"testing"

	"goscribe/pkg/lyrics"
)

func TestLyricsValidation_JSONRoundTrip(t *testing.T) {
	original := lyrics.LyricsValidation{
		CoherenceScore:  82,
		ViabilityScore:  75,
		StructureScore:  90,
		IsPlausibleSong: true,
		CoherenceIssues: []lyrics.CoherenceIssue{{Line: "line1", Issue: "minor"}},
		StructureAnalysis: lyrics.StructureAnalysis{
			HasRepetition: true, HasChorusPattern: true, StructureConsistent: true, Notes: "ok",
		},
		SemanticConsistency: lyrics.SemanticConsistency{
			HasTheme: true, ThemeDescription: "love", Contradictions: []string{},
		},
		SuspectedErrors: []lyrics.SuspectedError{{Original: "foo", Suggestion: "bar", Reason: "typo"}},
		CleanedLyrics:   "clean text",
		Confidence:      0.91,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got lyrics.LyricsValidation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CoherenceScore != original.CoherenceScore {
		t.Errorf("CoherenceScore: got %v, want %v", got.CoherenceScore, original.CoherenceScore)
	}
	if got.Confidence != original.Confidence {
		t.Errorf("Confidence: got %v, want %v", got.Confidence, original.Confidence)
	}
}

func TestLyricsValidation_FloatScores(t *testing.T) {
	raw := `{"coherence_score": 82.5, "viability_score": 70.0, "confidence": 0.88}`
	var v lyrics.LyricsValidation
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("unmarshal float score: %v", err)
	}
	if v.CoherenceScore != 82.5 {
		t.Errorf("CoherenceScore: got %v, want 82.5", v.CoherenceScore)
	}
}
