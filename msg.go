package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
)

// Outlook .msg files are OLE compound documents (MS-OXMSG). Each attachment
// lives in a top-level storage named "__attach_version1.0_#NNNNNNNN" holding
// property streams named "__substg1.0_<tag><type>":
//
//	3701 0102  PR_ATTACH_DATA_BIN       raw bytes
//	3701 000D  PR_ATTACH_DATA_OBJ       storage: embedded message / OLE object
//	3707 001F  PR_ATTACH_LONG_FILENAME  UTF-16LE
//	3704 001F  PR_ATTACH_FILENAME       UTF-16LE (8.3 short name)
//	3001 001F  PR_DISPLAY_NAME          UTF-16LE
//	           ...001E variants are 8-bit (code page) strings
const attachPrefix = "__attach_version1.0_#"

type msgAttachment struct {
	storage  string
	data     []byte
	hasData  bool
	embedded bool
	longName string
	name     string
	display  string
}

// extractMsg writes every binary attachment of a .msg into outDir.
func extractMsg(src, outDir string) ([]Attachment, []string, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	doc, err := mscfb.New(f)
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid Outlook .msg (OLE) file: %v", err)
	}

	atts := map[string]*msgAttachment{}
	order := []string{}
	get := func(storage string) *msgAttachment {
		a, ok := atts[storage]
		if !ok {
			a = &msgAttachment{storage: storage}
			atts[storage] = a
			order = append(order, storage)
		}
		return a
	}

	for entry, err := doc.Next(); err == nil; entry, err = doc.Next() {
		// Only direct children of a top-level attachment storage.
		if len(entry.Path) != 1 || !strings.HasPrefix(entry.Path[0], attachPrefix) {
			continue
		}
		a := get(entry.Path[0])
		name := entry.Name
		if !strings.HasPrefix(name, "__substg1.0_") {
			continue
		}
		tag := strings.ToUpper(strings.TrimPrefix(name, "__substg1.0_"))
		if len(tag) != 8 {
			continue
		}
		prop, typ := tag[:4], tag[4:]

		switch {
		case prop == "3701" && typ == "0102":
			b, err := readStream(entry)
			if err != nil {
				return nil, nil, fmt.Errorf("reading %s: %v", entry.Path[0], err)
			}
			a.data, a.hasData = b, true
		case prop == "3701" && typ == "000D":
			a.embedded = true
		case prop == "3707":
			a.longName = readString(entry, typ)
		case prop == "3704":
			a.name = readString(entry, typ)
		case prop == "3001":
			a.display = readString(entry, typ)
		}
	}

	var out []Attachment
	var warns []string
	for i, key := range order {
		a := atts[key]
		if !a.hasData {
			if a.embedded {
				label := firstNonEmpty(a.longName, a.display, a.name, fmt.Sprintf("attachment %d", i+1))
				warns = append(warns, fmt.Sprintf("skipped embedded message/OLE object %q (not a file attachment)", label))
			}
			continue
		}
		fname := firstNonEmpty(a.longName, a.name, a.display)
		if fname == "" {
			fname = fmt.Sprintf("attachment-%d.bin", i+1)
		}
		dst := uniquePath(outDir, filepath.Base(fname))
		if err := os.WriteFile(dst, a.data, 0o644); err != nil {
			return out, warns, fmt.Errorf("writing %s: %v", filepath.Base(dst), err)
		}
		out = append(out, Attachment{Name: filepath.Base(dst), Path: dst, Size: int64(len(a.data))})
	}
	if len(order) == 0 && len(out) == 0 {
		// No attachment storages at all: fine, just an email with no attachments.
		return out, warns, nil
	}
	return out, warns, nil
}

func readStream(e *mscfb.File) ([]byte, error) {
	if e.Size <= 0 {
		return []byte{}, nil
	}
	b := make([]byte, e.Size)
	_, err := io.ReadFull(e, b)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return b, nil
}

func readString(e *mscfb.File, typ string) string {
	b, err := readStream(e)
	if err != nil {
		return ""
	}
	switch typ {
	case "001F": // UTF-16LE
		if len(b)%2 == 1 {
			b = b[:len(b)-1]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		return strings.TrimRight(string(utf16.Decode(u)), "\x00")
	case "001E": // 8-bit; assume ASCII/UTF-8-compatible
		return strings.TrimRight(string(b), "\x00")
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
