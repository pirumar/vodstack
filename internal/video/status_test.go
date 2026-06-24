package video

import "testing"

func TestStatusIsReady(t *testing.T) {
	ready := map[Status]bool{
		StatusCreated:     false,
		StatusUploaded:    false,
		StatusProcessing:  false,
		StatusTranscoding: true, // matches Bunny: 3 || 4 are playable
		StatusFinished:    true,
		StatusFailed:      false,
	}
	for s, want := range ready {
		if s.IsReady() != want {
			t.Errorf("%s.IsReady() = %v, want %v", s, s.IsReady(), want)
		}
	}
}

func TestStatusString(t *testing.T) {
	if StatusFinished.String() != "finished" {
		t.Errorf("got %q", StatusFinished.String())
	}
	if Status(99).String() != "unknown" {
		t.Errorf("got %q", Status(99).String())
	}
}
