package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Attachment is one extracted file.
type Attachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// FileResult is the outcome for one input email.
type FileResult struct {
	Source      string       `json:"source"`
	Kind        string       `json:"kind"` // "eml" | "msg" | "unknown"
	OutputDir   string       `json:"outputDir"`
	Attachments []Attachment `json:"attachments"`
	Warnings    []string     `json:"warnings"`
	Error       string       `json:"error"`
	Stderr      string       `json:"stderr"`
}

const ripmimeTimeout = 2 * time.Minute

func extractOne(src, outBase, ripmimePath string, includeNameless bool) FileResult {
	r := FileResult{Source: src, Kind: "unknown", Attachments: []Attachment{}, Warnings: []string{}}

	st, err := os.Stat(src)
	if err != nil {
		r.Error = fmt.Sprintf("cannot read file: %v", err)
		return r
	}
	if st.IsDir() {
		r.Error = "is a folder"
		return r
	}
	if st.Size() == 0 {
		r.Error = "file is empty"
		return r
	}

	ext := strings.ToLower(filepath.Ext(src))
	switch ext {
	case ".eml":
		r.Kind = "eml"
	case ".msg":
		r.Kind = "msg"
	default:
		// Sniff: OLE compound files start with D0 CF 11 E0.
		if isOLE(src) {
			r.Kind = "msg"
		} else {
			r.Kind = "eml" // let ripmime try; it copes with most RFC822-ish input
		}
	}

	outDir, err := uniqueOutputDir(outBase, src)
	if err != nil {
		r.Error = fmt.Sprintf("cannot create output folder: %v", err)
		return r
	}
	r.OutputDir = outDir

	switch r.Kind {
	case "msg":
		atts, warns, err := extractMsg(src, outDir)
		r.Attachments = atts
		r.Warnings = append(r.Warnings, warns...)
		if err != nil {
			r.Error = err.Error()
		}
	default:
		if ripmimePath == "" {
			r.Error = "ripmime is not installed (brew install ripmime)"
			break
		}
		info, ierr := inspectEml(src)
		if ierr == nil && !info.looksLikeEmail {
			r.Error = "does not look like an email (no RFC 822 headers found)"
			break
		}
		stderr, err := runRipmime(ripmimePath, src, outDir, includeNameless)
		r.Stderr = stderr
		if err != nil {
			r.Error = err.Error()
		}
		r.Attachments, _ = listDir(outDir)
		if ierr == nil {
			warns, _ := emlWarnings(info, len(r.Attachments), includeNameless)
			r.Warnings = append(r.Warnings, warns...)
		}
		if r.Error == "" && stderr != "" {
			r.Warnings = append(r.Warnings, "ripmime reported: "+firstLine(stderr))
		}
	}

	// Remove an empty output folder so the corpus doesn't fill with husks.
	if len(r.Attachments) == 0 {
		if entries, e := os.ReadDir(outDir); e == nil && len(entries) == 0 {
			_ = os.Remove(outDir)
			r.OutputDir = ""
		}
	}
	return r
}

func isOLE(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [8]byte
	if _, err := f.Read(hdr[:]); err != nil {
		return false
	}
	return bytes.Equal(hdr[:], []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
}

// runRipmime shells out with an argv slice (never a shell string) and
// treats a non-zero exit or noisy stderr as a failure signal.
func runRipmime(bin, src, outDir string, includeNameless bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ripmimeTimeout)
	defer cancel()

	args := []string{"-i", src, "-d", outDir, "--overwrite", "--stderr", "--verbose-defects"}
	if !includeNameless {
		args = append(args, "--no-nameless")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	errText := strings.TrimSpace(stderr.String())

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errText, fmt.Errorf("ripmime timed out after %s", ripmimeTimeout)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			msg := fmt.Sprintf("ripmime exited with code %d", ee.ExitCode())
			if errText != "" {
				msg += ": " + firstLine(errText)
			}
			return errText, errors.New(msg)
		}
		return errText, fmt.Errorf("ripmime failed to run: %w", err)
	}
	return errText, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var unsafeChars = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]+`)

// sanitizeName makes a string safe to use as a single path component.
func sanitizeName(s string) string {
	s = unsafeChars.ReplaceAllString(s, "_")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ". ")
	if s == "" || s == "." || s == ".." {
		return "untitled"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// uniqueOutputDir creates <outBase>/<email-name>[-N] and returns it.
func uniqueOutputDir(outBase, src string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	base = sanitizeName(base)
	for i := 1; i < 10000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		dir := filepath.Join(outBase, name)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("too many folders named %q", base)
}

// uniquePath returns a non-existing path in dir for the given filename.
func uniquePath(dir, name string) string {
	name = sanitizeName(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		p := filepath.Join(dir, n)
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
}

func listDir(dir string) ([]Attachment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Attachment, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Attachment{
			Name: e.Name(),
			Path: filepath.Join(dir, e.Name()),
			Size: info.Size(),
		})
	}
	return out, nil
}
