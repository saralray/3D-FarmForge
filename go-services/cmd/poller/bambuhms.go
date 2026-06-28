package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
)

// bambu_hms_en.json.gz is vendored from ha-bambulab; embedded so the distroless
// image needs no sidecar file.
//
//go:embed bambu_hms_en.json.gz
var bambuHMSGz []byte

// Run-out short codes and severities (parity with the Python poller).
var bambuRunoutShortCodes = map[string]bool{"0300_8015": true}

var bambuHMSSeverity = map[int]string{1: "fatal", 2: "serious", 3: "common", 4: "info"}

var bambuHMSModules = map[int]string{
	0x03: "Printer",
	0x05: "Mainboard",
	0x07: "AMS",
	0x08: "Toolhead",
	0x0C: "Camera",
}

var bambuHMSText = map[string]string{
	"0300_9600_0001_0001": "The front door is open; the print was paused",
	"0300_9600_0003_0001": "The front door is open",
	"0300_9700_0001_0001": "The top cover is open; the print was paused",
	"0300_9700_0003_0001": "The top cover is open",
	"0300_A100_0001_0001": "Chamber temperature is too high; open the cover/door to cool down",
	"0300_A700_0003_0001": "Chamber temperature is high or air filtration is on; the exhaust fan sped up. Open the front door/top cover or lower the ambient temperature",
	"12FF_2000_0002_0002": "The external spool holder is empty; insert new filament",
	"0300_1A00_0002_0002": "The nozzle is clogged with filament",
	"0300_1A00_0002_0001": "The nozzle is covered with filament, or the build plate is crooked",
	"0300_1200_0002_0001": "The toolhead front cover fell off",
}

var bambuHMSAttrText = map[string]string{
	"0300_0100": "Heatbed temperature is abnormal; heating was stopped",
	"0300_0200": "Nozzle temperature is abnormal; heating was stopped",
	"0300_1E00": "Left nozzle temperature is abnormal; heating was stopped",
	"0300_9300": "Chamber temperature sensor is abnormal",
	"0300_0900": "Extrusion is abnormal; the nozzle may be clogged or the filament tangled",
}

var bambuAMSFamilyText = map[int]string{
	0x40: "AMS filament-buffer position signal lost; the cable or sensor may be faulty",
	0x50: "AMS communication is abnormal; check the connection cable",
	0x55: "AMS PTFE-tube connection order is incorrect; check the buffer-to-extruder tubing",
	0x60: "AMS slot is overloaded; the filament may be tangled or the spool stuck",
	0x70: "Failed to pull filament from the extruder; it may be clogged or the filament broken",
	0x98: "AMS power-adapter voltage is too low; replace the adapter",
}

var bambuAMSFilamentText = map[int]string{
	0x0001: "AMS: filament has run out",
	0x0002: "AMS: the slot is empty",
	0x0004: "AMS: the filament may be broken in the toolhead",
}

var bambuAMSSuppressedFamilies = map[int]bool{0x25: true}

// bambuHMSFullTable is the comprehensive ha-bambulab table, keyed by the
// 16-hex-digit code (the four segments concatenated, uppercase).
var bambuHMSFullTable = map[string]string{}

func loadBambuHMSTable() {
	gr, err := gzip.NewReader(bytes.NewReader(bambuHMSGz))
	if err != nil {
		log.Printf("bambu hms table load failed: %v", err)
		return
	}
	defer gr.Close()
	data, err := io.ReadAll(gr)
	if err != nil {
		log.Printf("bambu hms table load failed: %v", err)
		return
	}
	var top struct {
		DeviceHMS map[string]json.RawMessage `json:"device_hms"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		log.Printf("bambu hms table load failed: %v", err)
		return
	}
	for key, raw := range top.DeviceHMS {
		text, ok := firstObjectKey(raw)
		if ok && strings.TrimSpace(text) != "" {
			bambuHMSFullTable[strings.ToUpper(key)] = strings.TrimSpace(text)
		}
	}
}

// firstObjectKey returns the first key of a JSON object (mirroring Python's
// next(iter(value)), which relies on dict insertion order).
func firstObjectKey(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return "", false
	}
	tok, err = dec.Token()
	if err != nil {
		return "", false
	}
	key, ok := tok.(string)
	return key, ok
}

func init() { loadBambuHMSTable() }

func coerceHMSInt(value any) int {
	switch v := value.(type) {
	case string:
		cleaned := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(v)), "0x", "")
		if cleaned == "" {
			return 0
		}
		n, err := strconv.ParseInt(cleaned, 16, 64)
		if err != nil {
			return 0
		}
		return int(n)
	default:
		if f, ok := asFloat(value); ok {
			return int(f)
		}
	}
	return 0
}

func hmsFourSegment(attr, code int) string {
	return fmt.Sprintf("%04X_%04X_%04X_%04X",
		(attr>>16)&0xFFFF, attr&0xFFFF, (code>>16)&0xFFFF, code&0xFFFF)
}

func hmsAttrSegment(attr int) string {
	return fmt.Sprintf("%04X_%04X", (attr>>16)&0xFFFF, attr&0xFFFF)
}

func isBambuRunoutCode(shortCode string) bool {
	return strings.HasSuffix(shortCode, "_8011") || bambuRunoutShortCodes[shortCode]
}

// bambuHMSCodes returns active HMS fault codes ("MMMM_EEEE") in report order.
func bambuHMSCodes(printData pmap) []string {
	var codes []string
	seen := map[string]bool{}
	for _, item := range mSlice(printData, "hms") {
		hms := asMap(item)
		if hms == nil {
			continue
		}
		attr := coerceHMSInt(hms["attr"])
		code := coerceHMSInt(hms["code"])
		if code < 0x4000 {
			continue
		}
		short := fmt.Sprintf("%04X_%04X", (attr>>16)&0xFFFF, code&0xFFFF)
		if !seen[short] {
			seen[short] = true
			codes = append(codes, short)
		}
	}
	return codes
}

func bambuFilamentRunout(printData pmap) bool {
	shortCodes := map[string]bool{}
	for _, c := range bambuHMSCodes(printData) {
		shortCodes[c] = true
	}
	if pe, ok := mFloat(printData, "print_error"); ok && pe != 0 {
		pei := int(pe)
		errWord := pei & 0xFFFF
		if errWord >= 0x4000 {
			shortCodes[fmt.Sprintf("%04X_%04X", (pei>>16)&0xFFFF, errWord)] = true
		}
	}
	for code := range shortCodes {
		if isBambuRunoutCode(code) {
			return true
		}
	}
	return false
}

func bambuAMSFamily(attr int) int { return (attr >> 8) & 0xFF }

func bambuHMSTextFor(attr, code int) string {
	full := hmsFourSegment(attr, code)
	if exact, ok := bambuHMSText[full]; ok {
		return exact
	}
	if vendored, ok := bambuHMSFullTable[strings.ReplaceAll(full, "_", "")]; ok {
		return vendored
	}
	if family, ok := bambuHMSAttrText[hmsAttrSegment(attr)]; ok {
		return family
	}
	module := bambuHMSModules[(attr>>24)&0xFF]
	if module == "" {
		module = "Printer"
	}
	if module == "AMS" {
		amsFamily := bambuAMSFamily(attr)
		if amsFamily == 0x20 {
			if t, ok := bambuAMSFilamentText[code&0xFFFF]; ok {
				return t
			}
			return "AMS filament error"
		}
		if famText, ok := bambuAMSFamilyText[amsFamily]; ok {
			return famText
		}
	}
	return module + " fault"
}

// bambuErrorMessage summarises currently-active HMS faults, or "" when none.
func bambuErrorMessage(printData pmap) string {
	hmsList := mSlice(printData, "hms")
	if hmsList == nil {
		return ""
	}
	var parts []string
	seen := map[string]bool{}
	for _, item := range hmsList {
		hms := asMap(item)
		if hms == nil {
			continue
		}
		attr := coerceHMSInt(hms["attr"])
		code := coerceHMSInt(hms["code"])
		if code < 0x4000 {
			continue
		}
		severity := (code >> 16) & 0xFFFF
		if severity != 1 && severity != 2 && severity != 3 {
			continue
		}
		if (attr>>24)&0xFF == 0x07 && bambuAMSSuppressedFamilies[bambuAMSFamily(attr)] {
			continue
		}
		full := hmsFourSegment(attr, code)
		if seen[full] {
			continue
		}
		seen[full] = true
		parts = append(parts, fmt.Sprintf("%s (HMS_%s)", bambuHMSTextFor(attr, code), full))
	}
	if len(parts) == 0 {
		return ""
	}
	return truncate(strings.Join(parts, "; "), 500)
}

const bambuDoorOpenBit = 0x00800000

func bambuDoorOpen(printData pmap) bool {
	if stat, ok := mGet(printData, "stat").(string); ok && strings.TrimSpace(stat) != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(stat), 16, 64); err == nil {
			return (int(n) & bambuDoorOpenBit) != 0
		}
	}
	if hf, ok := mFloat(printData, "home_flag"); ok {
		return (int(hf) & bambuDoorOpenBit) != 0
	}
	return false
}

// buildBambuErrorMessage combines HMS faults with a live door/cover-open notice
// for models with a cover sensor. Returns nil (any) when all clear.
func buildBambuErrorMessage(printData pmap, profile, printerID string) any {
	if bambuDoorDebug && bambuDoorProfiles[profile] {
		log.Printf("[door-debug] %s profile=%s stat=%v home_flag=%v hw_switch_state=%v open=%v",
			printerID, profile, printData["stat"], printData["home_flag"],
			printData["hw_switch_state"], bambuDoorOpen(printData))
	}
	var messages []string
	hmsMessage := bambuErrorMessage(printData)
	if hmsMessage != "" {
		messages = append(messages, hmsMessage)
	}
	doorInHMS := hmsMessage != "" && strings.Contains(strings.ToLower(hmsMessage), "open")
	if bambuDoorProfiles[profile] && !doorInHMS && bambuDoorOpen(printData) {
		messages = append(messages, "The chamber door / top cover is open")
	}
	if len(messages) == 0 {
		return nil
	}
	return strings.Join(messages, "; ")
}

var _ = bambuHMSSeverity // retained for parity/reference with the Python table
