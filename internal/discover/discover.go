// Package discover finds the user's installed Stremio addons: from manual
// URLs, an auth key, or by reading the Stremio desktop app's local storage.
package discover

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/dan-yates1/stremio-doctor/internal/addon"
)

// Options selects the discovery sources. The first that yields addons wins.
type Options struct {
	ManualURLs []string // --addon
	AddonsFile string   // --addons-file
	AuthKey    string   // --auth-key / STREMIO_AUTH_KEY
	StorageDir string   // --profile-dir, overrides auto-detection
}

// Result is the discovered addon list and a description of where it came from.
type Result struct {
	Addons []addon.Installed
	Source string
	Notes  []string // non-fatal problems worth telling the user
}

// ErrNothingFound means no source produced any addons.
var ErrNothingFound = errors.New("no Stremio addons found")

// Discover runs the sources in order. The auth key never leaves this
// function except in the request to the official Stremio API.
func Discover(ctx context.Context, o Options) (Result, error) {
	manual, err := manualAddons(o)
	if err != nil {
		return Result{}, err
	}
	if len(manual) > 0 {
		return Result{Addons: manual, Source: fmt.Sprintf("%d addon URL(s) given on the command line", len(manual))}, nil
	}

	var res Result
	key := o.AuthKey
	var local []addon.Installed
	if key == "" {
		var lerr error
		key, local, res.Source, lerr = fromLocalStorage(o.StorageDir)
		if lerr != nil {
			res.Notes = append(res.Notes, lerr.Error())
		}
	} else {
		res.Source = "auth key"
	}

	if key != "" {
		addons, err := FetchCollection(ctx, key)
		if err == nil && len(addons) > 0 {
			res.Addons = addons
			res.Source += " → Stremio account (live addon list)"
			return res, nil
		}
		if err != nil {
			res.Notes = append(res.Notes, err.Error())
		}
	}
	if len(local) > 0 {
		res.Addons = local
		res.Source += " (local copy of the addon list)"
		return res, nil
	}
	return res, ErrNothingFound
}

func fromLocalStorage(dir string) (key string, addons []addon.Installed, source string, err error) {
	dirs := []string{dir}
	if dir == "" {
		dirs = StorageDirs()
	}
	if len(dirs) == 0 {
		return "", nil, "", errors.New("couldn't find a Stremio desktop install on this computer")
	}
	var errs []string
	for _, d := range dirs {
		profiles, perr := ReadProfiles(d)
		if perr != nil {
			errs = append(errs, perr.Error())
			continue
		}
		for _, p := range profiles {
			if p.AuthKey() != "" || len(p.Addons) > 0 {
				return p.AuthKey(), p.Addons, "Stremio app data", nil
			}
		}
	}
	if len(errs) > 0 {
		return "", nil, "", errors.New("couldn't read Stremio app data: " + strings.Join(errs, "; "))
	}
	return "", nil, "", errors.New("found Stremio app data but no profile in it (are you logged in?)")
}

func manualAddons(o Options) ([]addon.Installed, error) {
	urls := append([]string(nil), o.ManualURLs...)
	if o.AddonsFile != "" {
		fromFile, err := readURLFile(o.AddonsFile)
		if err != nil {
			return nil, err
		}
		urls = append(urls, fromFile...)
	}
	var out []addon.Installed
	for _, raw := range urls {
		u, err := normaliseURL(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, addon.Installed{TransportURL: u})
	}
	return out, nil
}

func readURLFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls, sc.Err()
}

// normaliseURL accepts stremio:// links and bare base URLs and returns an
// https manifest URL.
func normaliseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "stremio://") {
		raw = "https://" + strings.TrimPrefix(raw, "stremio://")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("not an addon URL: %q", raw)
	}
	if !strings.HasSuffix(u.Path, "/manifest.json") {
		escaped := u.EscapedPath() // keep the original encoding of config segments
		u.Path = strings.TrimSuffix(u.Path, "/") + "/manifest.json"
		u.RawPath = strings.TrimSuffix(escaped, "/") + "/manifest.json"
	}
	return u.String(), nil
}
