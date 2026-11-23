package search

import (
	"testing"
)

func TestSuggestTrialUnderNeverIncreases(t *testing.T) {
	uRef := []float64{0.5, 0.6, 0.4, 0.55, 0.7}
	for seed := int64(0); seed < 50; seed++ {
		s := NewState(uRef, 1, seed, TrialUnder)
		for i := 0; i < 5; i++ {
			cand := s.SuggestTrial()
			if i > 0 {
				if cand[0] != s.UCandidate[0] || cand[1] != s.UCandidate[1] {
					t.Fatalf("under trial should freeze after first proposal seed=%d", seed)
				}
			}
			for j, u := range uRef {
				if cand[j] > u+1e-9 {
					t.Fatalf("group %d increased: %f -> %f seed=%d", j, u, cand[j], seed)
				}
			}
		}
	}
}

func TestSuggestTrialOverCanIncrease(t *testing.T) {
	uRef := []float64{0.3, 0.3}
	s := NewState(uRef, 1, 1, TrialOver)
	raised := false
	for i := 0; i < 20; i++ {
		cand := s.SuggestTrial()
		for j, u := range uRef {
			if cand[j] > u {
				raised = true
			}
		}
	}
	if !raised {
		t.Fatal("over mode should sometimes raise duties")
	}
}

func TestShouldCommitUnder(t *testing.T) {
	uRef := []float64{0.5, 0.5}
	cand := []float64{0.38, 0.38}
	if !ShouldCommitUnder(uRef, cand, 0.10, 0, false) {
		t.Fatal("expected commit")
	}
	if ShouldCommitUnder(uRef, cand, 0.10, 0.1, false) {
		t.Fatal("overshoot blocks commit")
	}
	if ShouldCommitUnder(uRef, cand, 0.10, 0, true) {
		t.Fatal("miss blocks commit")
	}
	high := []float64{0.52, 0.38}
	if ShouldCommitUnder(uRef, high, 0.10, 0, false) {
		t.Fatal("duty above ref blocks commit")
	}
}

func TestDutiesNotAboveRef(t *testing.T) {
	uRef := []float64{0.5, 0.6}
	if !DutiesNotAboveRef(uRef, []float64{0.5, 0.6}) {
		t.Fatal("equal should pass")
	}
	if DutiesNotAboveRef(uRef, []float64{0.51, 0.6}) {
		t.Fatal("above ref should fail")
	}
	if DutiesNotAboveRef(uRef, []float64{0.5, 0.59}) {
		t.Fatal("below ref should pass")
	}
}