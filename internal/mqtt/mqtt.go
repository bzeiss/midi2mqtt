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
	config       *config.MQTTConfig
	mu           sync.RWMutex
	isConnected  bool
	reconnecting bool
	logger       *slog.Logger
}

// NewMQTTClient creates a new MQTT client and establishes a connection
func NewMQTTClient(cfg *config.MQTTConfig, logger *slog.Logger) (*MQTTClient, error) {
	m := &MQTTClient{
		config: cfg,
		logger: logger,
	}

	if err := m.Connect(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *MQTTClient) setupTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: !m.config.TLS.VerifyCert,
		MinVersion:         tls.VersionTLS12,
	}

	// Load CA certificate if provided
	if m.config.TLS.CACert != "" {
		caCert, err := os.ReadFile(m.config.TLS.CACert)
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
	if m.config.TLS.ClientCert != "" && m.config.TLS.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(m.config.TLS.ClientCert, m.config.TLS.ClientKey)
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
	broker := fmt.Sprintf("%s://%s:%d", m.config.Broker.Protocol, m.config.Broker.Host, m.config.Broker.Port)
	opts.AddBroker(broker)

	// Setup TLS if enabled
	if m.config.TLS.Enabled {
		tlsConfig, err := m.setupTLSConfig()
		if err != nil {
			return fmt.Errorf("TLS setup failed:\n  %w", err)
		}
		opts.SetTLSConfig(tlsConfig)
	}

	// Set client options
	opts.SetClientID(m.config.Client.ClientID)
	opts.SetCleanSession(m.config.Client.CleanSession)
	opts.SetKeepAlive(time.Duration(m.config.Client.Keepalive) * time.Second)

	if m.config.Auth.Username != "" {
		opts.SetUsername(m.config.Auth.Username)
		opts.SetPassword(m.config.Auth.Password)
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
	opts.SetMaxReconnectInterval(time.Duration(m.config.Connection.RetryInterval) * time.Second)
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
		"protocol", m.config.Broker.Protocol,
		"host", m.config.Broker.Host,
		"port", m.config.Broker.Port)
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

// Publish sends a message to the specified MQTT topic
func (m *MQTTClient) Publish(topic string, payload []byte) error {
	if !m.IsConnected() {
		m.logger.Error("Not connected to MQTT broker")
		return fmt.Errorf("not connected to MQTT broker")
	}

	pubConfig := m.config.Topics.Publications[0]
	token := m.client.Publish(topic, byte(pubConfig.QoS), pubConfig.Retain, payload)
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
