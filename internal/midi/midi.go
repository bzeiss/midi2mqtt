package midi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv" // autoregisters driver

	"midi2mqtt/internal/config"
)

type MIDIHandler struct {
	portName  string
	port     drivers.In
	callback func([]byte) error
	stop     func()
	config   *config.MIDIConfig
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	isActive bool
}

type MIDIEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Port       string    `json:"port"`
	Channel    uint8     `json:"channel"`
	Note       uint8     `json:"note,omitempty"`
	Key        string    `json:"key,omitempty"`
	Octave     int       `json:"octave,omitempty"`
	Velocity   uint8     `json:"velocity,omitempty"`
	Controller uint8     `json:"controller,omitempty"`
	Value      uint8     `json:"value,omitempty"`
	PitchBend  int16     `json:"pitch_bend,omitempty"`
	Program    uint8     `json:"program,omitempty"`
	EventType  string    `json:"event_type"`
}

// noteToKey converts a MIDI note number to key name and octave
func noteToKey(note uint8) (key string, octave int) {
	noteNames := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave = int((int(note) - 12) / 12)
	keyIndex := int(note) % 12
	return noteNames[keyIndex], octave
}

func NewMIDIHandler(portName string, cfg *config.MIDIConfig, callback func([]byte) error) (*MIDIHandler, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	handler := &MIDIHandler{
		portName:  portName,
		callback: callback,
		config:   cfg,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Initial connection
	if err := handler.connect(); err != nil {
		cancel()
		return nil, err
	}

	return handler, nil
}

func (h *MIDIHandler) connect() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Get all available ports
	ports := midi.GetInPorts()
	if len(ports) == 0 {
		return fmt.Errorf("no MIDI ports found")
	}

	// Find the first port that contains the configured port name as a substring (case insensitive)
	configuredPortName := strings.ToLower(h.portName)
	var matchedPort drivers.In
	for _, port := range ports {
		if strings.Contains(strings.ToLower(port.String()), configuredPortName) {
			matchedPort = port
			break
		}
	}

	if matchedPort == nil {
		return fmt.Errorf("no MIDI port matching '%s' found", h.portName)
	}

	h.port = matchedPort
	h.isActive = true
	return nil
}

func (h *MIDIHandler) monitorConnection() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			if !h.isActive {
				h.mu.Unlock()
				continue
			}
			h.mu.Unlock()

			// Check if port is still available
			ports := midi.GetInPorts()
			portFound := false
			configuredPortName := strings.ToLower(h.portName)
			
			for _, port := range ports {
				if strings.Contains(strings.ToLower(port.String()), configuredPortName) {
					portFound = true
					break
				}
			}

			if !portFound {
				h.mu.Lock()
				h.isActive = false
				if h.stop != nil {
					h.stop()
					h.stop = nil
				}
				h.mu.Unlock()
				
				slog.Error("MIDI port disconnected", "port", h.portName)

				// Try to reconnect
				go h.reconnect()
			}
		}
	}
}

func (h *MIDIHandler) reconnect() {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-time.After(backoff):
			if err := h.connect(); err == nil {
				// Successfully reconnected
				if err := h.Start(); err == nil {
					slog.Info("MIDI port reconnected", "port", h.portName)
					return
				}
			}

			// Increase backoff time
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (h *MIDIHandler) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.isActive {
		return fmt.Errorf("MIDI handler is not active")
	}

	var err error
	h.stop, err = midi.ListenTo(h.port, func(msg midi.Message, timestampms int32) {
		var event *MIDIEvent
		var eventType string

		// Common fields for all events
		baseEvent := MIDIEvent{
			Timestamp: time.Now(),
			Port:      h.port.String(),
		}

		// Handle different message types
		switch {
		case msg.GetNoteStart(&baseEvent.Channel, &baseEvent.Note, &baseEvent.Velocity):
			eventType = "note_on"
			baseEvent.Key, baseEvent.Octave = noteToKey(baseEvent.Note)
			event = &baseEvent

		case msg.GetNoteEnd(&baseEvent.Channel, &baseEvent.Note):
			eventType = "note_off"
			baseEvent.Key, baseEvent.Octave = noteToKey(baseEvent.Note)
			event = &baseEvent

		case msg.GetControlChange(&baseEvent.Channel, &baseEvent.Controller, &baseEvent.Value):
			eventType = fmt.Sprintf("cc:%d", baseEvent.Controller)
			event = &baseEvent

		case msg.GetProgramChange(&baseEvent.Channel, &baseEvent.Program):
			eventType = "program_change"
			event = &baseEvent

		case msg.GetPitchBend(&baseEvent.Channel, &baseEvent.PitchBend, nil):
			eventType = "pitch_bend"
			event = &baseEvent

		case msg.GetAfterTouch(&baseEvent.Channel, &baseEvent.Value):
			eventType = "channel_pressure"
			event = &baseEvent
		}

		if event != nil {
			event.EventType = eventType

			// Check if this event type is allowed
			if !h.config.IsEventAllowed(eventType) {
				return
			}

			jsonData, err := json.Marshal(event)
			if err != nil {
				slog.Error("Error marshaling MIDI event", "error", err)
				return
			}

			if err := h.callback(jsonData); err != nil {
				slog.Error("Error handling MIDI event", "error", err)
			}
		}
	})

	if err == nil {
		// Start the connection monitor
		go h.monitorConnection()
	}

	return err
}

// ListPorts returns a list of available MIDI input ports
func ListPorts() []string {
	var ports []string
	for _, port := range midi.GetInPorts() {
		ports = append(ports, port.String())
	}
	return ports
}

func (h *MIDIHandler) Close() error {
	h.cancel() // Stop the monitor
	h.mu.Lock()
	if h.stop != nil {
		h.stop()
	}
	h.mu.Unlock()
	midi.CloseDriver()
	return nil
}
