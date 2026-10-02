package diagnose

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// slowestShown is how many addons the "slowest stream addons" summary lists.
const slowestShown = 3

// Global finds problems that only show up across several addons.
func Global(all []scan.AddonResult) []Finding {
	var fs []Finding
	fs = append(fs, duplicates(all)...)
	fs = append(fs, sharedHosts(all)...)
	if f, ok := slowestStreams(all); ok {
		fs = append(fs, f)
	}
	return fs
}

func duplicates(all []scan.AddonResult) []Finding {
	byID := map[string][]string{}
	var order []string
	for _, a := range all {
		if a.ID == "" {
			continue
		}
		if _, seen := byID[a.ID]; !seen {
			order = append(order, a.ID)
		}
		byID[a.ID] = append(byID[a.ID], a.Name)
	}
	var fs []Finding
	for _, id := range order {
		if names := byID[id]; len(names) > 1 {
			fs = append(fs, Finding{Severity: Warn, Title: fmt.Sprintf("%s is installed %d times", names[0], len(names)),
				Detail: "Stremio queries every copy, so streams load slower and show up twice. Remove the extra copies unless they're deliberately configured differently."})
		}
	}
	return fs
}

// sharedHosts flags hosting sites where several addons fail or are slow
// together, which points at the host rather than the individual addons.
func sharedHosts(all []scan.AddonResult) []Finding {
	groups := map[string][]scan.AddonResult{}
	var order []string
	for _, a := range all {
		site := Site(a.Host)
		if site == "" {
			continue
		}
		if _, seen := groups[site]; !seen {
			order = append(order, site)
		}
		groups[site] = append(groups[site], a)
	}
	var fs []Finding
	for _, site := range order {
		g := groups[site]
		if len(g) < 2 {
			continue
		}
		down, slow := 0, 0
		for _, a := range g {
			switch {
			case isDown(a):
				down++
			case isSlowStream(a):
				slow++
			}
		}
		switch {
		case down == len(g):
			fs = append(fs, Finding{Severity: Fail, Title: "Host " + site + " is down",
				Detail: fmt.Sprintf("All %d addons hosted on %s are failing (%s). The problem is the host, not the individual addons.", len(g), site, names(g))})
		case down+slow == len(g):
			fs = append(fs, Finding{Severity: Warn, Title: "Host " + site + " is struggling",
				Detail: fmt.Sprintf("All %d addons on %s are failing or slow (%s), which suggests the host is overloaded.", len(g), site, names(g))})
		}
	}
	return fs
}

// slowestStreams summarises which addons hold up the stream list. Stremio
// keeps loading until every stream addon has answered or given up.
func slowestStreams(all []scan.AddonResult) (Finding, bool) {
	type entry struct {
		name string
		d    time.Duration
	}
	var es []entry
	for _, a := range all {
		var worst time.Duration
		for _, c := range a.Checks {
			if c.Kind == scan.KindStream && c.Failures() < len(c.Results) {
				if t := c.Median().Total; t > worst {
					worst = t
				}
			}
		}
		if worst > 0 {
			es = append(es, entry{a.Name, worst})
		}
	}
	if len(es) < 2 {
		return Finding{}, false
	}
	sort.Slice(es, func(i, j int) bool { return es[i].d > es[j].d })
	if es[0].d < Thresholds[scan.KindStream].Slow {
		return Finding{}, false
	}
	var parts []string
	for i := 0; i < len(es) && i < slowestShown; i++ {
		parts = append(parts, fmt.Sprintf("%s (%s)", es[i].name, Dur(es[i].d)))
	}
	return Finding{Severity: Info, Title: "What's holding up your stream list",
		Detail: "The stream list keeps loading until the slowest addon answers. Slowest: " + strings.Join(parts, ", ") + "."}, true
}

func isDown(a scan.AddonResult) bool {
	for _, c := range a.Checks {
		if !c.AllFailed() {
			return false
		}
	}
	return len(a.Checks) > 0
}

func isSlowStream(a scan.AddonResult) bool {
	for _, c := range a.Checks {
		if c.Kind == scan.KindStream && c.Median().Total >= Thresholds[scan.KindStream].Slow {
			return true
		}
	}
	return false
}

func names(g []scan.AddonResult) string {
	ns := make([]string, len(g))
	for i, a := range g {
		ns[i] = a.Name
	}
	return strings.Join(ns, ", ")
}

// Site approximates the registrable domain of a host ("a.b.elfhosted.com" →
// "elfhosted.com", "x.co.uk" style suffixes keep three labels). IPs and
// localhost are returned unchanged.
func Site(host string) string {
	if host == "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return host
	}
	labels := strings.Split(host, ".")
	n := 2
	if len(labels) >= 3 && len(labels[len(labels)-1]) == 2 && len(labels[len(labels)-2]) <= 3 {
		n = 3
	}
	if len(labels) <= n {
		return host
	}
	return strings.Join(labels[len(labels)-n:], ".")
}
