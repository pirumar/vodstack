package video

import (
	"fmt"
	"strings"
)

// ChaptersVTT renders chapters as a WebVTT chapters track. Each cue runs from its
// start to the next chapter's start (last → video duration). Shared by the admin
// chapter editor and the LLM auto-chapters worker.
func ChaptersVTT(chapters []Chapter, duration int) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, c := range chapters {
		end := duration
		if i+1 < len(chapters) {
			end = chapters[i+1].Start
		}
		if end <= c.Start {
			end = c.Start + 1
		}
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", vttClock(c.Start), vttClock(end), c.Title)
	}
	return b.String()
}

func vttClock(sec int) string {
	return fmt.Sprintf("%02d:%02d:%02d.000", sec/3600, (sec%3600)/60, sec%60)
}
