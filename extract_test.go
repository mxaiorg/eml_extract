package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"a/b\\c:d":      "a_b_c_d",
		"  ..hidden  ":  "hidden",
		"":              "untitled",
		"..":            "untitled",
		"Report Q3.pdf": "Report Q3.pdf",
		"x\x00y":        "x_y",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueOutputDirDedupes(t *testing.T) {
	base := t.TempDir()
	a, err := uniqueOutputDir(base, "/x/Mail Item.eml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := uniqueOutputDir(base, "/y/Mail Item.eml")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(a) != "Mail Item" || filepath.Base(b) != "Mail Item-2" {
		t.Errorf("got %q and %q", a, b)
	}
}

func TestExtractMsg(t *testing.T) {
	for _, name := range []string{"sample1.msg", "sample2.msg"} {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			r := extractOne(filepath.Join("testdata", name), out, "", false)
			if r.Error != "" {
				t.Fatalf("error: %s", r.Error)
			}
			if r.Kind != "msg" {
				t.Errorf("kind = %s", r.Kind)
			}
			t.Logf("%s: %d attachments, warnings=%v", name, len(r.Attachments), r.Warnings)
			for _, a := range r.Attachments {
				st, err := os.Stat(a.Path)
				if err != nil || st.Size() != a.Size {
					t.Errorf("attachment %s missing or wrong size", a.Name)
				}
				t.Logf("  %s (%d bytes)", a.Name, a.Size)
			}
		})
	}
}

func TestExtractEml(t *testing.T) {
	bin, err := exec.LookPath("ripmime")
	if err != nil {
		t.Skip("ripmime not installed")
	}
	// Build a small multipart email with two attachments, one with spaces in the name.
	eml := strings.Join([]string{
		"From: a@example.com",
		"To: b@example.com",
		"Subject: test",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"XX\"",
		"",
		"--XX",
		"Content-Type: text/plain",
		"",
		"hello body",
		"--XX",
		"Content-Type: application/pdf; name=\"Quarterly Report (final).pdf\"",
		"Content-Disposition: attachment; filename=\"Quarterly Report (final).pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0xLjQK",
		"--XX",
		"Content-Type: text/csv",
		"Content-Disposition: attachment; filename=\"data.csv\"",
		"",
		"a,b",
		"1,2",
		"--XX--",
		"",
	}, "\r\n")
	src := filepath.Join(t.TempDir(), "my message.eml")
	if err := os.WriteFile(src, []byte(eml), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	r := extractOne(src, out, bin, false)
	if r.Error != "" {
		t.Fatalf("error: %s (stderr: %s)", r.Error, r.Stderr)
	}
	names := map[string]bool{}
	for _, a := range r.Attachments {
		names[a.Name] = true
	}
	if !names["Quarterly Report (final).pdf"] || !names["data.csv"] {
		t.Errorf("unexpected attachments: %v", names)
	}
	if len(r.Attachments) != 2 {
		t.Errorf("expected only the 2 named attachments (no body text), got %d: %v", len(r.Attachments), names)
	}
	if filepath.Base(r.OutputDir) != "my message" {
		t.Errorf("output dir = %s", r.OutputDir)
	}

	// Nameless parts included → body text appears too.
	out2 := t.TempDir()
	r2 := extractOne(src, out2, bin, true)
	if len(r2.Attachments) < 3 {
		t.Errorf("with includeNameless expected >=3 files, got %d", len(r2.Attachments))
	}
}

func TestExtractEmlCorrupt(t *testing.T) {
	bin, err := exec.LookPath("ripmime")
	if err != nil {
		t.Skip("ripmime not installed")
	}
	src := filepath.Join(t.TempDir(), "garbage.eml")
	_ = os.WriteFile(src, []byte("\x00\x01\x02 not an email at all"), 0o644)
	r := extractOne(src, t.TempDir(), bin, false)
	t.Logf("corrupt: err=%q attachments=%d outputDir=%q stderr=%q", r.Error, len(r.Attachments), r.OutputDir, r.Stderr)
	if len(r.Attachments) != 0 {
		t.Errorf("expected no attachments from garbage")
	}
	if r.OutputDir != "" {
		t.Errorf("empty output folder should have been removed")
	}
}

func TestExpandInputs(t *testing.T) {
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, "sub"), 0o755)
	for _, n := range []string{"a.EML", "b.msg", "sub/c.eml", "sub/ignore.txt"} {
		_ = os.WriteFile(filepath.Join(d, n), []byte("x"), 0o644)
	}
	got := expandInputs([]string{d, filepath.Join(d, "a.EML")})
	if len(got) != 3 {
		t.Errorf("expected 3 unique email files, got %v", got)
	}
}

func TestInspectEml(t *testing.T) {
	d := t.TempDir()
	good := "From: a@b\r\nContent-Type: multipart/mixed; boundary=\"B1\"\r\n\r\n--B1\r\nContent-Disposition: attachment; filename=\"x.txt\"\r\n\r\nhi\r\n--B1--\r\n"
	trunc := "From: a@b\r\nContent-Type: multipart/mixed; boundary=B1\r\n\r\n--B1\r\nContent-Disposition: attachment; filename=\"x.txt\"\r\n\r\nhi\r\n"
	garbage := "\x00\x01 nothing here"
	for name, c := range map[string]struct {
		body   string
		email  bool
		trunc  bool
		declar int
	}{
		"good":    {good, true, false, 1},
		"trunc":   {trunc, true, true, 1},
		"garbage": {garbage, false, false, 0},
	} {
		p := filepath.Join(d, name+".eml")
		_ = os.WriteFile(p, []byte(c.body), 0o644)
		info, err := inspectEml(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.looksLikeEmail != c.email || info.truncated != c.trunc || info.declaredAttachmts != c.declar {
			t.Errorf("%s: got %+v", name, info)
		}
	}
}
