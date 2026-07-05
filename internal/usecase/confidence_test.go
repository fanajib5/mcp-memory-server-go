package usecase

import "testing"

func TestClampConfidence(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0.0, 0},
		{0.5, 0.5},
		{1.0, 1},
		{1.5, 1},
		{-0.3, 0},
		{0.75, 0.75},
	}
	for _, c := range cases {
		if got := clampConfidence(c.in); got != c.want {
			t.Errorf("clampConfidence(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestClampConfidencesInPlace(t *testing.T) {
	in := []float64{0.2, 1.7, -0.4, 0.9}
	got := clampConfidences(in)
	want := []float64{0.2, 1.0, 0.0, 0.9}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("clampConfidences[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if in[1] != 1.0 {
		t.Errorf("clampConfidences must mutate in place; in[1]=%v", in[1])
	}
}

func TestClampConfidencesNil(t *testing.T) {
	if got := clampConfidences(nil); got != nil {
		t.Errorf("clampConfidences(nil) = %v, want nil", got)
	}
}
