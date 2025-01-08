package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MQTT             MQTTConfig          `yaml:"mqtt_server"`
	MIDI             MIDIConfig          `yaml:"midi"`
	LogLevel         string              `yaml:"log_level"`
	MQTTPublications []PublicationConfig `yaml:"mqtt_publications"`
}

type PublicationType string

const (
	PublicationTypeCustomJSON    PublicationType = "custom_json"
	PublicationTypeHomeAssistant PublicationType = "home_assistant"
)

type PublicationConfig struct {
	Type      PublicationType `yaml:"type"`
	Enabled   bool            `yaml:"enabled"`
	Topic     string          `yaml:"topic"`
	QoS       int             `yaml:"qos"`
	Retain    bool            `yaml:"retain"`
	UniqueID  string          `yaml:"unique_id,omitempty"`
	Device    *DeviceConfig   `yaml:"device,omitempty"`
}

type DeviceConfig struct {
	Identifiers  []string `yaml:"identifiers"`
	Name         string   `yaml:"name"`
	Manufacturer string   `yaml:"manufacturer"`
}

type MQTTConfig struct {
	Broker     BrokerConfig     `yaml:"broker"`
	Client     ClientConfig     `yaml:"client"`
	Auth       AuthConfig       `yaml:"auth"`
	TLS        TLSConfig        `yaml:"tls"`
	Topics     TopicConfig      `yaml:"topics"`
	Connection ConnectionConfig `yaml:"connection"`
}

type BrokerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Protocol string `yaml:"protocol"`
}

type ClientConfig struct {
	ClientID     string `yaml:"client_id"`
	CleanSession bool   `yaml:"clean_session"`
	Keepalive    int    `yaml:"keepalive"`
}

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type TLSConfig struct {
	Enabled      bool     `yaml:"enabled"`
	VerifyCert   bool     `yaml:"verify_cert"`
	CACert       string   `yaml:"ca_cert"`
	ClientCert   string   `yaml:"client_cert"`
	ClientKey    string   `yaml:"client_key"`
	CipherSuites []string `yaml:"cipher_suites"`
	StartTLS     bool     `yaml:"starttls"`
}

type TopicConfig struct {
	Subscriptions []TopicDetail `yaml:"subscriptions"`
	Publications  []TopicDetail `yaml:"publications"`
}

type TopicDetail struct {
	Topic  string `yaml:"topic"`
	QoS    int    `yaml:"qos"`
	Retain bool   `yaml:"retain,omitempty"`
}

type ConnectionConfig struct {
	RetryInterval int `yaml:"retry_interval"`
	MaxRetries    int `yaml:"max_retries"`
	Timeout       int `yaml:"timeout"`
}

type MIDIConfig struct {
	Port       string   `yaml:"port"`
	EventTypes []string `yaml:"event_types"`
}

// IsEventAllowed checks if a given event type is in the whitelist
func (c *MIDIConfig) IsEventAllowed(eventType string) bool {
	// If no event types are specified, allow all events
	if len(c.EventTypes) == 0 {
		return true
	}

	// Check if the event type is in the whitelist
	for _, allowed := range c.EventTypes {
		// Direct match for simple events
		if allowed == eventType {
			return true
		}

		// Handle CC events specially
		if ccRegex := regexp.MustCompile(`^cc:(\d+)$`); ccRegex.MatchString(allowed) {
			if ccEventRegex := regexp.MustCompile(`^cc:(\d+)$`); ccEventRegex.MatchString(eventType) {
				// Extract CC numbers from both strings
				allowedMatches := ccRegex.FindStringSubmatch(allowed)
				eventMatches := ccEventRegex.FindStringSubmatch(eventType)
				if len(allowedMatches) == 2 && len(eventMatches) == 2 {
					allowedCC, _ := strconv.Atoi(allowedMatches[1])
					eventCC, _ := strconv.Atoi(eventMatches[1])
					if allowedCC == eventCC {
						return true
					}
				}
			}
		}
	}

	return false
}

// GetConfigSearchPaths returns a list of paths to search for the config file
func GetConfigSearchPaths() []string {
	configName := "midi2mqtt.yaml"

	// Get executable directory
	exePath, err := os.Executable()
	exeDir := "."
	if err == nil {
		exeDir = filepath.Dir(exePath)
	}

	// Start with current directory and executable directory
	paths := []string{
		// Current working directory
		configName,
		// Executable directory
		filepath.Join(exeDir, configName),
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		switch runtime.GOOS {
		case "linux", "darwin":
			// Linux/macOS paths
			paths = append(paths,
				filepath.Join(homeDir, ".config", "midi2mqtt", configName),
				filepath.Join("/etc", "midi2mqtt", configName),
				filepath.Join("/etc", configName),
			)
		case "windows":
			// Windows paths
			if appData := os.Getenv("APPDATA"); appData != "" {
				paths = append(paths,
					filepath.Join(appData, "midi2mqtt", configName),
				)
			}
		}
	}

	// Remove duplicates while preserving order
	seen := make(map[string]bool)
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if !seen[absPath] {
			seen[absPath] = true
			unique = append(unique, path)
		}
	}

	return unique
}

// FindConfig searches for the config file in standard locations
func FindConfig() (string, error) {
	searchPaths := GetConfigSearchPaths()

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			absPath, err := filepath.Abs(path)
			if err == nil {
				return absPath, nil
			}
			return path, nil
		}
	}

	return "", fmt.Errorf("config file not found. Searched in:\n  %s",
		"\n  "+filepath.Join(searchPaths...))
}

// LoadConfig loads the configuration from the specified file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	return &config, nil
}
