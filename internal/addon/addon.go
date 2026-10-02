// Package addon models Stremio addon manifests and the installed-addon
// descriptors stored in a Stremio profile, and builds addon resource URLs.
package addon

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Manifest is the subset of a Stremio addon manifest this tool needs.
type Manifest struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	Types      []string   `json:"types"`
	IDPrefixes []string   `json:"idPrefixes"`
	Resources  []Resource `json:"resources"`
	Catalogs   []Catalog  `json:"catalogs"`
}

// Resource is one manifest resource. In manifests it is either a plain string
// ("stream") or an object with its own types and idPrefixes.
type Resource struct {
	Name       string   `json:"name"`
	Types      []string `json:"types,omitempty"`
	IDPrefixes []string `json:"idPrefixes,omitempty"`
}

// UnmarshalJSON accepts both the short (string) and long (object) forms.
func (r *Resource) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err == nil {
		*r = Resource{Name: name}
		return nil
	}
	type plain Resource
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*r = Resource(p)
	return nil
}

// Catalog is one catalog declared in a manifest.
type Catalog struct {
	Type  string  `json:"type"`
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Extra []Extra `json:"extra"`
}

// Extra is a catalog extra property (search, genre, skip, ...).
type Extra struct {
	Name       string `json:"name"`
	IsRequired bool   `json:"isRequired"`
}

// Installed is an addon as it appears in a user's addon collection.
type Installed struct {
	TransportURL string    `json:"transportUrl"`
	Manifest     *Manifest `json:"manifest,omitempty"`
	Flags        Flags     `json:"flags"`
}

// Flags are the collection flags Stremio attaches to an installed addon.
type Flags struct {
	Official  bool `json:"official"`
	Protected bool `json:"protected"`
}

// Supports reports whether the manifest declares resource for the given
// content type and id.
func (m *Manifest) Supports(resource, contentType, id string) bool {
	for _, r := range m.Resources {
		if r.Name != resource {
			continue
		}
		types, prefixes := r.Types, r.IDPrefixes
		if types == nil {
			types = m.Types
		}
		if prefixes == nil {
			prefixes = m.IDPrefixes
		}
		return contains(types, contentType) && matchesPrefix(prefixes, id)
	}
	return false
}

// HasResource reports whether the manifest declares resource at all.
func (m *Manifest) HasResource(resource string) bool {
	for _, r := range m.Resources {
		if r.Name == resource {
			return true
		}
	}
	return false
}

// BrowsableCatalog returns the first catalog that needs no required extra
// (i.e. one Stremio would load on the Board/Discover page), if any.
func (m *Manifest) BrowsableCatalog() (Catalog, bool) {
	for _, c := range m.Catalogs {
		if !hasRequiredExtra(c) {
			return c, true
		}
	}
	return Catalog{}, false
}

// BaseURL strips the trailing /manifest.json from a transport URL.
func BaseURL(transportURL string) string {
	u, err := url.Parse(transportURL)
	if err != nil {
		return strings.TrimSuffix(transportURL, "/manifest.json")
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimSuffix(u.Path, "/manifest.json")
	u.RawPath = strings.TrimSuffix(u.RawPath, "/manifest.json")
	return strings.TrimSuffix(u.String(), "/")
}

// ResourceURL builds the URL Stremio requests for a resource, e.g.
// <base>/stream/movie/tt0111161.json.
func ResourceURL(transportURL, resource, contentType, id string) string {
	return BaseURL(transportURL) + "/" + resource + "/" + url.PathEscape(contentType) + "/" + url.PathEscape(id) + ".json"
}

// DisplayName returns the best available name for an addon.
func (a Installed) DisplayName() string {
	if a.Manifest != nil && a.Manifest.Name != "" {
		return a.Manifest.Name
	}
	if u, err := url.Parse(a.TransportURL); err == nil && u.Host != "" {
		return u.Host
	}
	return a.TransportURL
}

func hasRequiredExtra(c Catalog) bool {
	for _, e := range c.Extra {
		if e.IsRequired {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func matchesPrefix(prefixes []string, id string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}
