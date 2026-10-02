package discover

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/dan-yates1/stremio-doctor/internal/addon"
)

// Profile is the part of the stremio-core profile stored in localStorage.
type Profile struct {
	Auth *struct {
		Key string `json:"key"`
	} `json:"auth"`
	Addons []addon.Installed `json:"addons"`
}

// AuthKey returns the profile's auth key, or "" when logged out.
func (p Profile) AuthKey() string {
	if p.Auth == nil {
		return ""
	}
	return p.Auth.Key
}

// profileKeySuffix ends every Chromium localStorage key for "profile":
// "_" + origin + "\x00" + "\x01" (Latin-1 marker) + "profile".
var profileKeySuffix = []byte("\x00\x01profile")

// ReadProfiles reads every Stremio "profile" entry from a Chromium
// "Local Storage/leveldb" directory. The directory is copied first because
// the running app holds a lock on it; the original is never modified.
func ReadProfiles(dir string) ([]Profile, error) {
	tmp, err := os.MkdirTemp("", "stremio-doctor-ls-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyDir(dir, tmp); err != nil {
		return nil, fmt.Errorf("copy local storage: %w", err)
	}

	db, err := leveldb.OpenFile(tmp, &opt.Options{ReadOnly: true})
	if err != nil {
		// A copy taken while the app is writing can look corrupted; recover
		// rebuilds the manifest from the table files.
		db, err = leveldb.RecoverFile(tmp, nil)
		if err != nil {
			return nil, fmt.Errorf("open local storage: %w", err)
		}
	}
	defer db.Close()

	var profiles []Profile
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		if !bytes.HasPrefix(it.Key(), []byte("_")) || !bytes.HasSuffix(it.Key(), profileKeySuffix) {
			continue
		}
		raw, err := decodeValue(it.Value())
		if err != nil {
			continue
		}
		var p Profile
		if json.Unmarshal([]byte(raw), &p) == nil && (p.AuthKey() != "" || len(p.Addons) > 0) {
			profiles = append(profiles, p)
		}
	}
	return profiles, it.Error()
}

// decodeValue decodes a Chromium localStorage value: a one-byte format
// prefix (0 = UTF-16LE, 1 = Latin-1) followed by the string.
func decodeValue(v []byte) (string, error) {
	if len(v) == 0 {
		return "", errors.New("empty value")
	}
	body := v[1:]
	switch v[0] {
	case 1:
		runes := make([]rune, len(body))
		for i, b := range body {
			runes[i] = rune(b)
		}
		return string(runes), nil
	case 0:
		if len(body)%2 != 0 {
			return "", errors.New("odd UTF-16 length")
		}
		u := make([]uint16, len(body)/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(body[2*i:])
		}
		return string(utf16.Decode(u)), nil
	}
	return "", fmt.Errorf("unknown value format %d", v[0])
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || strings.EqualFold(e.Name(), "LOCK") {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
