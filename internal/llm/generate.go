package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pirumar/vodstack/internal/search"
	"github.com/pirumar/vodstack/internal/video"
)

// maxTranscriptChars caps how much transcript we feed the model (keeps the prompt
// within context limits; the tail of a long lesson rarely changes the summary).
const maxTranscriptChars = 12000

// Summary returns a short Turkish description generated from the transcript.
func (c *Client) Summary(ctx context.Context, transcript string) (string, error) {
	system := "Sen eğitim videoları için kısa, bilgilendirici açıklamalar yazan bir asistansın. Sadece açıklamayı döndür, başka bir şey ekleme."
	user := "Aşağıdaki ders transkriptine dayanarak Türkçe, 2-4 cümlelik bir açıklama yaz.\n\nTranskript:\n" + truncate(transcript, maxTranscriptChars)
	out, err := c.Chat(ctx, system, user)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Tags returns content keywords/tags generated from the transcript.
func (c *Client) Tags(ctx context.Context, transcript string) ([]string, error) {
	system := "Sen video içeriğinden anahtar kelime/etiket çıkaran bir asistansın. SADECE bir JSON dizi döndür, örnek: [\"cebir\",\"denklem\"]. Başka metin ekleme."
	user := "Aşağıdaki ders transkriptine göre 5-10 adet Türkçe etiket üret.\n\nTranskript:\n" + truncate(transcript, maxTranscriptChars)
	out, err := c.Chat(ctx, system, user)
	if err != nil {
		return nil, err
	}
	var tags []string
	if err := json.Unmarshal([]byte(extractJSON(out, '[', ']')), &tags); err != nil {
		return nil, fmt.Errorf("parse tags json: %w (raw: %.120q)", err, out)
	}
	// Trim, drop empties, dedupe.
	seen := make(map[string]bool)
	cleaned := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		cleaned = append(cleaned, t)
	}
	return cleaned, nil
}

// Chapters returns YouTube-style chapter markers generated from timestamped cues.
// duration (seconds, 0 if unknown) clamps the returned starts.
func (c *Client) Chapters(ctx context.Context, cues []search.Cue, duration int) ([]video.Chapter, error) {
	var b strings.Builder
	for _, cu := range cues {
		fmt.Fprintf(&b, "[%d] %s\n", int(cu.Start), cu.Text)
	}
	system := "Sen bir videoyu mantıklı bölümlere ayıran bir asistansın. SADECE JSON döndür: [{\"start\":0,\"title\":\"...\"}]. start tam sayı saniyedir, title kısa Türkçe başlıktır. İlk bölüm 0'dan başlamalı. Başka metin ekleme."
	user := "Aşağıda her satır [saniye] metin biçiminde bir ders transkripti var. 3-8 bölüme ayır.\n\n" + truncate(b.String(), maxTranscriptChars)
	out, err := c.Chat(ctx, system, user)
	if err != nil {
		return nil, err
	}
	var raw []video.Chapter
	if err := json.Unmarshal([]byte(extractJSON(out, '[', ']')), &raw); err != nil {
		return nil, fmt.Errorf("parse chapters json: %w (raw: %.120q)", err, out)
	}
	cleaned := make([]video.Chapter, 0, len(raw))
	for _, ch := range raw {
		title := strings.TrimSpace(ch.Title)
		if title == "" || ch.Start < 0 {
			continue
		}
		if duration > 0 && ch.Start > duration {
			continue
		}
		cleaned = append(cleaned, video.Chapter{Start: ch.Start, Title: title})
	}
	sort.SliceStable(cleaned, func(i, j int) bool { return cleaned[i].Start < cleaned[j].Start })
	return cleaned, nil
}

// extractJSON returns the substring from the first `open` to the last `close`
// rune, so we tolerate models that wrap JSON in prose or ```json fences.
func extractJSON(s string, open, close byte) string {
	i := strings.IndexByte(s, open)
	j := strings.LastIndexByte(s, close)
	if i < 0 || j < 0 || j < i {
		return s
	}
	return s[i : j+1]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
