package mqtt

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// HomeAssistantManager manages Home Assistant MQTT discovery
type HomeAssistantManager struct {
	mu            sync.RWMutex
	configSentMap map[string]bool // Maps note values to whether config has been sent
}

// NewHomeAssistantManager creates a new Home Assistant manager
func NewHomeAssistantManager() *HomeAssistantManager {
	return &HomeAssistantManager{
		configSentMap: make(map[string]bool),
	}
}

// HomeAssistantConfig represents the Home Assistant MQTT discovery config
type HomeAssistantConfig struct {
	Name       string                 `json:"name"`
	UniqueID   string                 `json:"unique_id"`
	ObjectID   string                 `json:"object_id"`
	Device     map[string]interface{} `json:"device"`
	StateTopic string                 `json:"state_topic"`
	PayloadOn  string                 `json:"payload_on"`
	PayloadOff string                 `json:"payload_off"`
	Icon       string                 `json:"icon"`
}

// HasConfigBeenSent checks if config has been sent for a specific note
func (h *HomeAssistantManager) HasConfigBeenSent(noteValue string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.configSentMap[noteValue]
}

// MarkConfigAsSent marks a config as sent for a specific note
func (h *HomeAssistantManager) MarkConfigAsSent(noteValue string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configSentMap[noteValue] = true
}

// BuildConfigMessage builds a Home Assistant MQTT discovery config message
func (h *HomeAssistantManager) BuildConfigMessage(baseTopic, uniqueID string, device map[string]interface{}, noteValue string) ([]byte, error) {
	config := HomeAssistantConfig{
		Name:       fmt.Sprintf("Note %s", noteValue),
		UniqueID:   fmt.Sprintf("%s_%s", uniqueID, noteValue),
		ObjectID:   fmt.Sprintf("midi_note_%s", strings.ToLower(noteValue)),
		Device:     device,
		StateTopic: baseTopic + "/state",
		PayloadOn:  "on",
		PayloadOff: "off",
		Icon:       "mdi:piano",
	}

	return json.Marshal(config)
}
