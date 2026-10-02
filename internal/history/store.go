package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// maxLine bounds one JSONL line; an entry for dozens of addons is a few KB.
const maxLine = 4 << 20

// DefaultPath is where history lives unless --history says otherwise.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "stremio-doctor", "history.jsonl")
}

// Append adds one entry to the history file, creating it if needed.
func Append(path string, e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads entries at or after since, oldest first. Malformed lines (e.g.
// from a crash mid-write) are skipped. A missing file is no history.
func Load(path string, since time.Time) ([]Entry, error) {
	entries, _, err := load(path, since)
	return entries, err
}

// Prune drops entries older than cutoff, rewriting the file only when
// something was dropped, and returns the entries that remain.
func Prune(path string, cutoff time.Time) ([]Entry, error) {
	kept, dropped, err := load(path, cutoff)
	if err != nil || !dropped {
		return kept, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".history-*.jsonl")
	if err != nil {
		return kept, err
	}
	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	for _, e := range kept {
		if err := enc.Encode(e); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return kept, err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return kept, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return kept, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return kept, err
	}
	return kept, nil
}

// load returns entries at or after since and whether any line was dropped.
func load(path string, since time.Time) ([]Entry, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	var (
		out     []Entry
		dropped bool
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Time.Before(since) {
			dropped = true
			continue
		}
		out = append(out, e)
	}
	return out, dropped, sc.Err()
}
