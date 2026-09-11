package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound backend.
type App struct {
	ctx     context.Context
	running sync.Mutex
	busy    bool
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Native file drops arrive here with absolute paths; forward them to the UI.
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		if len(paths) == 0 {
			return
		}
		runtime.EventsEmit(ctx, "files:dropped", paths)
	})
}

// Status describes the environment the UI needs to know about at startup.
type Status struct {
	RipmimeFound     bool   `json:"ripmimeFound"`
	RipmimePath      string `json:"ripmimePath"`
	RipmimeVersion   string `json:"ripmimeVersion"`
	DefaultOutputDir string `json:"defaultOutputDir"`
}

// GetStatus reports whether ripmime is available and the default output folder.
func (a *App) GetStatus() Status {
	s := Status{DefaultOutputDir: defaultOutputDir()}
	p, err := exec.LookPath("ripmime")
	if err != nil {
		// Homebrew paths are often missing from a GUI app's PATH.
		for _, c := range []string{"/opt/homebrew/bin/ripmime", "/usr/local/bin/ripmime"} {
			if _, e := os.Stat(c); e == nil {
				p = c
				err = nil
				break
			}
		}
	}
	if err != nil {
		return s
	}
	s.RipmimeFound = true
	s.RipmimePath = p
	out, _ := exec.Command(p, "-V").CombinedOutput()
	if m := versionRe.FindString(string(out)); m != "" {
		s.RipmimeVersion = m
	}
	return s
}

// versionRe pulls "v1.4.1.0" out of ripmime's -V banner.
var versionRe = regexp.MustCompile(`v\d+(?:\.\d+)+`)

func defaultOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, "Downloads", "EML Xtract")
}

// ChooseOutputDir opens a native folder picker.
func (a *App) ChooseOutputDir(current string) (string, error) {
	opts := runtime.OpenDialogOptions{
		Title:                "Choose output folder",
		CanCreateDirectories: true,
	}
	if current != "" {
		if st, err := os.Stat(current); err == nil && st.IsDir() {
			opts.DefaultDirectory = current
		}
	}
	return runtime.OpenDirectoryDialog(a.ctx, opts)
}

// ChooseInputFiles opens a native multi-file picker filtered to eml/msg.
func (a *App) ChooseInputFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose .eml or .msg files",
		Filters: []runtime.FileFilter{
			{DisplayName: "Email files (*.eml, *.msg)", Pattern: "*.eml;*.msg"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
}

// ExtractOptions controls a batch run.
type ExtractOptions struct {
	Paths           []string `json:"paths"`
	OutputDir       string   `json:"outputDir"`
	IncludeNameless bool     `json:"includeNameless"` // keep body text / unnamed inline parts (eml only)
}

// StartExtract runs a batch in the background. Progress is reported via events:
//
//	extract:begin {total}
//	extract:file  FileResult   (one per input)
//	extract:done  {ok, failed, attachments}
func (a *App) StartExtract(opts ExtractOptions) error {
	a.running.Lock()
	if a.busy {
		a.running.Unlock()
		return fmt.Errorf("an extraction is already running")
	}
	a.busy = true
	a.running.Unlock()

	go func() {
		defer func() {
			a.running.Lock()
			a.busy = false
			a.running.Unlock()
		}()
		a.runBatch(opts)
	}()
	return nil
}

func (a *App) runBatch(opts ExtractOptions) {
	files := expandInputs(opts.Paths)
	outBase := strings.TrimSpace(opts.OutputDir)
	if outBase == "" {
		outBase = defaultOutputDir()
	}

	runtime.EventsEmit(a.ctx, "extract:begin", map[string]any{"total": len(files)})

	if err := os.MkdirAll(outBase, 0o755); err != nil {
		runtime.EventsEmit(a.ctx, "extract:done", map[string]any{
			"ok": 0, "failed": len(files), "attachments": 0,
			"error": fmt.Sprintf("cannot create output folder: %v", err),
		})
		return
	}

	status := a.GetStatus()
	ok, failed, total := 0, 0, 0
	for _, f := range files {
		r := extractOne(f, outBase, status.RipmimePath, opts.IncludeNameless)
		if r.Error == "" {
			ok++
		} else {
			failed++
		}
		total += len(r.Attachments)
		runtime.EventsEmit(a.ctx, "extract:file", r)
	}
	runtime.EventsEmit(a.ctx, "extract:done", map[string]any{
		"ok": ok, "failed": failed, "attachments": total, "outputDir": outBase,
	})
}

// expandInputs turns dropped paths (files or folders) into a flat list of .eml/.msg files.
func expandInputs(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			add(p) // let extractOne report the error
			continue
		}
		if !st.IsDir() {
			add(p)
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if isSupported(path) {
				add(path)
			}
			return nil
		})
	}
	return out
}

func isSupported(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".eml", ".msg":
		return true
	}
	return false
}

// OpenPath opens a file or folder with the default macOS handler.
func (a *App) OpenPath(p string) error {
	return exec.Command("open", p).Run()
}

// RevealInFinder selects the file or folder in Finder.
func (a *App) RevealInFinder(p string) error {
	return exec.Command("open", "-R", p).Run()
}
