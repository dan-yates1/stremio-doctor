package scan

import (
	"sort"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/probe"
)

// Failures counts results that were not a 2xx response.
func (c Check) Failures() int {
	n := 0
	for _, r := range c.Results {
		if !r.OK() {
			n++
		}
	}
	return n
}

// AllFailed reports whether every round failed.
func (c Check) AllFailed() bool { return len(c.Results) > 0 && c.Failures() == len(c.Results) }

// Median returns the per-phase median timing over successful results, or
// over all results when none succeeded.
func (c Check) Median() probe.Timing {
	rs := c.successes()
	if len(rs) == 0 {
		rs = c.Results
	}
	if len(rs) == 0 {
		return probe.Timing{}
	}
	pick := func(f func(probe.Timing) time.Duration) time.Duration {
		ds := make([]time.Duration, len(rs))
		for i, r := range rs {
			ds[i] = f(r.Timing)
		}
		return median(ds)
	}
	return probe.Timing{
		DNS:     pick(func(t probe.Timing) time.Duration { return t.DNS }),
		Connect: pick(func(t probe.Timing) time.Duration { return t.Connect }),
		TLS:     pick(func(t probe.Timing) time.Duration { return t.TLS }),
		TTFB:    pick(func(t probe.Timing) time.Duration { return t.TTFB }),
		Total:   pick(func(t probe.Timing) time.Duration { return t.Total }),
	}
}

// MinMaxTotal returns the fastest and slowest successful total times.
func (c Check) MinMaxTotal() (lo, hi time.Duration) {
	for i, r := range c.successes() {
		if i == 0 || r.Timing.Total < lo {
			lo = r.Timing.Total
		}
		if r.Timing.Total > hi {
			hi = r.Timing.Total
		}
	}
	return lo, hi
}

// MaxItems returns the largest item count seen, or -1 if none was counted.
func (c Check) MaxItems() int {
	best := -1
	for _, n := range c.Items {
		if n > best {
			best = n
		}
	}
	return best
}

// LastError returns the most recent failed result, if any.
func (c Check) LastError() (probe.Result, bool) {
	for i := len(c.Results) - 1; i >= 0; i-- {
		if !c.Results[i].OK() {
			return c.Results[i], true
		}
	}
	return probe.Result{}, false
}

// Find returns the first check of the given kind.
func (a AddonResult) Find(kind string) (Check, bool) {
	for _, c := range a.Checks {
		if c.Kind == kind {
			return c, true
		}
	}
	return Check{}, false
}

func (c Check) successes() []probe.Result {
	var out []probe.Result
	for _, r := range c.Results {
		if r.OK() {
			out = append(out, r)
		}
	}
	return out
}

func median(ds []time.Duration) time.Duration {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}
