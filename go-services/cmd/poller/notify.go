package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

func discordColorForStatus(status string) int {
	switch strings.ToLower(status) {
	case "printing":
		return 0x3B82F6
	case "paused":
		return 0xFACC15
	case "idle":
		return 0x22C55E
	case "offline":
		return 0xEF4444
	case "error":
		return 0xEF4444
	case "completed":
		return 0x22C55E
	case "failed":
		return 0xEF4444
	}
	return 0x5865F2
}

// webhookWants reports whether a webhook should receive an event: a disabled
// webhook never does; events == nil (not a list) means all events; a list
// restricts to the listed keys.
func webhookWants(webhook pmap, eventKey string) bool {
	if enabled, ok := webhook["enabled"].(bool); ok && !enabled {
		return false
	}
	events, ok := webhook["events"].([]any)
	if !ok {
		return true
	}
	for _, e := range events {
		if s, ok := e.(string); ok && s == eventKey {
			return true
		}
	}
	return false
}

func ttsContentForEmbed(embed pmap) string {
	spoken := strings.TrimSpace(mStr(embed, "title"))
	if spoken == "" {
		spoken = "Print farm notification"
	}
	return truncate(spoken, 2000)
}

func sendDiscordEmbed(webhooks []pmap, embed pmap, eventKey string, snapshot []byte) {
	if embed == nil {
		return
	}
	for _, webhook := range webhooks {
		webhookURL := mStr(webhook, "webhookUrl")
		if webhookURL == "" || !webhookWants(webhook, eventKey) {
			continue
		}
		username := mStr(webhook, "name")
		if username == "" {
			username = "PrintFarm Bot"
		}
		ttsOn, _ := webhook["tts"].(bool)

		var err error
		switch {
		case ttsOn:
			err = postJSON(webhookURL, pmap{
				"username": username,
				"tts":      true,
				"content":  ttsContentForEmbed(embed),
			})
		case snapshot != nil:
			embedWithImage := clone(embed)
			embedWithImage["image"] = pmap{"url": "attachment://snapshot.jpg"}
			err = postSnapshot(webhookURL, pmap{
				"username": username,
				"embeds":   []any{embedWithImage},
			}, snapshot)
		default:
			err = postJSON(webhookURL, pmap{
				"username": username,
				"embeds":   []any{embed},
			})
		}
		if err != nil {
			log.Printf("discord webhook error (%s): %v", mStr(webhook, "name"), err)
		}
	}
}

func postJSON(webhookURL string, payload pmap) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func postSnapshot(webhookURL string, payload pmap, snapshot []byte) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("payload_json", string(payloadJSON)); err != nil {
		return err
	}
	part, err := mw.CreatePart(fileHeader("file", "snapshot.jpg", "image/jpeg"))
	if err != nil {
		return err
	}
	if _, err := part.Write(snapshot); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Post(webhookURL, mw.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func fileHeader(field, filename, contentType string) map[string][]string {
	return map[string][]string{
		"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename)},
		"Content-Type":        {contentType},
	}
}

// ── snapshots ────────────────────────────────────────────────────────────────

// grabMJPEGFrame pulls the first complete JPEG frame out of a
// multipart/x-mixed-replace MJPEG stream and drops the connection.
func grabMJPEGFrame(streamURL string) []byte {
	client := &http.Client{Timeout: bambuSnapshotTimeout}
	resp, err := client.Get(streamURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	buffer := make([]byte, 0, 65536)
	chunk := make([]byte, 16384)
	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
		}
		headerEnd := bytes.Index(buffer, []byte("\r\n\r\n"))
		if headerEnd == -1 {
			if len(buffer) > 65536 {
				return nil
			}
		} else {
			m := mjpegContentLengthRe.FindSubmatch(buffer[:headerEnd])
			if m == nil {
				return nil
			}
			length, ok := parseFloat(string(m[1]))
			if !ok || length <= 0 || int(length) > maxFrameBytes {
				return nil
			}
			bodyStart := headerEnd + 4
			if len(buffer)-bodyStart >= int(length) {
				out := make([]byte, int(length))
				copy(out, buffer[bodyStart:bodyStart+int(length)])
				return out
			}
		}
		if readErr != nil {
			return nil
		}
	}
}

func fetchBambuSnapshot(printer pmap) []byte {
	printerID := mStr(printer, "id")
	if printerID == "" {
		return nil
	}
	webcamBase := fmt.Sprintf("%s/__printer_webcam/%s", webSnapshotBaseURL, url.PathEscape(printerID))
	if bambuRtspProfiles[mStr(printer, "profile")] {
		return grabMJPEGFrame(webcamBase + "/stream.mjpg")
	}
	client := &http.Client{Timeout: bambuSnapshotTimeout}
	resp, err := client.Get(webcamBase + "/snapshot.jpg")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	return data
}

func fetchPrinterSnapshot(printer pmap) []byte {
	if bambuProfiles[mStr(printer, "profile")] {
		return fetchBambuSnapshot(printer)
	}
	header := parseHeaderString(mStr(printer, "apiKeyHeader"))
	body, err := httpGet(mStr(printer, "url")+"/webcam/snapshot.jpg", header, requestTimeout)
	if err != nil {
		return nil
	}
	return body
}
