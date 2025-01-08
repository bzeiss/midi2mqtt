# midi2mqtt

A lightweight and easy-to-use MIDI to MQTT bridge written in Go. This tool allows you to capture MIDI events from your devices and publish them to an MQTT broker, making it perfect for home automation integration.

*This is work in progress and has not been tested very thoroughly.*

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

## Binaries
There currently are pre-release binaries available for windows-amd64 (statically linked), linux-amd64 (dynamically linked, built on ubuntu 20.04 for backward compatibility) and macos (universal, should be compatible from macos 11 big sur or later). These are updated each time something is comitted/merged into main, so these are not stable releases yet, but you are welcome to test them.

## Usage

The tool is controlled through command-line parameters and a configuration file:

```bash
Usage: midi2mqtt [options]
Options:
  -list-ports     List available MIDI ports and exit
  -test          Run in test mode (print MIDI events to stdout)
  -all-events    Listen to all MIDI events
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
2025/01/07 22:27:24 INFO Using config file path=/etc/midi2mqtt.yaml
2025/01/07 22:27:24 INFO Started MIDI test mode
2025/01/07 22:27:24 INFO Listening on MIDI port port="MPK mini 3:MPK mini 3 MIDI 1"
2025/01/07 22:27:24 INFO Press Ctrl+C to exit
2025/01/07 22:27:25 INFO Received MIDI event data="{\n  \"timestamp\": \"2025-01-07T22:27:25.567315529+01:00\",\n  \"port\": \"MPK mini 3:MPK mini 3 MIDI 1 40:0\",\n  \"channel\": 0,\n  \"note\": 60,\n  \"key\": \"C\",\n  \"octave\": 4,\n  \"velocity\": 51,\n  \"event_type\": \"note_on\"\n}"
2025/01/07 22:27:25 INFO Received MIDI event data="{\n  \"timestamp\": \"2025-01-07T22:27:25.795211919+01:00\",\n  \"port\": \"MPK mini 3:MPK mini 3 MIDI 1 40:0\",\n  \"channel\": 0,\n  \"note\": 60,\n  \"key\": \"C\",\n  \"octave\": 4,\n  \"event_type\": \"note_off\"\n}"
```

## Configuration

The program looks for the configuration file (`midi2mqtt.yaml`) in the following locations, in order:

1. Current working directory (`./midi2mqtt.yaml`)
1. Directory containing the executable
1. On Linux/macOS:
   - `~/.config/midi2mqtt/midi2mqtt.yaml`
   - `/etc/midi2mqtt/midi2mqtt.yaml`
   - `/etc/midi2mqtt.yaml`
1. On Windows:
   - `%APPDATA%\midi2mqtt\midi2mqtt.yaml`

The first file found in this order will be used. The program will display which configuration file it is using when started.

The configuration file uses YAML format and supports the following settings:

For a complete configuration example with all available options and their descriptions, please refer to the [`midi2mqtt.yaml.template`](midi2mqtt.yaml.template) file included in this repository.

## Home Assistant Integration

There are two ways to integrate MIDI events with Home Assistant:

### 1. Custom JSON Integration

For mqtt events of publication type `custom_json`, you can add these MIDI events into Home Assistant by adding a configuration such as the following to your `configuration.yaml`:

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

#### Creating Automations with Custom JSON

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

### 2. Native Home Assistant MQTT Discovery

The `home_assistant` publication type provides native MQTT discovery integration with Home Assistant. When enabled, it automatically:

- Creates individual binary sensors for each MIDI key
- Uses MQTT discovery to automatically register devices and entities
- Shows the current state of each key (on/off) in real-time

To use this integration:

1. Enable the `home_assistant` publication type in your configuration:
   ```yaml
   mqtt_publications:
     - type: home_assistant
       enabled: true
       topic: "homeassistant/sensor/your_device_name"
       qos: 1
       retain: false
       device:
         identifiers: ["your_device_name"]
         name: "Your MIDI Device"
         manufacturer: "Device Manufacturer"
   ```

2. Home Assistant will automatically discover the MIDI keys as binary sensors
3. Each key will appear as a separate entity that shows whether it's currently pressed (on) or released (off)
4. You can use these sensors directly in your automations, scripts, and dashboards

This native integration provides a more seamless experience as it:
- Requires no manual configuration in Home Assistant
- Creates properly named and organized entities
- Provides real-time state updates
- Maintains state between Home Assistant restarts

## Known Issues / Untested

1. Documentation generated by LLM. Be aware of errors or mistakes.
1. MQTT with TLS and certificates is untested.
1. Most testing is currently done with note_on and note_off events. Other MIDI events may need additional testing.
1. Usage on Windows and Mac is untested.

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
