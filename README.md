# midi2mqtt

A lightweight and easy-to-use MIDI to MQTT bridge written in Go. This tool allows you to capture MIDI events from your devices and publish them to an MQTT broker, making it perfect for home automation integration.

## Motivation

While there are several MIDI-to-MQTT bridges available, many of them are complex to set up or require additional dependencies. This tool aims to solve these problems by providing:

- A single, standalone binary that's easy to deploy
- Simple configuration through YAML
- Seamless integration with Home Assistant
- Support for all common MIDI events
- Reliable connection handling with automatic reconnection

One of the main use cases is integrating MIDI controllers with Home Assistant, allowing you to use MIDI devices (like the Arturia KeyLab) as input devices for your home automation. This opens up possibilities like:

- Using MIDI faders to control light brightness
- Triggering scenes with MIDI pads
- Creating custom control surfaces with MIDI controllers
- Monitoring MIDI events for automation triggers

## Usage

The tool is controlled through command-line parameters and a configuration file:

```bash
Usage: midi2mqtt [options]
Options:
  -list-ports     List available MIDI ports and exit
  -test          Run in test mode (print MIDI events to stdout)
```

Example outputs:

```bash
# List available MIDI ports
$ midi2mqtt -list-ports
Available MIDI ports:
  1: Arturia KeyLab mkII
  2: Virtual Port 1

# Test MIDI events without MQTT
$ midi2mqtt -test
Using config file: /path/to/config/midi2mqtt.yaml
Started MIDI test mode
Listening on MIDI port: Arturia KeyStep 37:Arturia KeyStep 37 MIDI 1 36:0
Press Ctrl+C to exit
{
  "timestamp": "2024-12-11T23:12:35.313590024+01:00",
  "port": "Arturia KeyStep 37:Arturia KeyStep 37 MIDI 1 36:0",
  "channel": 0,
  "note": 60,
  "key": "C",
  "octave": 4,
  "velocity": 105,
  "event_type": "note_on"
}
{
  "timestamp": "2024-12-11T23:12:35.886200485+01:00",
  "port": "Arturia KeyStep 37:Arturia KeyStep 37 MIDI 1 36:0",
  "channel": 0,
  "note": 60,
  "key": "C",
  "octave": 4,
  "event_type": "note_off"
}
```

## Configuration

The program looks for the configuration file (`midi2mqtt.yaml`) in the following locations, in order:

1. Current working directory (`./midi2mqtt.yaml`)
2. Directory containing the executable
3. On Linux/macOS:
   - `~/.config/midi2mqtt/midi2mqtt.yaml`
   - `/etc/midi2mqtt/midi2mqtt.yaml`
   - `/etc/midi2mqtt.yaml`
4. On Windows:
   - `%APPDATA%\midi2mqtt\midi2mqtt.yaml`

The first file found in this order will be used. The program will display which configuration file it is using when started.

The configuration file uses YAML format and supports the following settings:

```yaml
mqtt:
  # Broker connection settings
  broker:
    host: localhost
    port: 1883
    protocol: tcp  # tcp or ssl

  # Client settings
  client:
    client_id: midi2mqtt
    clean_session: true
    keepalive: 30

  # Authentication (optional)
  auth:
    username: ""
    password: ""

  # TLS/SSL settings (optional)
  tls:
    enabled: false
    verify_cert: true
    ca_cert: ""
    client_cert: ""
    client_key: ""

  # Topics configuration
  topics:
    publications:
      - topic: "midi/events"
        qos: 0
        retain: false

  # Connection behavior
  connection:
    retry_interval: 10 # Will retry every 10 seconds
    timeout: 30 # Will timeout after 30 seconds

midi:
  # MIDI port configuration
  port: "Arturia KeyLab mkII"  # Port name or leave empty for first available
  event_types:  # List of MIDI events to capture
    - note_on
    - note_off
    - control_change
    - pitch_bend
    - program_change
```

## Home Assistant Integration

To integrate MIDI events into Home Assistant, add a configuration such as the following to your `configuration.yaml`:

```yaml
mqtt:
  sensor:
    - name: "Arturia MIDI Event Sensor"
      state_topic: "midi/events"
      value_template: "{{ value_json.event_type }}"
      unique_id: arturia_midi_event_sensor
      json_attributes_topic: "midi/events"
      json_attributes_template: >-
        {% set attrs = {
          "timestamp": value_json.timestamp,
          "port": value_json.port,
          "channel": value_json.channel,
          "event_type": value_json.event_type
        } %}
        {% for attr in ['note', 'key', 'octave', 'velocity', 'controller', 'value', 'pitch_bend', 'program'] %}
          {% if value_json[attr] is defined %}
            {% set attrs = attrs | combine({attr: value_json[attr]}) %}
          {% endif %}
        {% endfor %}
        {{ attrs | tojson }}
```

This configuration creates a sensor that:
- Shows the current MIDI event type as its state
- Stores all event details as attributes
- Updates in real-time as MIDI events occur
- Can be used in automations and scripts

### Creating Automations in Home Assistant

You can create automations using the Home Assistant GUI to react to MIDI events. Here's how to access the MIDI event attributes:

1. Go to Settings → Automations & Scenes → Create Automation
2. Add a trigger:
   - Choose "Entity" -> "State" as trigger type
   - Select the MIDI Event Sensor entity
   - Optionally set a specific state attribute to trigger on (e.g., Event Type "note_on" or "control_change")

3. Add a condition (optional)

4. Add your desired action:
   - For continuous controls (like faders), you can use the attribute value:
     ```
     {{ trigger.to_state.attributes.value }}  {# For control change values #}
     {{ trigger.to_state.attributes.velocity }}  {# For note velocity #}
     ```

Available attributes in the sensor:
- `timestamp`: Time of the MIDI event
- `port`: MIDI port name
- `channel`: MIDI channel number (0-15)
- `event_type`: Type of MIDI event (note_on, note_off, control_change, etc.)
- `note`: MIDI note number (0-127, for note events)
- `key`: Note name (C, C#, etc., for note events)
- `octave`: Note octave (for note events)
- `velocity`: Note velocity (0-127, for note events)
- `controller`: Controller number (for control_change events)
- `value`: Controller value (0-127, for control_change events)
- `pitch_bend`: Pitch bend value (for pitch_bend events)
- `program`: Program number (for program_change events)

## Known Issues / Untested

1. Documentation generated by LLM. Be aware of errors or mistakes.
2. MQTT with TLS and certificates is untested.
3. Most testing is currently done with note_on and note_off events. Other MIDI events may need additional testing.

## Building from Source

To build the binary from source, you need Go 1.21 or later installed. Then:

```bash
# Clone the repository
git clone https://github.com/bzeiss/midi2mqtt.git
cd midi2mqtt

# Build the binary
go build -o midi2mqtt cmd/main.go

```

The resulting binary will be self-contained and can be deployed anywhere without additional dependencies.
