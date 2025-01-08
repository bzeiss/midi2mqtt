package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"midi2mqtt/internal/config"
	"midi2mqtt/internal/logging"
	"midi2mqtt/internal/midi"
	"midi2mqtt/internal/mqtt"

	"log/slog"
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

func setupLogger(cfg *config.Config) {
	// Default to info level if not specified
	logLevel := slog.LevelInfo

	// Parse log level from config
	switch strings.ToUpper(cfg.LogLevel) {
	case "DEBUG":
		logLevel = slog.LevelDebug
	case "INFO":
		logLevel = slog.LevelInfo
	case "WARN":
		logLevel = slog.LevelWarn
	case "ERROR":
		logLevel = slog.LevelError
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	handler := logging.NewCustomHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func NewApp(cfg *config.Config) (*App, error) {
	// Create MQTT client
	mqttClient, err := mqtt.NewMQTTClient(cfg, slog.Default())
	if err != nil {
		return nil, fmt.Errorf("Error creating MQTT client: %v", err)
	}

	// Create MIDI handler
	midiHandler, err := midi.NewMIDIHandler(cfg.MIDI.Port, &cfg.MIDI, func(data []byte) error {
		if err := mqttClient.PublishToAll(data); err != nil {
			return fmt.Errorf("Error publishing MIDI event: %v", err)
		}
		if cfg.LogLevel == "DEBUG" {
			slog.Debug("MIDI event", "data", formatEvent(data))
		}
		return nil
	})
	if err != nil {
		mqttClient.Disconnect() // Clean up MQTT client if MIDI fails
		return nil, fmt.Errorf("Error creating MIDI handler: %v", err)
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
		return fmt.Errorf("Error starting MIDI handler: %v", err)
	}

	slog.Info("Started MIDI to MQTT bridge")
	slog.Info("Listening on MIDI port", "port", a.cfg.MIDI.Port)

	if len(a.cfg.MQTTPublications) > 0 {
		for _, pub := range a.cfg.MQTTPublications {
			if pub.Enabled {
				slog.Info("Publishing to MQTT topic", 
					"type", pub.Type,
					"topic", pub.Topic,
					"qos", pub.QoS,
					"retain", pub.Retain)
			}
		}
	} else {
		slog.Warn("No MQTT publication topics configured")
	}

	// Wait for context cancellation
	<-ctx.Done()
	slog.Info("Shutting down gracefully...")
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
		slog.Info("Shutting down gracefully...")
		cancel()
	}()
	return ctx, cancel
}

func main() {
	// Parse command line flags
	showPorts := flag.Bool("list-ports", false, "List available MIDI ports and exit")
	testMode := flag.Bool("test", false, "Run in test mode (print MIDI events to stdout)")
	allEvents := flag.Bool("all-events", false, "Listening to all MIDI events")
	flag.Parse()

	// Handle list-ports command first, before loading config
	if *showPorts {
		listPorts()
		os.Exit(exitCodeSuccess)
	}

	// Load configuration for all other modes
	configPath, err := config.FindConfig()
	if err != nil {
		slog.Error("Configuration error", "error", err)
		os.Exit(exitCodeError)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		slog.Error("Error loading config from %s", "error", err, "path", configPath)
		os.Exit(exitCodeError)
	}

	slog.Info("Using config file", "path", configPath)

	if *allEvents {
		cfg.MIDI.EventTypes = nil
		slog.Info("Listening to all MIDI events")
	}

	if *testMode {
		if err := runTestMode(cfg); err != nil {
			slog.Error("Error running test mode", "error", err)
			os.Exit(exitCodeError)
		}
		os.Exit(exitCodeSuccess)
	}

	// Setup logger with configured level
	setupLogger(cfg)

	// Create and start the application
	app, err := NewApp(cfg)
	if err != nil {
		slog.Error("Error initializing application", "error", err)
		os.Exit(exitCodeError)
	}
	defer app.Shutdown()

	// Setup signal handling
	ctx, cancel := setupSignalHandler()
	defer cancel()

	// Run the application
	if err := app.Start(ctx); err != nil {
		slog.Error("Error running application", "error", err)
		os.Exit(exitCodeError)
	}
}

func runTestMode(cfg *config.Config) error {
	// Create MIDI handler for test mode
	handler, err := midi.NewMIDIHandler(cfg.MIDI.Port, &cfg.MIDI, func(data []byte) error {
		// Pretty print the JSON
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, data, "", "  "); err != nil {
			slog.Error("Error formatting JSON", "error", err)
			return nil
		}
		slog.Info("Received MIDI event", "data", prettyJSON.String())
		return nil
	})
	if err != nil {
		return fmt.Errorf("Error creating MIDI handler: %v", err)
	}
	defer handler.Close()

	if err := handler.Start(); err != nil {
		slog.Error("Error starting MIDI handler", "error", err)
		return err
	}

	slog.Info("Started MIDI test mode")
	slog.Info("Listening on MIDI port", "port", cfg.MIDI.Port)
	slog.Info("Press Ctrl+C to exit")

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

	fmt.Println("Available MIDI input ports")
	for i, port := range ports {
		fmt.Println("Port found", "id", i+1, "name", port)
	}
}

// formatEvent creates a compact one-line string representation of a MIDI event
func formatEvent(data []byte) string {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Sprintf("Error formatting event: %v", err)
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

	eventType := getString("event_type")
	channel := getUint8("channel")

	switch eventType {
	case "note_on", "note_off":
		return fmt.Sprintf("%s ch:%d %s%d vel:%d",
			eventType,
			channel,
			getString("key"),
			getInt("octave"),
			getUint8("velocity"))

	case "pitch_bend":
		return fmt.Sprintf("%s ch:%d value:%d",
			eventType,
			channel,
			getInt16("pitch_bend"))

	default:
		if len(eventType) >= 3 && eventType[:2] == "cc" {
			return fmt.Sprintf("%s ch:%d value:%d",
				eventType,
				channel,
				getUint8("value"))
		}

		// Generic format for other events
		return fmt.Sprintf("%s ch:%d",
			eventType,
			channel)
	}
}
