package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const tempReachedTolerance = 2.0

// embedEvent bundles a Discord event key, its embed, and whether a snapshot
// should be attached.
type embedEvent struct {
	event           string
	embed           pmap
	includeSnapshot bool
}

// In-memory transition trackers (main-thread only).
var (
	printingSpools = map[string]map[string]bool{}
	runoutReported = map[string]bool{}
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func buildStatusTransitionEmbed(previous, next pmap) *embedEvent {
	prevStatus := mStr(previous, "status")
	nextStatus := mStr(next, "status")
	if prevStatus == nextStatus {
		return nil
	}
	name := firstNonEmpty(mStr(next, "name"), mStr(previous, "name"), "Printer")

	var title, description, statusValue, eventKey string
	var color int
	switch {
	case prevStatus == "offline" && nextStatus != "offline":
		title, description, statusValue = name+" Online", "Connection restored", "online"
		color, eventKey = discordColorForStatus("completed"), "printer_online"
	case prevStatus != "offline" && nextStatus == "offline":
		title, description, statusValue = name+" Offline", "Connection lost", "offline"
		color, eventKey = discordColorForStatus("offline"), "printer_offline"
	default:
		return nil
	}

	return &embedEvent{
		event: eventKey,
		embed: pmap{
			"title": title, "description": description, "color": color,
			"fields": []any{
				pmap{"name": "Printer", "value": name, "inline": true},
				pmap{"name": "Status", "value": statusValue, "inline": true},
			},
			"timestamp": isoTimestamp(),
		},
	}
}

func buildJobTransitionEvent(previous, next pmap) *embedEvent {
	previousJob := mMap(previous, "currentJob")
	nextJob := mMap(next, "currentJob")
	previousFilename := mStr(previousJob, "filename")
	nextFilename := mStr(nextJob, "filename")
	name := firstNonEmpty(mStr(next, "name"), mStr(previous, "name"), "Printer")

	if previousFilename == "" && nextFilename == "" {
		return nil
	}

	if previousFilename == "" && nextFilename != "" {
		return &embedEvent{
			event: "print_started",
			embed: pmap{
				"title": name + " Print Started", "description": nextFilename,
				"color":     discordColorForStatus("printing"),
				"fields":    []any{pmap{"name": "Printer", "value": name, "inline": true}},
				"timestamp": isoTimestamp(),
			},
			includeSnapshot: false,
		}
	}

	if previousFilename != "" && nextFilename == "" {
		if mStr(next, "status") == "offline" {
			return nil
		}
		rawPrintState := mStr(next, "rawPrintState")
		var title, description, eventKey string
		var color int
		var includeSnapshot bool
		if rawPrintState == "cancelled" || rawPrintState == "failed" {
			title = name + " Print Cancelled"
			description = previousFilename + "\nCancelled by printer state"
			color = discordColorForStatus("failed")
			includeSnapshot = true
			eventKey = "print_cancelled"
		} else {
			nextStatus := mStr(next, "status")
			if nextStatus != "error" {
				title = name + " Print Completed"
			} else {
				title = name + " Print Stopped"
			}
			description = previousFilename
			if nextStatus == "error" {
				color = discordColorForStatus("failed")
			} else {
				color = discordColorForStatus("completed")
			}
			includeSnapshot = nextStatus != "error"
			eventKey = "print_completed"
		}
		return &embedEvent{
			event: eventKey,
			embed: pmap{
				"title": title, "description": description, "color": color,
				"fields": []any{
					pmap{"name": "Printer", "value": name, "inline": true},
					pmap{"name": "Filament Used", "value": fmt.Sprintf("%s g", numFmt(mFloatDef(previousJob, "filamentUsed", 0))), "inline": true},
				},
				"timestamp": isoTimestamp(),
			},
			includeSnapshot: includeSnapshot,
		}
	}

	if previousFilename != nextFilename {
		return &embedEvent{
			event: "print_started",
			embed: pmap{
				"title":       name + " Print Job Switched",
				"description": previousFilename + " -> " + nextFilename,
				"color":       discordColorForStatus("printing"),
				"timestamp":   isoTimestamp(),
			},
			includeSnapshot: false,
		}
	}

	previousJobStatus := mStr(previousJob, "status")
	nextJobStatus := mStr(nextJob, "status")
	if previousJobStatus == nextJobStatus {
		return nil
	}
	var title, statusColor, eventKey string
	if previousJobStatus == "paused" && nextJobStatus == "printing" {
		title, statusColor, eventKey = name+" Print Resumed", "printing", "print_resumed"
	} else if previousJobStatus == "printing" && nextJobStatus == "paused" {
		title, statusColor, eventKey = name+" Print Paused", "paused", "print_paused"
	} else {
		return nil
	}
	return &embedEvent{
		event: eventKey,
		embed: pmap{
			"title": title, "description": nextFilename,
			"color": discordColorForStatus(statusColor),
			"fields": []any{
				pmap{"name": "Printer", "value": name, "inline": true},
				pmap{"name": "Progress", "value": fmt.Sprintf("%s%%", numFmt(mFloatDef(next, "progress", 0))), "inline": true},
			},
			"timestamp": isoTimestamp(),
		},
		includeSnapshot: false,
	}
}

func collectAnalyticsForTransition(ctx context.Context, conn *pgx.Conn, previous, next pmap) error {
	previousJob := mMap(previous, "currentJob")
	if previousJob == nil {
		return nil
	}
	nextJob := mMap(next, "currentJob")
	previousFilename := mStr(previousJob, "filename")
	nextFilename := mStr(nextJob, "filename")
	if nextJob != nil && nextFilename == previousFilename {
		return nil
	}
	nextStatus := mStr(next, "status")
	if nextStatus == "offline" {
		return nil
	}
	rawPrintState := mStr(next, "rawPrintState")
	outcome := "completed"
	if rawPrintState == "cancelled" || rawPrintState == "failed" || nextStatus == "error" {
		outcome = "failed"
	}
	printerID := firstNonEmpty(mStr(next, "id"), mStr(previous, "id"))
	return finalizeJobAnalytics(ctx, conn, previousJob, outcome, printerID)
}

func spoolIDSet(printer pmap) map[string]bool {
	out := map[string]bool{}
	for _, s := range asSlice(printer["spools"]) {
		sp := asMap(s)
		if sp == nil {
			continue
		}
		if id := mStr(sp, "id"); id != "" {
			out[id] = true
		}
	}
	return out
}

func checkFilamentRunout(next pmap) bool {
	printerID := mStr(next, "id")
	if printerID == "" {
		return false
	}
	runoutActive, _ := next["filamentRunout"].(bool)
	previouslyReported := runoutReported[printerID]
	runoutReported[printerID] = runoutActive
	hmsEdge := runoutActive && !previouslyReported

	if mStr(next, "status") != "printing" {
		delete(printingSpools, printerID)
		return hmsEdge
	}

	currentIDs := spoolIDSet(next)
	previousIDs, hadPrevious := printingSpools[printerID]
	printingSpools[printerID] = currentIDs

	disappeared := map[string]bool{}
	if hadPrevious {
		for id := range previousIDs {
			if !currentIDs[id] {
				disappeared[id] = true
			}
		}
	}
	activeSpoolID := mStr(next, "activeSpoolId")
	var spoolEdge bool
	if activeSpoolID != "" {
		spoolEdge = disappeared[activeSpoolID]
	} else {
		spoolEdge = len(disappeared) > 0
	}
	return hmsEdge || spoolEdge
}

func humanizeSpoolID(spoolID string) string {
	if spoolID == "" {
		return ""
	}
	if spoolID == "external" {
		return "External spool"
	}
	if strings.HasPrefix(spoolID, "tool-") {
		return "Lane " + spoolID[len("tool-"):]
	}
	if strings.HasPrefix(spoolID, "ams") {
		parts := strings.SplitN(spoolID[len("ams"):], "-", 2)
		if len(parts) == 2 {
			unit, err1 := strconv.Atoi(parts[0])
			tray, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return fmt.Sprintf("AMS %d slot %d", unit+1, tray+1)
			}
		}
		return spoolID
	}
	return spoolID
}

func buildFilamentRunoutEmbed(printer pmap) pmap {
	name := firstNonEmpty(mStr(printer, "name"), "Printer")
	job := mMap(printer, "currentJob")
	filename := mStr(job, "filename")
	fields := []any{pmap{"name": "Printer", "value": name, "inline": true}}
	if filename != "" {
		fields = append(fields, pmap{"name": "Job", "value": filename, "inline": true})
	}
	if slotLabel := humanizeSpoolID(mStr(printer, "activeSpoolId")); slotLabel != "" {
		fields = append(fields, pmap{"name": "Filament", "value": slotLabel, "inline": true})
	}
	return pmap{
		"title":       name + " Out of Filament",
		"description": "The filament feeding the current print was depleted or removed.",
		"color":       discordColorForStatus("failed"),
		"fields":      fields,
		"timestamp":   isoTimestamp(),
	}
}

type reachedTemp struct {
	label  string
	temp   float64
	target float64
}

func buildTempReachedEmbed(previous, next pmap) pmap {
	name := firstNonEmpty(mStr(next, "name"), mStr(previous, "name"), "Printer")
	var reached []reachedTemp

	prevNozzles := mSlice(previous, "nozzleTemperatures")
	nextNozzles := mSlice(next, "nozzleTemperatures")
	nextTargets := mSlice(next, "nozzleTargets")
	activeTargets := 0
	for _, t := range nextTargets {
		if f, ok := asFloat(t); ok && f > 0 {
			activeTargets++
		}
	}
	multiNozzle := activeTargets > 1

	for index, t := range nextTargets {
		target, ok := asFloat(t)
		if !ok || target <= 0 {
			continue
		}
		var nextTemp, prevTemp float64
		var nOK, pOK bool
		if index < len(nextNozzles) {
			nextTemp, nOK = asFloat(nextNozzles[index])
		}
		if index < len(prevNozzles) {
			prevTemp, pOK = asFloat(prevNozzles[index])
		}
		if !nOK || !pOK {
			continue
		}
		threshold := target - tempReachedTolerance
		if prevTemp < threshold && threshold <= nextTemp {
			label := "Nozzle"
			if multiNozzle {
				label = fmt.Sprintf("Nozzle %d", index+1)
			}
			reached = append(reached, reachedTemp{label, nextTemp, target})
		}
	}

	bedTarget, btOK := mFloat(next, "bedTarget")
	nextBed, nbOK := mFloat(mMap(next, "temperature"), "bed")
	prevBed, pbOK := mFloat(mMap(previous, "temperature"), "bed")
	if btOK && bedTarget > 0 && nbOK && pbOK {
		threshold := bedTarget - tempReachedTolerance
		if prevBed < threshold && threshold <= nextBed {
			reached = append(reached, reachedTemp{"Bed", nextBed, bedTarget})
		}
	}

	if len(reached) == 0 {
		return nil
	}

	fields := []any{pmap{"name": "Printer", "value": name, "inline": false}}
	var labels []string
	for _, r := range reached {
		fields = append(fields, pmap{"name": r.label, "value": fmt.Sprintf("%s°C / %s°C", numFmt(r.temp), numFmt(r.target)), "inline": true})
		labels = append(labels, r.label)
	}
	return pmap{
		"title":       name + " Reached Target Temperature",
		"description": strings.Join(labels, ", "),
		"color":       discordColorForStatus("printing"),
		"fields":      fields,
		"timestamp":   isoTimestamp(),
	}
}

func notifyForTransition(webhooks []pmap, previous, next pmap) {
	if len(webhooks) == 0 {
		return
	}
	if statusEvent := buildStatusTransitionEmbed(previous, next); statusEvent != nil {
		sendDiscordEmbed(webhooks, statusEvent.embed, statusEvent.event, nil)
	}
	if tempEmbed := buildTempReachedEmbed(previous, next); tempEmbed != nil {
		sendDiscordEmbed(webhooks, tempEmbed, "temp_target_reached", nil)
	}
	if checkFilamentRunout(next) {
		sendDiscordEmbed(webhooks, buildFilamentRunoutEmbed(next), "filament_runout", nil)
	}
	jobEvent := buildJobTransitionEvent(previous, next)
	if jobEvent == nil {
		return
	}
	var snapshot []byte
	if jobEvent.includeSnapshot {
		snapshot = fetchPrinterSnapshot(previous)
	}
	sendDiscordEmbed(webhooks, jobEvent.embed, jobEvent.event, snapshot)
}

// numFmt formats a number the way the Python f-strings render it: an integral
// value with no decimals, otherwise its shortest decimal form.
func numFmt(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
