package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// ripmime (at least the 1.4.x Homebrew build) exits 0 and stays silent on
// garbage, truncated, or even missing input. These cheap checks give the
// user a signal when ripmime would otherwise fail silently.

var (
	headerLine     = regexp.MustCompile(`(?mi)^(From|To|Subject|Date|Received|Content-Type|MIME-Version|Message-ID|Return-Path):`)
	boundaryRe     = regexp.MustCompile(`(?i)boundary\s*=\s*"?([^";\r\n]+)"?`)
	attachmentDisp = regexp.MustCompile(`(?mi)^Content-Disposition:\s*attachment`)
)

type emlInfo struct {
	looksLikeEmail    bool
	truncated         bool
	declaredAttachmts int
}

// inspectEml reads the file once and reports structural hints.
func inspectEml(path string) (emlInfo, error) {
	var info emlInfo
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()

	// Headers: first 8 KiB is plenty to find at least one RFC 822 header line.
	head := make([]byte, 8192)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	info.looksLikeEmail = headerLine.Match(head)

	boundary := ""
	if m := boundaryRe.FindSubmatch(head); m != nil {
		boundary = strings.TrimSpace(string(m[1]))
	}

	// Stream the rest line by line: count attachment parts, find closing boundary.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return info, err
	}
	closing := []byte("--" + boundary + "--")
	sawClosing := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if attachmentDisp.Match(line) {
			info.declaredAttachmts++
		}
		if boundary != "" && bytes.HasPrefix(bytes.TrimSpace(line), closing) {
			sawClosing = true
		}
	}
	if boundary != "" && !sawClosing {
		info.truncated = true
	}
	return info, nil
}

// emlWarnings turns inspection results plus the extraction outcome into user-facing notes.
func emlWarnings(info emlInfo, extracted int, includeNameless bool) (warnings []string, fatal string) {
	if !info.looksLikeEmail {
		return nil, "does not look like an email (no RFC 822 headers found)"
	}
	if info.truncated {
		warnings = append(warnings, "message looks truncated (closing MIME boundary missing); attachments may be incomplete")
	}
	if !includeNameless && info.declaredAttachmts > 0 && extracted < info.declaredAttachmts {
		warnings = append(warnings, fmt.Sprintf("email declares %d attachment(s) but %d were extracted", info.declaredAttachmts, extracted))
	}
	return warnings, ""
}
