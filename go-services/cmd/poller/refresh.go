package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
)

// errOffline is the equivalent of Python's PrinterOfflineError: an expected
// unreachable condition (no MQTT report), treated as a normal offline transition.
var errOffline = errors.New("no recent MQTT report from Bambu printer")

// refreshStatus refreshes one printer's live status (network/MQTT I/O only).
func refreshStatus(printer pmap) (pmap, error) {
	profile := mStr(printer, "profile")
	var liveStatus pmap
	var err error
	switch {
	case profile == "snapmaker_u1":
		liveStatus, err = fetchSnapmakerStatus(printer)
		if err != nil {
			return nil, err
		}
		if spools, serr := fetchSnapmakerTaskConfig(printer); serr != nil {
			liveStatus["spools"] = printer["spools"]
		} else {
			liveStatus["spools"] = spools
		}
	case bambuProfiles[profile]:
		liveStatus, err = fetchBambuStatus(printer)
		if err != nil {
			return nil, err
		}
	default:
		liveStatus, err = fetchGenericStatus(printer)
		if err != nil {
			return nil, err
		}
	}
	out := merge(printer, liveStatus)
	out["offlineSince"] = nil
	return out, nil
}

// computeNextPrinter refreshes one printer, falling back to the offline grace
// state on failure. Returns (nextState, refreshFailed). Safe in a worker goroutine.
func computeNextPrinter(printer pmap) (pmap, bool) {
	next, err := refreshStatus(printer)
	if err == nil {
		return next, false
	}
	fallback := applyOfflineGracePeriod(printer, 0)
	if mStr(fallback, "status") == "offline" {
		if isExpectedOffline(err) {
			fallback["errorMessage"] = nil
		} else {
			msg := strings.TrimSpace(err.Error())
			if msg == "" {
				msg = "Printer unreachable"
			}
			fallback["errorMessage"] = truncate(msg, 500)
		}
	}
	return fallback, true
}

// isExpectedOffline reports whether an error is a normal unreachable condition
// (no MQTT report, connection refused, timeout, DNS) — for which no error message
// is surfaced — versus a real fault (e.g. a bad HTTP status or malformed JSON).
func isExpectedOffline(err error) bool {
	if errors.Is(err, errOffline) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return false
}
