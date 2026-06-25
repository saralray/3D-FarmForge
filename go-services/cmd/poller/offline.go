package main

import "time"

func nowSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// buildOfflinePrinterState returns the telemetry overlay for an offline printer.
func buildOfflinePrinterState(printer pmap) pmap {
	nozzles := mSlice(printer, "nozzleTemperatures")
	var nozzleArr []any
	if len(nozzles) > 0 {
		nozzleArr = make([]any, len(nozzles))
		for i := range nozzleArr {
			nozzleArr[i] = float64(0)
		}
	} else {
		nozzleArr = []any{float64(0)}
	}
	return pmap{
		"status":             "offline",
		"currentJob":         nil,
		"progress":           float64(0),
		"temperature":        pmap{"nozzle": float64(0), "bed": float64(0), "chamber": float64(0)},
		"nozzleTemperatures": nozzleArr,
		"fanSpeeds":          nil,
	}
}

// applyOfflineGracePeriod keeps the last-known state until OFFLINE_GRACE_SECONDS
// has elapsed since offline was first detected, then overlays the offline state.
func applyOfflineGracePeriod(printer pmap, now float64) pmap {
	if now == 0 {
		now = nowSeconds()
	}
	detectedAt, ok := mFloat(printer, "offlineSince")
	if !ok {
		return merge(printer, pmap{"offlineSince": now})
	}
	if now-detectedAt < offlineGrace.Seconds() {
		return printer
	}
	out := merge(printer, buildOfflinePrinterState(printer))
	out["offlineSince"] = detectedAt
	return out
}
