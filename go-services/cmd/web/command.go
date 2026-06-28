package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// command.go ports POST /api/printers/:id/command from server/app.js: the Bambu
// MQTT command surface. Bambu printers have no HTTP control API, so pause/resume/
// cancel, temperatures, gcode, fans, the chamber light, the H2 air duct, and AMS
// filament actions are published as MQTT messages to device/<serial>/request over
// a short-lived publish-only TLS connection (port 8883, user bblp, pass = the LAN
// access code stored in api_key_header).

// ── ordered JSON object (preserves Node's JSON.stringify key order) ──────────

type ojField struct {
	k string
	v any
}
type ojson []ojField

func (o ojson) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(f.k)
		b.Write(kb)
		b.WriteByte(':')
		vb := marshalJSON(f.v)
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ── command constants (mirror server/app.js) ─────────────────────────────────

var bambuPrintActions = map[string]string{"pause": "pause", "resume": "resume", "cancel": "stop"}

type filamentPreset struct {
	idx, typ string
	min, max int
}

var bambuFilamentPresets = map[string]filamentPreset{
	"PLA":  {"GFL99", "PLA", 190, 230},
	"PETG": {"GFG99", "PETG", 230, 260},
	"ABS":  {"GFB99", "ABS", 240, 270},
	"ASA":  {"GFB98", "ASA", 240, 270},
	"TPU":  {"GFU99", "TPU", 200, 240},
	"PC":   {"GFC99", "PC", 260, 280},
	"PA":   {"GFN99", "PA", 260, 290},
	"PVA":  {"GFS99", "PVA", 190, 220},
}

var bambuLightNodesByProfile = map[string][]string{
	"bambulab_h2s": {"chamber_light", "chamber_light2"},
	"bambulab_h2d": {"chamber_light", "chamber_light2"},
	"bambulab_h2c": {"chamber_light", "chamber_light2"},
}

func bambuLightNodes(profile string) []string {
	if nodes, ok := bambuLightNodesByProfile[profile]; ok {
		return nodes
	}
	return []string{"chamber_light"}
}

// allowedMotionGcode mirrors /^(?:G0|G1|G28|G90|G91|M84|M18)\b/i.
var allowedMotionPrefixes = []string{"G0", "G1", "G28", "G90", "G91", "M84", "M18"}

// ── HTTP handler ─────────────────────────────────────────────────────────────

// handlePrinterCommand backs POST /api/printers/:id/command. A validation or
// MQTT error is surfaced as 500 { error: <message> }, mirroring Node where the
// throw propagates to the top-level catch (which 500s with the message).
func handlePrinterCommand(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	id := decodePathSegment(req.URL.Path, "/api/printers/", "/command")
	printer, err := getPrinterConn(ctx, id)
	if err != nil {
		internalError(w, "getPrinterConn", err)
		return
	}
	if printer == nil {
		sendJSON(w, http.StatusNotFound, map[string]any{"error": "Printer not found"}, "")
		return
	}

	body := decodeBodyMap(req)
	// Mirror JS: an unsupported-command error interpolates the raw value via
	// template string, so a missing key renders "undefined", JSON null "null",
	// etc. A non-string command can't match any (string) known command anyway.
	command := commandDisplay(body, "command")

	if err := sendBambuCommand(printer, command, body); err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()}, "")
		return
	}
	sendEmpty(w, http.StatusNoContent)
}

// ── payload builders ─────────────────────────────────────────────────────────

// buildBambuCommandPayload mirrors the Node builder: it returns one or more MQTT
// payloads, or an error for an unsupported command / out-of-range param.
func buildBambuCommandPayload(command string, params map[string]any, profile string) ([]any, error) {
	sequenceID := strconv.FormatInt(time.Now().UnixMilli()%1000000, 10)

	switch command {
	case "light_on", "light_off":
		on := command == "light_on"
		nodes := bambuLightNodes(profile)
		out := make([]any, 0, len(nodes))
		for i, node := range nodes {
			seq := strconv.FormatInt((time.Now().UnixMilli()+int64(i))%1000000, 10)
			out = append(out, buildBambuLedPayload(node, on, seq))
		}
		return out, nil

	case "set_temperature":
		param, err := buildBambuTemperatureGcode(strField(params, "heater"), numField(params, "target"), nozzleIndexField(params))
		if err != nil {
			return nil, err
		}
		return single(gcodeLinePayload(param, sequenceID)), nil

	case "gcode":
		param, err := sanitizeMotionGcode(params["gcode"])
		if err != nil {
			return nil, err
		}
		return single(gcodeLinePayload(param, sequenceID)), nil

	case "set_fan":
		port := numField(params, "fanPort")
		speed := math.Round(numField(params, "speed"))
		if !isIntegerValue(port) || port < 1 || port > 3 {
			return nil, fmt.Errorf("Fan port is out of range")
		}
		if math.IsNaN(speed) || math.IsInf(speed, 0) || speed < 0 || speed > 255 {
			return nil, fmt.Errorf("Fan speed is out of range")
		}
		param := fmt.Sprintf("M106 P%s S%s\n", jsInt(port), jsInt(speed))
		return single(gcodeLinePayload(param, sequenceID)), nil

	case "set_airduct":
		modeID := numField(params, "modeId")
		if !isIntegerValue(modeID) || modeID < 0 || modeID > 3 {
			return nil, fmt.Errorf("Air duct mode is out of range")
		}
		submode := -1.0
		if _, present := params["submode"]; present {
			submode = numField(params, "submode")
		}
		if !isIntegerValue(submode) {
			return nil, fmt.Errorf("Air duct submode must be an integer")
		}
		return single(ojson{
			{"print", ojson{
				{"command", "set_airduct"},
				{"modeId", int(modeID)},
				{"submode", int(submode)},
				{"sequence_id", sequenceID},
			}},
		}), nil

	case "load_filament", "unload_filament":
		isUnload := command == "unload_filament"
		var target float64
		if isUnload {
			target = 255
		} else {
			target = numField(params, "trayId")
		}
		if math.IsNaN(target) || math.IsInf(target, 0) || target < 0 || target > 255 {
			return nil, fmt.Errorf("Filament tray target is out of range")
		}
		tarTemp := 0.0
		if !isUnload {
			t := numField(params, "target")
			if t == 0 || math.IsNaN(t) {
				t = 220
			}
			tarTemp = math.Round(t)
		}
		return single(ojson{
			{"print", ojson{
				{"command", "ams_change_filament"},
				{"target", jsIntVal(target)},
				{"curr_temp", 0},
				{"tar_temp", jsIntVal(tarTemp)},
				{"sequence_id", sequenceID},
			}},
		}), nil

	case "set_filament":
		target := numField(params, "trayId")
		if math.IsNaN(target) || math.IsInf(target, 0) || target < 0 || target > 255 {
			return nil, fmt.Errorf("Filament tray target is out of range")
		}
		isExternal := target == 254
		amsID := 255
		trayID := 254
		if !isExternal {
			amsID = int(math.Floor(target / 4))
			trayID = int(math.Mod(target, 4))
		}
		typ := strings.TrimSpace(strings.ToUpper(strField(params, "type")))
		preset, ok := bambuFilamentPresets[typ]
		if !ok {
			preset = bambuFilamentPresets["PLA"]
		}
		colorRaw := strField(params, "color")
		if colorRaw == "" {
			colorRaw = "#808080"
		}
		// Node uses String#replace('#', '') which strips only the first '#'.
		color := strings.ToUpper(sliceStr(strings.Replace(colorRaw, "#", "", 1), 6))
		trayColor := padEnd(color, 6, '0') + "FF"
		vendor := sliceStr(strings.TrimSpace(stripNonPrintable(strField(params, "vendor"))), 32)
		return single(ojson{
			{"print", ojson{
				{"command", "ams_filament_setting"},
				{"ams_id", amsID},
				{"tray_id", trayID},
				{"tray_info_idx", preset.idx},
				{"tray_color", trayColor},
				{"nozzle_temp_min", preset.min},
				{"nozzle_temp_max", preset.max},
				{"tray_type", preset.typ},
				{"tray_id_name", vendor},
				{"setting_id", ""},
				{"sequence_id", sequenceID},
			}},
		}), nil
	}

	action, ok := bambuPrintActions[command]
	if !ok {
		return nil, fmt.Errorf("Unsupported command: %s", command)
	}
	pc := ojson{{"command", action}, {"sequence_id", sequenceID}}
	if action == "stop" {
		pc = append(pc, ojField{"param", ""})
	}
	return single(ojson{{"print", pc}}), nil
}

func buildBambuLedPayload(node string, on bool, sequenceID string) ojson {
	mode := "off"
	if on {
		mode = "on"
	}
	return ojson{
		{"system", ojson{
			{"sequence_id", sequenceID},
			{"command", "ledctrl"},
			{"led_node", node},
			{"led_mode", mode},
			{"led_on_time", 500},
			{"led_off_time", 500},
			{"loop_times", 0},
			{"interval_time", 0},
		}},
	}
}

func gcodeLinePayload(param, sequenceID string) ojson {
	return ojson{
		{"print", ojson{
			{"command", "gcode_line"},
			{"param", param},
			{"sequence_id", sequenceID},
		}},
	}
}

func buildBambuTemperatureGcode(heater string, target float64, nozzleIndex float64) (string, error) {
	value := math.Round(target)
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 350 {
		return "", fmt.Errorf("Temperature target is out of range")
	}
	switch heater {
	case "nozzle":
		tool := ""
		if nozzleIndex > 0 {
			tool = " T" + jsInt(nozzleIndex)
		}
		return fmt.Sprintf("M104%s S%s\n", tool, jsInt(value)), nil
	case "bed":
		return fmt.Sprintf("M140 S%s\n", jsInt(value)), nil
	case "chamber":
		if value > 60 {
			return "", fmt.Errorf("Chamber temperature target is out of range")
		}
		return fmt.Sprintf("M141 S%s\n", jsInt(value)), nil
	}
	return "", fmt.Errorf("Unsupported heater: %s", heater)
}

func sanitizeMotionGcode(v any) (string, error) {
	gcode, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("gcode must be a string")
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(gcode, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) == 0 || len(lines) > 8 {
		return "", fmt.Errorf("gcode must contain between 1 and 8 commands")
	}
	for _, line := range lines {
		if !matchesMotionPrefix(line) {
			return "", fmt.Errorf("Disallowed motion command: %s", line)
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// matchesMotionPrefix mirrors /^(?:G0|G1|G28|G90|G91|M84|M18)\b/i: a case-
// insensitive prefix followed by a word boundary (end of string or a non-word
// char — a word char being [A-Za-z0-9_]).
func matchesMotionPrefix(line string) bool {
	upper := strings.ToUpper(line)
	for _, p := range allowedMotionPrefixes {
		if strings.HasPrefix(upper, p) {
			rest := upper[len(p):]
			if rest == "" || !isWordByte(rest[0]) {
				return true
			}
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// ── MQTT publish (short-lived, publish-only) ─────────────────────────────────

func sendBambuCommand(printer *printerConn, command string, params map[string]any) error {
	payloads, err := buildBambuCommandPayload(command, params, printer.Profile)
	if err != nil {
		return err
	}
	serial := strings.TrimSpace(printer.Serial)
	if serial == "" {
		return fmt.Errorf("Bambu printer is missing its serial number")
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("ssl://%s:8883", printer.IPAddress))
	opts.SetUsername("bblp")
	opts.SetPassword(strings.TrimSpace(printer.APIKeyHeader))
	opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
	opts.SetClientID(fmt.Sprintf("printfarm-web-%s-%d", serial, time.Now().UnixNano()))
	opts.SetConnectTimeout(4 * time.Second)
	opts.SetAutoReconnect(false)
	opts.SetConnectRetry(false)

	client := mqtt.NewClient(opts)
	deadline := time.Now().Add(6 * time.Second)

	tok := client.Connect()
	if !tok.WaitTimeout(time.Until(deadline)) {
		client.Disconnect(0)
		return fmt.Errorf("MQTT command timed out")
	}
	if err := tok.Error(); err != nil {
		client.Disconnect(0)
		return err
	}

	topic := fmt.Sprintf("device/%s/request", serial)
	var pubs []mqtt.Token
	for _, payload := range payloads {
		pubs = append(pubs, client.Publish(topic, 0, false, marshalJSON(payload)))
	}
	for _, t := range pubs {
		if !t.WaitTimeout(time.Until(deadline)) {
			client.Disconnect(0)
			return fmt.Errorf("MQTT command timed out")
		}
		if err := t.Error(); err != nil {
			client.Disconnect(250)
			return err
		}
	}
	// Graceful disconnect flushes queued packets before the socket closes.
	client.Disconnect(250)
	return nil
}

// ── value helpers (mirror JS Number()/String() coercion) ─────────────────────

func single(v any) []any { return []any{v} }

// commandDisplay renders body[key] the way JS template-string interpolation does,
// so the "Unsupported command: …" message matches: an absent key → "undefined",
// JSON null → "null", a string → itself, bool → "true"/"false", number → its JS
// string form.
func commandDisplay(m map[string]any, key string) string {
	v, present := m[key]
	if !present {
		return "undefined"
	}
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return ""
	}
}

func strField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// numField mirrors Number(params[key]) with the absent-vs-null distinction:
// an absent key is undefined → NaN; an explicit JSON null → 0.
func numField(m map[string]any, key string) float64 {
	v, present := m[key]
	if !present {
		return math.NaN()
	}
	return jsNumber(v)
}

// nozzleIndexField mirrors the `nozzleIndex = 0` default param: an absent value
// (undefined) defaults to 0 rather than NaN.
func nozzleIndexField(m map[string]any) float64 {
	if _, present := m["nozzleIndex"]; !present {
		return 0
	}
	return jsNumber(m["nozzleIndex"])
}

// jsNumber mirrors JS Number(): number→itself, null→0, bool→0/1, string→parsed
// (trimmed; ""→0; invalid→NaN), anything else→NaN.
func jsNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return math.NaN()
		}
		return f
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN()
		}
		return f
	default:
		return math.NaN()
	}
}

// isIntegerValue mirrors Number.isInteger: finite and whole.
func isIntegerValue(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f)
}

// jsInt formats a whole float the way JS string-concatenation renders an integer
// number (no decimal point).
func jsInt(f float64) string {
	return strconv.FormatInt(int64(f), 10)
}

// jsIntVal returns an int for a whole value so it JSON-encodes without a decimal
// point (the inputs here are range-validated integers).
func jsIntVal(f float64) int { return int(f) }

func sliceStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func padEnd(s string, n int, pad byte) string {
	for len(s) < n {
		s += string(pad)
	}
	return s
}

func stripNonPrintable(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x20 && s[i] <= 0x7e {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
