package model

import "testing"

func TestParseAttributes(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		wantErr bool
		has     []string
		missing []string
	}{
		{
			name:    "intensity only",
			input:   []string{"intensity"},
			has:     []string{AttrIntensity},
			missing: []string{AttrClassification, AttrReturnNumber, AttrNumberOfReturns},
		},
		{
			name:    "classification only",
			input:   []string{"classification"},
			has:     []string{AttrClassification},
			missing: []string{AttrIntensity, AttrReturnNumber, AttrNumberOfReturns},
		},
		{
			name:    "return_number",
			input:   []string{"return_number"},
			has:     []string{AttrReturnNumber},
			missing: []string{AttrIntensity, AttrClassification, AttrNumberOfReturns},
		},
		{
			name:    "number_of_returns",
			input:   []string{"number_of_returns"},
			has:     []string{AttrNumberOfReturns},
			missing: []string{AttrIntensity, AttrClassification, AttrReturnNumber},
		},
		{
			name:  "all four attributes",
			input: []string{"intensity", "classification", "return_number", "number_of_returns"},
			has:   []string{AttrIntensity, AttrClassification, AttrReturnNumber, AttrNumberOfReturns},
		},
		{
			name:    "none keyword returns empty set",
			input:   []string{"none"},
			missing: []string{AttrIntensity, AttrClassification, AttrReturnNumber, AttrNumberOfReturns},
		},
		{
			name:  "arbitrary generic attribute is accepted",
			input: []string{"bogus"},
			has:   []string{"bogus"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAttributes(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, attr := range tc.has {
				if !got.Has(attr) {
					t.Errorf("expected attribute %q to be present", attr)
				}
			}
			for _, attr := range tc.missing {
				if got.Has(attr) {
					t.Errorf("expected attribute %q to be absent", attr)
				}
			}
		})
	}
}

func TestDefaultAttributes(t *testing.T) {
	d := DefaultAttributes()
	if !d.Has(AttrIntensity) {
		t.Errorf("default attrs should include intensity")
	}
	if !d.Has(AttrClassification) {
		t.Errorf("default attrs should include classification")
	}
	if d.Has(AttrReturnNumber) {
		t.Errorf("default attrs should NOT include return_number")
	}
	if d.Has(AttrNumberOfReturns) {
		t.Errorf("default attrs should NOT include number_of_returns")
	}
}
