package main

import (
	"fmt"
	"strconv"
	"strings"
)

func mapPrintStateToStatus(state string) string {
	switch state {
	case "printing":
		return "printing"
	case "paused":
		return "paused"
	case "error":
		return "error"
	}
	return "idle"
}

// buildCurrentJob builds the currentJob object from Moonraker print_stats.
func buildCurrentJob(printStats, previousJob pmap, progress int) pmap {
	if printStats == nil {
		return nil
	}
	state := mStr(printStats, "state")
	filename := mStr(printStats, "filename")
	if filename == "" || state == "" || state == "standby" || state == "complete" || state == "cancelled" {
		return nil
	}

	filamentUsedGrams := 0.0
	if v, ok := mFloat(printStats, "filament_used"); ok {
		filamentUsedGrams = round1(v / 1000 * 3)
	}

	printDuration, hasDuration := mFloat(printStats, "print_duration")
	printingTimeMinutes := 0.0
	if hasDuration && printDuration > 0 {
		printingTimeMinutes = maxF(0, round(printDuration/60))
	}
	estimatedTimeMinutes := 0.0
	timeRemainingMinutes := 0.0
	if hasDuration && printDuration > 0 && progress > 0 {
		estimatedTotalSeconds := printDuration / maxF(float64(progress)/100, 0.01)
		remainingSeconds := maxF(estimatedTotalSeconds-printDuration, 0)
		estimatedTimeMinutes = maxF(1, round(estimatedTotalSeconds/60))
		timeRemainingMinutes = maxF(0, round(remainingSeconds/60))
	}

	var startTime string
	if previousJob != nil && mStr(previousJob, "filename") == filename {
		startTime = mStr(previousJob, "startTime")
	} else {
		startTime = isoTimestamp()
	}

	status := "printing"
	if state == "paused" {
		status = "paused"
	} else if state == "error" {
		status = "failed"
	}

	return pmap{
		"id":            "job-" + filename,
		"filename":      filename,
		"status":        status,
		"progress":      float64(progress),
		"estimatedTime": estimatedTimeMinutes,
		"timeRemaining": timeRemainingMinutes,
		"printingTime":  printingTimeMinutes,
		"filamentUsed":  filamentUsedGrams,
		"startTime":     startTime,
		"priority":      "medium",
	}
}

func getReachableGenericStatus(printer pmap) string {
	cj := mMap(printer, "currentJob")
	if mStr(cj, "status") == "paused" || mStr(printer, "status") == "paused" {
		return "paused"
	}
	if mStr(cj, "status") == "printing" || mStr(printer, "status") == "printing" {
		return "printing"
	}
	if mStr(printer, "status") == "error" {
		return "error"
	}
	return "idle"
}

func fetchGenericStatus(printer pmap) (pmap, error) {
	header := parseHeaderString(mStr(printer, "apiKeyHeader"))
	if _, err := httpGet(mStr(printer, "url")+"/", header, requestTimeout); err != nil {
		return nil, err
	}
	status := getReachableGenericStatus(printer)
	var errorMessage any
	if status == "error" {
		errorMessage = "Printer reported an error"
	}
	return pmap{"status": status, "errorMessage": errorMessage}, nil
}

func fetchSnapmakerStatus(printer pmap) (pmap, error) {
	header := parseHeaderString(mStr(printer, "apiKeyHeader"))
	payload, err := getJSON(mStr(printer, "url")+snapmakerStatusPath, header, requestTimeout)
	if err != nil {
		return nil, err
	}
	status := mMap(asMap(mGet(payload, "result")), "status")
	printStats := mMap(status, "print_stats")
	if printStats == nil {
		return nil, fmt.Errorf("printer did not return the expected status JSON")
	}
	virtualSdcard := mMap(status, "virtual_sdcard")

	extruders := []pmap{
		mMap(status, "extruder"),
		mMap(status, "extruder1"),
		mMap(status, "extruder2"),
		mMap(status, "extruder3"),
	}
	heaterBed := mMap(status, "heater_bed")
	fallbackNozzle := mFloatDef(mMap(printer, "temperature"), "nozzle", 0)
	existingNozzles := mSlice(printer, "nozzleTemperatures")
	existingTargets := mSlice(printer, "nozzleTargets")

	nozzleTemperatures := []any{}
	nozzleTargets := []any{}
	for index, extruder := range extruders {
		if t, ok := mFloat(extruder, "temperature"); ok {
			nozzleTemperatures = append(nozzleTemperatures, round(t))
		} else if index < len(existingNozzles) {
			nozzleTemperatures = append(nozzleTemperatures, existingNozzles[index])
		} else {
			nozzleTemperatures = append(nozzleTemperatures, fallbackNozzle)
		}

		if tg, ok := mFloat(extruder, "target"); ok {
			nozzleTargets = append(nozzleTargets, round(tg))
		} else if index < len(existingTargets) {
			nozzleTargets = append(nozzleTargets, existingTargets[index])
		} else {
			nozzleTargets = append(nozzleTargets, float64(0))
		}
	}

	bedTemperature, ok := mFloat(heaterBed, "temperature")
	if !ok {
		bedTemperature = mFloatDef(mMap(printer, "temperature"), "bed", 0)
	}
	bedTargetVal, ok := mFloat(heaterBed, "target")
	if !ok {
		bedTargetVal = mFloatDef(printer, "bedTarget", 0)
	}

	rawPrintState := mStr(printStats, "state")
	var errorMessage any
	rawMessage := mStr(printStats, "message")
	if rawPrintState == "error" {
		if strings.TrimSpace(rawMessage) != "" {
			errorMessage = truncate(strings.TrimSpace(rawMessage), 500)
		} else {
			errorMessage = "Printer reported an error"
		}
	}

	progress := 0
	if rp, ok := mFloat(virtualSdcard, "progress"); ok {
		progress = clampInt(round(rp*100), 0, 100)
	}

	fan := mMap(status, "fan")
	var fanSpeeds any
	if fs, ok := mFloat(fan, "speed"); ok {
		fanSpeeds = []any{pmap{"id": "part", "speed": float64(clampInt(round(fs*100), 0, 100))}}
	} else {
		fanSpeeds = printer["fanSpeeds"]
	}

	var activeSpoolID any
	activeExtruder := mStr(mMap(status, "toolhead"), "extruder")
	if strings.HasPrefix(activeExtruder, "extruder") {
		suffix := activeExtruder[len("extruder"):]
		activeIndex := 0
		if suffix != "" {
			if n, err := strconv.Atoi(suffix); err == nil {
				activeIndex = n
			}
		}
		activeSpoolID = fmt.Sprintf("tool-%d", activeIndex+1)
	}

	nozzleForTemp := fallbackNozzle
	if len(nozzleTemperatures) > 0 {
		if v, ok := asFloat(nozzleTemperatures[0]); ok {
			nozzleForTemp = v
		}
	}

	return pmap{
		"status":     mapPrintStateToStatus(rawPrintState),
		"currentJob": buildCurrentJob(printStats, mMap(printer, "currentJob"), progress),
		"progress":   float64(progress),
		"rawPrintState": func() any {
			if rawPrintState == "" {
				return nil
			}
			return rawPrintState
		}(),
		"temperature": pmap{
			"nozzle": nozzleForTemp,
			"bed":    round(bedTemperature),
		},
		"nozzleTemperatures": nozzleTemperatures,
		"nozzleTargets":      nozzleTargets,
		"bedTarget":          round(bedTargetVal),
		"fanSpeeds":          fanSpeeds,
		"errorMessage":       errorMessage,
		"activeSpoolId":      activeSpoolID,
	}, nil
}

func buildSpoolsFromTaskConfig(taskConfig pmap) any {
	if taskConfig == nil {
		return nil
	}
	filamentTypes := mSlice(taskConfig, "filament_type")
	filamentColors := mSlice(taskConfig, "filament_color_rgba")
	filamentExists := mSlice(taskConfig, "filament_exist")
	if len(filamentTypes) == 0 {
		return nil
	}

	spools := []any{}
	for index, ft := range filamentTypes {
		if len(filamentExists) == 0 || index >= len(filamentExists) || !truthy(filamentExists[index]) {
			continue
		}
		colorRgba := "808080FF"
		if index < len(filamentColors) {
			colorRgba = fmt.Sprintf("%v", filamentColors[index])
		}
		hexColor := "#" + firstN(colorRgba, 6)
		material := "Unknown"
		if s, ok := ft.(string); ok && s != "" {
			material = s
		}
		spools = append(spools, pmap{
			"id":        fmt.Sprintf("tool-%d", index+1),
			"color":     hexColor,
			"material":  material,
			"remaining": float64(0),
			"weight":    float64(0),
		})
	}
	if len(spools) == 0 {
		return nil
	}
	return spools
}

func fetchSnapmakerTaskConfig(printer pmap) (any, error) {
	header := parseHeaderString(mStr(printer, "apiKeyHeader"))
	payload, err := getJSON(mStr(printer, "url")+"/printer/objects/query?print_task_config", header, requestTimeout)
	if err != nil {
		return nil, err
	}
	status := mMap(asMap(mGet(payload, "result")), "status")
	taskConfig := mMap(status, "print_task_config")
	return buildSpoolsFromTaskConfig(taskConfig), nil
}

// ── small value helpers ──

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// truthy mirrors Python truthiness for filament_exist entries (bool / number / non-empty).
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != ""
	case nil:
		return false
	}
	return true
}
