package main

// Command poller is the Go port of poller/printer_status_poller.py: it polls
// every printer this shard owns (generic HTTP ping, Snapmaker Moonraker, or Bambu
// MQTT-over-TLS), writes changed telemetry to Postgres, mirrors live state to
// optional Redis, fires Discord notifications on transitions, and records
// analytics/maintenance. See run.go for the poll loop.
func main() {
	run()
}
