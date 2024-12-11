package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"midi2mqtt/internal/config"
)

// MQTTClient represents an MQTT client with connection management
type MQTTClient struct {
	client       paho.Client
	config       *config.MQTTConfig
	mu           sync.RWMutex
	isConnected  bool
	reconnecting bool
}

// NewMQTTClient creates a new MQTT client and establishes a connection
func NewMQTTClient(cfg *config.MQTTConfig) (*MQTTClient, error) {
	m := &MQTTClient{
		config: cfg,
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

	// Set connection timeouts and retry options
	opts.SetConnectTimeout(time.Duration(m.config.Connection.Timeout) * time.Second)
	opts.SetConnectRetry(true)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(time.Duration(m.config.Connection.RetryInterval) * time.Second)
	opts.SetResumeSubs(true)

	// Set connection handlers
	opts.SetOnConnectHandler(m.onConnect)
	opts.SetConnectionLostHandler(m.onConnectionLost)
	opts.SetReconnectingHandler(m.onReconnecting)

	m.client = paho.NewClient(opts)

	// Connect with timeout
	token := m.client.Connect()
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("connection timeout while connecting to:\n  %s", broker)
	}
	if err := token.Error(); err != nil {
		// Check for common authentication errors
		errStr := err.Error()
		switch {
		case strings.Contains(errStr, "identifier rejected"):
			return fmt.Errorf("authentication failed - incorrect username or password\nBroker: %s", broker)
		case strings.Contains(errStr, "not authorised"):
			return fmt.Errorf("authorization failed - user does not have permission\nBroker: %s", broker)
		case strings.Contains(errStr, "connection refused"):
			return fmt.Errorf("connection refused - check your credentials and broker settings\nBroker: %s", broker)
		default:
			return fmt.Errorf("connection failed:\n  %v\nBroker: %s", err, broker)
		}
	}

	m.isConnected = true
	return nil
}

func (m *MQTTClient) onConnect(client paho.Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isConnected = true
	m.reconnecting = false
	fmt.Printf("Connected to MQTT broker at %s://%s:%d\n",
		m.config.Broker.Protocol, m.config.Broker.Host, m.config.Broker.Port)
}

func (m *MQTTClient) onConnectionLost(client paho.Client, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isConnected = false
	fmt.Printf("Connection lost to MQTT broker: %v\n", err)
}

func (m *MQTTClient) onReconnecting(client paho.Client, opts *paho.ClientOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.reconnecting {
		m.reconnecting = true
		fmt.Println("Attempting to reconnect to MQTT broker...")
	}
}

// IsConnected returns the current connection state
func (m *MQTTClient) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isConnected && m.client.IsConnected()
}

// Publish sends a message to the specified MQTT topic
func (m *MQTTClient) Publish(topic string, payload []byte) error {
	if !m.IsConnected() {
		return fmt.Errorf("not connected to MQTT broker")
	}

	pubConfig := m.config.Topics.Publications[0]
	token := m.client.Publish(topic, byte(pubConfig.QoS), pubConfig.Retain, payload)
	if !token.WaitTimeout(2 * time.Second) {
		return fmt.Errorf("publish timeout for topic %s", topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to publish to topic %s: %w", topic, err)
	}
	return nil
}

// Disconnect cleanly disconnects from the MQTT broker
func (m *MQTTClient) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil && m.client.IsConnected() {
		fmt.Println("Disconnecting from MQTT broker...")
		m.client.Disconnect(250)
	}
	m.isConnected = false
}
