package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"midi2mqtt/internal/config"
	"midi2mqtt/internal/midi"
	"midi2mqtt/internal/mqtt"
)

const (
	exitCodeSuccess = 0
	exitCodeError   = 1
)

type App struct {
	cfg         *config.Config
	mqttClient  *mqtt.MQTTClient
	midiHandler *midi.MIDIHandler
}

func NewApp(cfg *config.Config) (*App, error) {
	// Create MQTT client
	mqttClient, err := mqtt.NewMQTTClient(&cfg.MQTT)
	if err != nil {
		return nil, fmt.Errorf("failed to create MQTT client: %w", err)
	}

	// Create MIDI handler
	midiHandler, err := midi.NewMIDIHandler(cfg.MIDI.Port, &cfg.MIDI, func(data []byte) error {
		if err := mqttClient.Publish(cfg.MQTT.Topics.Publications[0].Topic, data); err != nil {
			return fmt.Errorf("failed to publish MIDI event: %w", err)
		}
		if cfg.Debug {
			fmt.Print(formatEvent(data))
		}
		return nil
	})
	if err != nil {
		mqttClient.Disconnect() // Clean up MQTT client if MIDI fails
		return nil, fmt.Errorf("failed to create MIDI handler: %w", err)
	}

	return &App{
		cfg:         cfg,
		mqttClient:  mqttClient,
		midiHandler: midiHandler,
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	// Start MIDI handler
	if err := a.midiHandler.Start(); err != nil {
		return fmt.Errorf("failed to start MIDI handler: %w", err)
	}

	fmt.Printf("Started MIDI to MQTT bridge\n")
	fmt.Printf("Listening on MIDI port: %s\n", a.cfg.MIDI.Port)
	fmt.Printf("Publishing to MQTT topic: %s\n", a.cfg.MQTT.Topics.Publications[0].Topic)

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

func (a *App) Shutdown() {
	if a.mqttClient != nil {
		a.mqttClient.Disconnect()
	}
	if a.midiHandler != nil {
		a.midiHandler.Close()
	}
}

func setupSignalHandler() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nShutting down gracefully...")
		cancel()
	}()
	return ctx, cancel
}

func main() {
	// Parse command line flags
	showPorts := flag.Bool("list-ports", false, "List available MIDI ports and exit")
	testMode := flag.Bool("test", false, "Run in test mode (print MIDI events to stdout)")
	flag.Parse()

	// Handle list-ports command first, before loading config
	if *showPorts {
		listPorts()
		os.Exit(exitCodeSuccess)
	}

	// Load configuration for all other modes
	configPath, err := config.FindConfig()
	if err != nil {
		log.Fatalf("\nConfiguration error:\n  %v\n", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("\nError loading config from %s:\n  %v\n", configPath, err)
	}

	fmt.Printf("Using config file: %s\n", configPath)

	if *testMode {
		if err := runTestMode(cfg); err != nil {
			log.Fatalf("\nError running test mode:\n  %v\n", err)
		}
		os.Exit(exitCodeSuccess)
	}

	// Create and start the application
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatalf("\nError initializing application:\n  %v\n", err)
	}
	defer app.Shutdown()

	// Setup signal handling
	ctx, cancel := setupSignalHandler()
	defer cancel()

	// Run the application
	if err := app.Start(ctx); err != nil {
		log.Fatalf("\nError running application:\n  %v\n", err)
	}
}

func runTestMode(cfg *config.Config) error {
	// Create MIDI handler for test mode
	handler, err := midi.NewMIDIHandler(cfg.MIDI.Port, &cfg.MIDI, func(data []byte) error {
		// Pretty print the JSON
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, data, "", "  "); err != nil {
			fmt.Printf("Error formatting JSON: %v\n", err)
			return nil
		}
		fmt.Printf("%s\n", prettyJSON.String())
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create MIDI handler: %w", err)
	}
	defer handler.Close()

	if err := handler.Start(); err != nil {
		return fmt.Errorf("failed to start MIDI handler: %w", err)
	}

	fmt.Printf("Started MIDI test mode\n")
	fmt.Printf("Listening on MIDI port: %s\n", cfg.MIDI.Port)
	fmt.Println("Press Ctrl+C to exit")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	return nil
}

func listPorts() {
	ports := midi.ListPorts()
	if len(ports) == 0 {
		fmt.Println("No MIDI input ports found")
		return
	}

	fmt.Println("Available MIDI input ports:")
	for i, port := range ports {
		fmt.Printf("%d: %s\n", i+1, port)
	}
}

// formatEvent creates a compact one-line string representation of a MIDI event
func formatEvent(data []byte) string {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Sprintf("Error formatting event: %v\n", err)
	}

	// Safely get values with type checking
	getUint8 := func(field string) uint8 {
		if v, ok := event[field].(float64); ok {
			return uint8(v)
		}
		return 0
	}

	getInt16 := func(field string) int16 {
		if v, ok := event[field].(float64); ok {
			return int16(v)
		}
		return 0
	}

	getString := func(field string) string {
		if v, ok := event[field].(string); ok {
			return v
		}
		return ""
	}

	getInt := func(field string) int {
		if v, ok := event[field].(float64); ok {
			return int(v)
		}
		return 0
	}

	// Get timestamp
	timestamp := getString("timestamp")
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t = time.Now()
	}
	timeStr := t.Format("15:04:05.000")

	eventType := getString("event_type")
	channel := getUint8("channel")

	switch eventType {
	case "note_on", "note_off":
		return fmt.Sprintf("%s %s ch:%d %s%d vel:%d\n",
			timeStr,
			eventType,
			channel,
			getString("key"),
			getInt("octave"),
			getUint8("velocity"))

	case "pitch_bend":
		return fmt.Sprintf("%s %s ch:%d value:%d\n",
			timeStr,
			eventType,
			channel,
			getInt16("pitch_bend"))

	default:
		if len(eventType) >= 3 && eventType[:2] == "cc" {
			return fmt.Sprintf("%s %s ch:%d value:%d\n",
				timeStr,
				eventType,
				channel,
				getUint8("value"))
		}

		// Generic format for other events
		return fmt.Sprintf("%s %s ch:%d\n",
			timeStr,
			eventType,
			channel)
	}
}
