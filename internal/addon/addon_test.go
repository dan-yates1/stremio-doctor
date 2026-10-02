package addon

import (
	"encoding/json"
	"testing"
)

const sampleManifest = `{
  "id": "org.example", "name": "Example", "version": "1.0.0",
  "types": ["movie", "series"], "idPrefixes": ["tt"],
  "resources": ["stream", {"name": "meta", "types": ["movie"], "idPrefixes": ["ex:"]}],
  "catalogs": [
    {"type": "movie", "id": "search", "extra": [{"name": "search", "isRequired": true}]},
    {"type": "movie", "id": "top"}
  ]
}`

func parse(t *testing.T) Manifest {
	t.Helper()
	var m Manifest
	if err := json.Unmarshal([]byte(sampleManifest), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManifestParsesShortAndLongResources(t *testing.T) {
	m := parse(t)
	if len(m.Resources) != 2 || m.Resources[0].Name != "stream" || m.Resources[1].Name != "meta" {
		t.Fatalf("resources = %+v", m.Resources)
	}
}

func TestSupportsUsesResourceOverridesThenManifestDefaults(t *testing.T) {
	m := parse(t)
	cases := []struct {
		res, typ, id string
		want         bool
	}{
		{"stream", "movie", "tt0111161", true},
		{"stream", "series", "tt0944947:1:1", true},
		{"stream", "movie", "kitsu:1", false},
		{"stream", "channel", "tt1", false},
		{"meta", "movie", "ex:42", true},
		{"meta", "movie", "tt0111161", false},
		{"subtitles", "movie", "tt0111161", false},
	}
	for _, c := range cases {
		if got := m.Supports(c.res, c.typ, c.id); got != c.want {
			t.Errorf("Supports(%s,%s,%s) = %v, want %v", c.res, c.typ, c.id, got, c.want)
		}
	}
}

func TestBrowsableCatalogSkipsRequiredExtras(t *testing.T) {
	m := parse(t)
	c, ok := m.BrowsableCatalog()
	if !ok || c.ID != "top" {
		t.Fatalf("got %+v, %v", c, ok)
	}
}

func TestResourceURLKeepsEncodedConfig(t *testing.T) {
	got := ResourceURL("https://x.example/%7B%22k%22%3A1%7D/manifest.json", "stream", "series", "tt0944947:1:1")
	want := "https://x.example/%7B%22k%22%3A1%7D/stream/series/tt0944947:1:1.json"
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}
