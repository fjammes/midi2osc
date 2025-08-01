package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/fjammes/midi2osc/resources"
	"github.com/hypebeast/go-osc/osc"
	"github.com/xthexder/go-jack"
	"gopkg.in/yaml.v3"
)

type OSCAction struct {
	Path  string      `yaml:"path"`
	Type  string      `yaml:"type"`
	Value interface{} `yaml:"value"`
}

type Mapping struct {
	CC      uint8       `yaml:"cc"`
	Value   uint8       `yaml:"value"`
	Actions []OSCAction `yaml:"actions"`
}

type Config struct {
	OscTarget string    `yaml:"osc_target"`
	Mappings  []Mapping `yaml:"mappings"`
}

type MidiEvent struct {
	CC      uint8
	Value   uint8
	Target  string
	Actions []OSCAction
}

var (
	portIn    *jack.Port
	ch        chan string // for printing midi events
	cfg       *Config
	eventChan chan MidiEvent // global channel for OSC events
)

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func sendOSC(target, path, t string, val interface{}) error {
	if !strings.HasPrefix(target, "osc.tcp://") {
		return fmt.Errorf("only osc.tcp:// supported")
	}
	addr := strings.TrimPrefix(target, "osc.tcp://")
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid OSC address format")
	}
	client := osc.NewClient(parts[0], atoi(parts[1]))
	msg := osc.NewMessage(path)
	switch t {
	case "i":
		msg.Append(int32(val.(int)))
	case "f":
		msg.Append(float32(val.(float64)))
	case "s":
		msg.Append(val.(string))
	case "T":
		msg.Append(true)
	case "F":
		msg.Append(false)
	default:
		return fmt.Errorf("unsupported OSC type: %s", t)
	}
	return client.Send(msg)
}

func atoi(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}

func process(nframes uint32) int {
	events := portIn.GetMidiEvents(nframes)

	if cfg == nil {
		// Ne pas logger ici pour ne pas bloquer JACK
		return 0
	}

	for _, event := range events {
		// Ne jamais bloquer dans le thread JACK :
		select {
		case ch <- fmt.Sprintf("%#v", event):
		default:
			// Si le chan est plein, on saute sans bloquer
		}

		if event.Buffer[0]&0xF0 == 0xB0 { // CC
			cc := event.Buffer[1]
			val := event.Buffer[2]

			for _, m := range cfg.Mappings {
				if m.CC == cc && m.Value == val {
					// Préparer une action à exécuter en dehors du thread JACK
					msg := MidiEvent{
						CC:      cc,
						Value:   val,
						Target:  cfg.OscTarget,
						Actions: m.Actions,
					}

					select {
					case eventChan <- msg:
					default:
						// Si le chan est plein, on ignore pour préserver le temps réel
					}
				}
			}
		}
	}
	return 0
}

func main() {

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	var err error
	cfgPath := flag.String("config", "", "Path to YAML config")
	flag.Parse()

	if *cfgPath == "" {
		err := yaml.Unmarshal([]byte(resources.MidiMappingYaml), &cfg)
		if err != nil {
			slog.Error("Failed to parse embedded config", slog.Any("err", err))
			os.Exit(1)
		}
		slog.Info("Loaded embedded config", slog.String("osc_target", cfg.OscTarget))
	} else {
		cfg, err = loadConfig(*cfgPath)
		if err != nil {
			slog.Error("Failed to load config", slog.String("file", *cfgPath), slog.Any("err", err))
			os.Exit(1)
		}
		slog.Info("Loaded config", slog.String("osc_target", cfg.OscTarget))
	}

	client, status := jack.ClientOpen("midi2osc", jack.NoStartServer)
	if client == nil || status != 0 {
		log.Fatalf("Failed to open JACK client: status %d", status)
	}
	defer client.Close()

	portIn = client.PortRegister("midi_in", jack.DEFAULT_MIDI_TYPE, jack.PortIsInput, 0)
	if portIn == nil {
		log.Fatal("Failed to register MIDI input port")
	}
	slog.Info("Registered MIDI input port", slog.String("name", portIn.GetName()))

	eventChan = make(chan MidiEvent, 64) // global
	ch = make(chan string, 64)
	go func() {
		for line := range ch {
			slog.Debug("Raw MIDI", "event", line)
		}
	}()
	go func() {
		for msg := range eventChan {
			for _, act := range msg.Actions {
				err := sendOSC(msg.Target, act.Path, act.Type, act.Value)
				if err != nil {
					slog.Error("Failed to send OSC", slog.String("path", act.Path), slog.Any("err", err))
				} else {
					slog.Info("OSC sent", slog.String("path", act.Path), slog.Any("val", act.Value))
				}
			}
		}
	}()

	if code := client.SetProcessCallback(process); code != 0 {
		slog.Error("Failed to set process callback:", slog.Any("err", jack.StrError(code)))
		return
	}
	client.OnShutdown(func() {
		close(ch)
	})

	if code := client.Activate(); code != 0 {
		slog.Error("Failed to activate JACK client", slog.Any("err", jack.StrError(code)))
		return
	}
	slog.Info("JACK client active", slog.String("name", client.GetName()))

	// Wait for Ctrl+C
	str, more := "", true
	for more {
		str, more = <-ch
		fmt.Printf("Midi Event: %s\n", str)
	}
	slog.Info("Exiting...")
}
