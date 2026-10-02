package share

// PublicHosts are well-known shared addon instances. Only addons served from
// one of these exact hosts are ever shared: a host that isn't on the list
// may be a personal or self-hosted instance, which would identify the user.
// Per-user hosts (e.g. ElfHosted's "<user>-<app>.elfhosted.com") must never
// be added.
var PublicHosts = map[string]bool{
	"v3-cinemeta.strem.io":                     true,
	"opensubtitles-v3.strem.io":                true,
	"watchhub.strem.io":                        true,
	"torrentio.strem.fun":                      true,
	"thepiratebay-plus.strem.fun":              true,
	"comet.elfhosted.com":                      true,
	"mediafusion.elfhosted.com":                true,
	"jackettio.elfhosted.com":                  true,
	"aiostreams.elfhosted.com":                 true,
	"stremio.torbox.app":                       true,
	"94c8cb9f702d-tmdb-addon.baby-beamup.club": true,
}
