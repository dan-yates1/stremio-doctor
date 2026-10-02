package tray

import (
	"embed"
	"runtime"
)

//go:generate go run gen_icons.go

//go:embed icons
var iconFS embed.FS

var iconNames = map[State]string{StateScanning: "scanning", StateOK: "ok", StateWarn: "warn", StateFail: "fail"}

// iconBytes returns the icon for a state: ICO on Windows, PNG elsewhere.
func iconBytes(s State) []byte {
	ext := ".png"
	if runtime.GOOS == "windows" {
		ext = ".ico"
	}
	b, _ := iconFS.ReadFile("icons/" + iconNames[s] + ext)
	return b
}
