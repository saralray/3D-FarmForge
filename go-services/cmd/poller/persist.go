package main

import (
	"encoding/json"
	"time"

	"printfarm/internal/telemetry"
)

// Fields upsertPrinter persists (minus id) — kept in sync with that function so a
// real change is never missed.
var persistFields = []string{
	"name", "model", "sortOrder", "profile", "url", "ipAddress", "apiKeyHeader", "serial",
	"status", "progress", "lastMaintenance", "totalPrintTime", "successRate", "bedTarget",
	"chamberTarget", "lightOn", "airFilterOn", "errorMessage", "offlineSince",
	"temperature", "currentJob", "nozzleTemperatures", "nozzleTargets", "spools", "fanSpeeds",
}

// Volatile, non-secret telemetry mirrored to Redis each cycle.
var telemetryFields = []string{
	"status", "progress", "totalPrintTime", "successRate", "bedTarget", "chamberTarget",
	"lightOn", "airFilterOn", "errorMessage", "offlineSince", "temperature", "currentJob",
	"nozzleTemperatures", "nozzleTargets", "spools", "fanSpeeds",
}

// Change-detection + last-write caches (main-thread only).
var (
	lastPersistSig = map[string]string{}
	lastPGWrite    = map[string]float64{}
)

// printingSince tracks when each printer was last seen printing (in-memory only).
var printingSince = map[string]float64{}

// redisClient is the optional telemetry sink, set in run().
var redisClient *telemetry.Client

func persistSignature(printer pmap) string {
	payload := make(pmap, len(persistFields))
	for _, field := range persistFields {
		payload[field] = printer[field]
	}
	// json.Marshal sorts map keys, giving a deterministic signature (the Python
	// sort_keys=True equivalent); equal states serialize identically.
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

func shouldPersistPrinter(printerID, signature string, now float64) bool {
	if signature != lastPersistSig[printerID] {
		return true
	}
	if persistMaxInterval <= 0 {
		return true
	}
	return (now - lastPGWrite[printerID]) >= persistMaxInterval.Seconds()
}

func accumulateTotalPrintTime(printer pmap) float64 {
	printerID := mStr(printer, "id")
	now := nowSeconds()
	total := mFloatDef(printer, "totalPrintTime", 0)

	last, ok := printingSince[printerID]
	if mStr(printer, "status") == "printing" {
		if ok {
			elapsed := now - last
			if elapsed > 0 && elapsed <= maxPrintTimeStep.Seconds() {
				total += elapsed / 3600
			}
		}
		printingSince[printerID] = now
	} else if printerID != "" {
		delete(printingSince, printerID)
	}
	return total
}

func pruneTracking(activeIDs map[string]bool) {
	for id := range printingSince {
		if !activeIDs[id] {
			delete(printingSince, id)
		}
	}
	for id := range printingSpools {
		if !activeIDs[id] {
			delete(printingSpools, id)
		}
	}
	for id := range runoutReported {
		if !activeIDs[id] {
			delete(runoutReported, id)
		}
	}
	bambuPrintBaselineMu.Lock()
	for id := range bambuPrintBaseline {
		if !activeIDs[id] {
			delete(bambuPrintBaseline, id)
		}
	}
	bambuPrintBaselineMu.Unlock()
	for id := range lastPersistSig {
		if !activeIDs[id] {
			delete(lastPersistSig, id)
			delete(lastPGWrite, id)
		}
	}
}

func publishLiveTelemetry(printerID string, printer pmap) {
	if redisClient == nil || !redisClient.Enabled() || printerID == "" {
		return
	}
	t := make(map[string]any, len(telemetryFields)+1)
	for _, field := range telemetryFields {
		t[field] = printer[field]
	}
	t["lastUpdated"] = time.Now().UnixMilli()
	redisClient.Publish(printerID, t, telemetryTTL)
}
