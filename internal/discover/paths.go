package discover

import (
	"os"
	"path/filepath"
	"runtime"
)

// storageGlobs lists where Stremio desktop apps keep their Chromium
// localStorage, per OS. Globs absorb version-suffixed folders and profile
// names. Covers Stremio 5 (WebView2 shell) and Stremio 4 (Qt WebEngine).
func storageGlobs() []string {
	home, _ := os.UserHomeDir()
	ls := filepath.Join("Local Storage", "leveldb")
	qt := filepath.Join("Smart Code ltd", "Stremio", "QtWebEngine", "*", ls)

	switch runtime.GOOS {
	case "windows":
		local, roaming := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
		return []string{
			filepath.Join(local, "Programs", "Stremio*", "*.WebView2", "EBWebView", "*", ls),
			filepath.Join(local, "Programs", "LNV", "Stremio*", "*.WebView2", "EBWebView", "*", ls),
			filepath.Join(local, "Stremio*", "*.WebView2", "EBWebView", "*", ls),
			filepath.Join(local, "Stremio*", "EBWebView", "*", ls),
			filepath.Join(local, qt),
			filepath.Join(roaming, qt),
		}
	case "darwin":
		support := filepath.Join(home, "Library", "Application Support")
		return []string{
			filepath.Join(support, qt),
			filepath.Join(support, "Stremio*", "*", ls),
			filepath.Join(support, "com.stremio.*", "*", ls),
			filepath.Join(home, "Library", "WebKit", "com.stremio.*", "*", ls),
		}
	default:
		return []string{
			filepath.Join(home, ".local", "share", qt),
			filepath.Join(home, ".var", "app", "com.stremio.Stremio", "data", qt),
			filepath.Join(home, ".config", "Stremio*", "*", ls),
		}
	}
}

// StorageDirs returns existing localStorage directories that may hold a
// Stremio profile, most recently modified first.
func StorageDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	for _, g := range storageGlobs() {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && st.IsDir() && !seen[m] {
				seen[m] = true
				dirs = append(dirs, m)
			}
		}
	}
	sortByModTime(dirs)
	return dirs
}

func sortByModTime(dirs []string) {
	mod := func(p string) int64 {
		if st, err := os.Stat(p); err == nil {
			return st.ModTime().UnixNano()
		}
		return 0
	}
	for i := 1; i < len(dirs); i++ {
		for j := i; j > 0 && mod(dirs[j]) > mod(dirs[j-1]); j-- {
			dirs[j], dirs[j-1] = dirs[j-1], dirs[j]
		}
	}
}
