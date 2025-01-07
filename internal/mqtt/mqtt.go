package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"midi2mqtt/internal/config"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// MQTTClient represents an MQTT client with connection management
type MQTTClient struct {
	client       paho.Client
	mqttConfig   *config.MQTTConfig
	rootConfig   *config.Config
	mu           sync.RWMutex
	isConnected  bool
	reconnecting bool
	logger       *slog.Logger
}

// NewMQTTClient creates a new MQTT client and establishes a connection
func NewMQTTClient(cfg *config.Config, logger *slog.Logger) (*MQTTClient, error) {
	m := &MQTTClient{
		mqttConfig: &cfg.MQTT,
		rootConfig: cfg,
		logger:     logger,
	}

	if err := m.Connect(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *MQTTClient) setupTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: !m.mqttConfig.TLS.VerifyCert,
		MinVersion:         tls.VersionTLS12,
	}

	// Load CA certificate if provided
	if m.mqttConfig.TLS.CACert != "" {
		caCert, err := os.ReadFile(m.mqttConfig.TLS.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = certPool
	}

	// Load client certificate and key if provided
	if m.mqttConfig.TLS.ClientCert != "" && m.mqttConfig.TLS.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(m.mqttConfig.TLS.ClientCert, m.mqttConfig.TLS.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// Connect establishes a connection to the MQTT broker
func (m *MQTTClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isConnected {
		return nil
	}

	opts := paho.NewClientOptions()
	broker := fmt.Sprintf("%s://%s:%d", m.mqttConfig.Broker.Protocol, m.mqttConfig.Broker.Host, m.mqttConfig.Broker.Port)
	opts.AddBroker(broker)

	// Setup TLS if enabled
	if m.mqttConfig.TLS.Enabled {
		tlsConfig, err := m.setupTLSConfig()
		if err != nil {
			return fmt.Errorf("TLS setup failed:\n  %w", err)
		}
		opts.SetTLSConfig(tlsConfig)
	}

	// Set client options
	opts.SetClientID(m.mqttConfig.Client.ClientID)
	opts.SetCleanSession(m.mqttConfig.Client.CleanSession)
	opts.SetKeepAlive(time.Duration(m.mqttConfig.Client.Keepalive) * time.Second)

	if m.mqttConfig.Auth.Username != "" {
		opts.SetUsername(m.mqttConfig.Auth.Username)
		opts.SetPassword(m.mqttConfig.Auth.Password)
	}

	// Set initial connection timeouts without retries
	opts.SetConnectTimeout(5 * time.Second)
	opts.SetWriteTimeout(2 * time.Second)
	opts.SetConnectRetry(false)
	opts.SetAutoReconnect(false)

	// Log initial connection attempt
	m.logger.Info("Testing MQTT connection", "broker", broker)

	// First try to connect without retries to verify credentials
	client := paho.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(2*time.Second) && token.Error() == nil {
		client.Disconnect(250)
		return fmt.Errorf("connection timeout - check if broker is reachable (Broker: %s)", broker)
	}
	if err := token.Error(); err != nil {
		client.Disconnect(250)
		return fmt.Errorf("%v (Broker: %s)", err, broker)
	}

	// Initial connection successful, disconnect and reconnect with retries enabled
	client.Disconnect(250)

	// Now set up the persistent connection with retries
	opts.SetConnectRetry(true)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(time.Duration(m.mqttConfig.Connection.RetryInterval) * time.Second)
	opts.SetResumeSubs(true)

	// Set connection handlers for the persistent connection
	opts.SetOnConnectHandler(m.onConnect)
	opts.SetConnectionLostHandler(m.onConnectionLost)
	opts.SetReconnectingHandler(m.onReconnecting)

	m.client = paho.NewClient(opts)
	m.logger.Info("Establishing persistent MQTT connection", "broker", broker)

	// Connect with retries enabled
	token = m.client.Connect()
	if !token.WaitTimeout(2*time.Second) && token.Error() == nil {
		return fmt.Errorf("connection timeout on persistent connection (Broker: %s)", broker)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("%v (Broker: %s)", err, broker)
	}

	m.isConnected = true
	return nil
}

func (m *MQTTClient) onConnect(client paho.Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isConnected = true
	m.reconnecting = false
	m.logger.Info("Connected to MQTT broker",
		"protocol", m.mqttConfig.Broker.Protocol,
		"host", m.mqttConfig.Broker.Host,
		"port", m.mqttConfig.Broker.Port)
}

func (m *MQTTClient) onConnectionLost(client paho.Client, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isConnected = false
	m.logger.Info("Connection lost to MQTT broker", "error", err)
}

func (m *MQTTClient) onReconnecting(client paho.Client, opts *paho.ClientOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.reconnecting {
		m.reconnecting = true
		m.logger.Info("Attempting to reconnect to MQTT broker...")
	}
}

// IsConnected returns the current connection state
func (m *MQTTClient) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isConnected
}

// publishToTopic is a helper function that handles the actual MQTT publish operation
func (m *MQTTClient) publishToTopic(topic string, qos byte, retain bool, payload []byte) error {
	if !m.IsConnected() {
		m.logger.Error("Not connected to MQTT broker")
		return fmt.Errorf("not connected to MQTT broker")
	}

	token := m.client.Publish(topic, qos, retain, payload)
	if !token.WaitTimeout(2 * time.Second) {
		m.logger.Error("Publish timeout", "topic", topic)
		return fmt.Errorf("publish timeout for topic %s", topic)
	}
	if err := token.Error(); err != nil {
		m.logger.Error("Failed to publish", "topic", topic, "error", err)
		return fmt.Errorf("failed to publish to topic %s: %w", topic, err)
	}
	return nil
}

// PublishToAll publishes a message to all enabled publications
func (m *MQTTClient) PublishToAll(payload []byte) error {
	var lastErr error

	// Try publishing to all enabled publications
	for i := range m.rootConfig.MQTTPublications {
		pub := &m.rootConfig.MQTTPublications[i]
		if !pub.Enabled {
			continue
		}

		var err error
		switch pub.Type {
		case config.PublicationTypeCustomJSON:
			err = m.PublishCustomJSON(payload, pub)
		case config.PublicationTypeHomeAssistant:
			err = m.PublishHomeAssistant(payload, pub)
		default:
			m.logger.Warn("Unknown publication type", "type", pub.Type)
			continue
		}

		if err != nil {
			lastErr = err
			m.logger.Error("Failed to publish", "type", pub.Type, "error", err)
		}
	}

	return lastErr
}

// PublishCustomJSON publishes a message using the custom JSON publication configuration
func (m *MQTTClient) PublishCustomJSON(payload []byte, pub *config.PublicationConfig) error {
	m.logger.Debug("Publishing to custom_json topic",
		"topic", pub.Topic,
		"qos", pub.QoS,
		"retain", pub.Retain)
	return m.publishToTopic(pub.Topic, byte(pub.QoS), pub.Retain, payload)
}

// PublishHomeAssistant publishes a message using the Home Assistant publication configuration
func (m *MQTTClient) PublishHomeAssistant(payload []byte, pub *config.PublicationConfig) error {
	m.logger.Debug("Publishing to home_assistant topic",
		"topic", pub.Topic,
		"qos", pub.QoS,
		"retain", pub.Retain)
	// TODO: Implement Home Assistant specific payload formatting if needed
	return m.publishToTopic(pub.Topic, byte(pub.QoS), pub.Retain, payload)
}

// Disconnect cleanly disconnects from the MQTT broker
func (m *MQTTClient) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && m.client.IsConnected() {
		m.logger.Info("Disconnecting from MQTT broker...")
		m.client.Disconnect(250)
	}
	m.isConnected = false
}
