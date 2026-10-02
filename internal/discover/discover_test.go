package discover

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
)

const fakeKey = "fake-auth-key-123"

var profileJSON = fmt.Sprintf(`{"auth":{"key":%q,"user":{"email":"x@y.z"}},
"addons":[{"transportUrl":"https://local.example/manifest.json","manifest":{"id":"a","name":"Local A"},"flags":{"official":false}}]}`, fakeKey)

func latin1(s string) []byte { return append([]byte{1}, []byte(s)...) }

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 1+2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[1+2*i:], c)
	}
	return b
}

// writeLocalStorage builds a Chromium-style localStorage leveldb.
func writeLocalStorage(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "leveldb")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range entries {
		if err := db.Put([]byte(k), v, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadProfilesLatin1AndUTF16(t *testing.T) {
	for name, enc := range map[string]func(string) []byte{"latin1": latin1, "utf16": utf16le} {
		t.Run(name, func(t *testing.T) {
			dir := writeLocalStorage(t, map[string][]byte{
				"VERSION":                                 []byte("1"),
				"META:https://web.stremio.com":            {8, 1},
				"_https://web.stremio.com\x00\x01other":   latin1(`{"x":1}`),
				"_https://web.stremio.com\x00\x01profile": enc(profileJSON),
			})
			ps, err := ReadProfiles(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(ps) != 1 || ps[0].AuthKey() != fakeKey || len(ps[0].Addons) != 1 || ps[0].Addons[0].Manifest.Name != "Local A" {
				t.Fatalf("profiles = %+v", ps)
			}
		})
	}
}

func TestReadProfilesLeavesOriginalUntouched(t *testing.T) {
	dir := writeLocalStorage(t, map[string][]byte{"_x\x00\x01profile": latin1(profileJSON)})
	before, _ := os.ReadDir(dir)
	if _, err := ReadProfiles(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(dir)
	if len(before) != len(after) {
		t.Fatalf("files changed: %d → %d", len(before), len(after))
	}
}

func fakeAPI(t *testing.T, handler http.HandlerFunc) {
	srv := httptest.NewServer(handler)
	old := APIBase
	APIBase = srv.URL
	t.Cleanup(func() { APIBase = old; srv.Close() })
}

func TestFetchCollection(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/api/addonCollectionGet" || body["authKey"] != fakeKey {
			fmt.Fprint(w, `{"error":{"message":"session does not exist"}}`)
			return
		}
		fmt.Fprint(w, `{"result":{"addons":[{"transportUrl":"https://remote.example/manifest.json"}]}}`)
	})

	addons, err := FetchCollection(context.Background(), fakeKey)
	if err != nil || len(addons) != 1 || addons[0].TransportURL != "https://remote.example/manifest.json" {
		t.Fatalf("addons=%+v err=%v", addons, err)
	}
	if _, err := FetchCollection(context.Background(), "wrong"); err == nil || err.Error() != "Stremio API: session does not exist" {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverPrefersLiveListAndFallsBackToLocal(t *testing.T) {
	dir := writeLocalStorage(t, map[string][]byte{"_x\x00\x01profile": latin1(profileJSON)})

	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"result":{"addons":[{"transportUrl":"https://remote.example/manifest.json"},{"transportUrl":"https://r2.example/manifest.json"}]}}`)
	})
	res, err := Discover(context.Background(), Options{StorageDir: dir})
	if err != nil || len(res.Addons) != 2 {
		t.Fatalf("live: %+v %v", res, err)
	}

	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	res, err = Discover(context.Background(), Options{StorageDir: dir})
	if err != nil || len(res.Addons) != 1 || len(res.Notes) == 0 {
		t.Fatalf("fallback: %+v %v", res, err)
	}
}

func TestDiscoverManualURLs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "addons.txt")
	os.WriteFile(file, []byte("# comment\n\nhttps://c.example/cfg/\n"), 0o600)
	res, err := Discover(context.Background(), Options{
		ManualURLs: []string{"stremio://a.example/manifest.json", "https://b.example"},
		AddonsFile: file,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://a.example/manifest.json", "https://b.example/manifest.json", "https://c.example/cfg/manifest.json"}
	for i, a := range res.Addons {
		if a.TransportURL != want[i] {
			t.Errorf("addon %d = %s, want %s", i, a.TransportURL, want[i])
		}
	}
	if _, err := Discover(context.Background(), Options{ManualURLs: []string{"not a url"}}); err == nil {
		t.Fatal("expected error for bad URL")
	}
}
