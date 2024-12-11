package midi

import (
	"encoding/json"
	"fmt"
	"time"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv" // autoregisters driver

	"midi2mqtt/internal/config"
)

type MIDIHandler struct {
	port     drivers.In
	callback func([]byte) error
	stop     func()
	config   *config.MIDIConfig
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
	// Find the specified port
	in, err := midi.FindInPort(portName)
	if err != nil {
		return nil, fmt.Errorf("MIDI port %s not found: %v", portName, err)
	}

	return &MIDIHandler{
		port:     in,
		callback: callback,
		config:   cfg,
	}, nil
}

func (h *MIDIHandler) Start() error {
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

		// Note: GetAfterTouch is the equivalent of Channel Pressure in gomidi v2
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
				fmt.Printf("Error marshaling MIDI event: %v\n", err)
				return
			}

			if err := h.callback(jsonData); err != nil {
				fmt.Printf("Error handling MIDI event: %v\n", err)
			}
		}
	})

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
	if h.stop != nil {
		h.stop()
	}
	midi.CloseDriver()
	return nil
}
