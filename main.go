package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/hypebeast/go-osc/osc"
	"gitlab.com/gomidi/midi"
	"gitlab.com/gomidi/midi/reader"
	"gitlab.com/gomidi/rtmididrv"
	"gopkg.in/yaml.v3"
)

// OSCAction represents one OSC message to send
type OSCAction struct {
	Path  string      `yaml:"path"`
	Type  string      `yaml:"type"`
	Value interface{} `yaml:"value"`
}

// Mapping ties a CC + value to a list of OSC actions
type Mapping struct {
	CC      uint8       `yaml:"cc"`
	Value   uint8       `yaml:"value"`
	Actions []OSCAction `yaml:"actions"`
}

// Config is the full YAML structure
type Config struct {
	OscTarget string    `yaml:"osc_target"`
	Mappings  []Mapping `yaml:"mappings"`
}

func main() {
	// CLI flags
	configPath := flag.String("config", "mapping.yaml", "Path to the YAML config file")
	midiInputName := flag.String("midi", "", "Name of the MIDI input device to use")
	listMidi := flag.Bool("list-midi", false, "List available MIDI input devices and exit")
	flag.Parse()

	// Setup structured logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load config
	config, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load config", slog.String("file", *configPath), slog.Any("error", err))
		os.Exit(1)
	}
	slog.Info("Loaded config", slog.String("osc_target", config.OscTarget))

	// Init MIDI
	drv, err := rtmididrv.New()
	if err != nil {
		slog.Error("Failed to init MIDI driver", slog.Any("error", err))
		os.Exit(1)
	}
	defer drv.Close()

	ins, err := drv.Ins()
	if err != nil || len(ins) == 0 {
		slog.Error("No MIDI input devices found", slog.Any("error", err))
		os.Exit(1)
	}

	if *listMidi {
		fmt.Println("Available MIDI input devices:")
		for _, i := range ins {
			fmt.Println("-", i.String())
		}
		return
	}

	// Select MIDI input
	var in midi.In
	found := false

	if *midiInputName == "" {
		in = ins[0]
		slog.Warn("No MIDI input specified, using default", slog.String("device", in.String()))
	} else {
		for _, i := range ins {
			if i.String() == *midiInputName {
				in = i
				found = true
				break
			}
		}
		if !found {
			slog.Error("MIDI input not found", slog.String("requested", *midiInputName))
			os.Exit(1)
		}
	}

	err = in.Open()
	if err != nil {
		slog.Error("Failed to open MIDI input", slog.String("device", in.String()), slog.Any("error", err))
		os.Exit(1)
	}
	defer in.Close()

	slog.Info("Listening on MIDI input", slog.String("device", in.String()))

	// MIDI message handler
	rd := reader.New(
		reader.NoLogger(),
		reader.ControlChange(func(pos *reader.Position, channel, cc, val uint8) {
			for _, m := range config.Mappings {
				if m.CC == cc && m.Value == val {
					slog.Info("Matched MIDI CC",
						slog.Int("cc", int(cc)),
						slog.Int("value", int(val)),
						slog.Int("actions", len(m.Actions)),
					)
					for _, action := range m.Actions {
						err := sendOSC(config.OscTarget, action.Path, action.Type, action.Value)
						if err != nil {
							slog.Error("Failed to send OSC",
								slog.String("path", action.Path),
								slog.Any("value", action.Value),
								slog.Any("error", err),
							)
						} else {
							slog.Info("Sent OSC",
								slog.String("path", action.Path),
								slog.String("type", action.Type),
								slog.Any("value", action.Value),
							)
						}
					}
				}
			}
		}),
	)

	// Start MIDI listening
	go func() {
		if err := rd.ListenTo(in); err != nil {
			slog.Error("MIDI listen failed", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Graceful shutdown on Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan
	slog.Info("Shutting down gracefully")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func sendOSC(oscAddress, oscPath, oscType string, value interface{}) error {
	if !strings.HasPrefix(oscAddress, "osc.tcp://") {
		return fmt.Errorf("only osc.tcp:// is supported")
	}

	addrParts := strings.Split(strings.TrimPrefix(oscAddress, "osc.tcp://"), ":")
	if len(addrParts) != 2 {
		return fmt.Errorf("invalid OSC address format")
	}
	host := addrParts[0]
	port := addrParts[1]

	client := osc.NewClient(host, atoi(port))
	msg := osc.NewMessage(oscPath)

	switch oscType {
	case "i":
		msg.Append(int32(value.(int)))
	case "f":
		msg.Append(float32(value.(float64)))
	case "s":
		msg.Append(value.(string))
	case "T":
		msg.Append(true)
	case "F":
		msg.Append(false)
	default:
		return fmt.Errorf("unsupported OSC type: %s", oscType)
	}

	return client.Send(msg)
}

func atoi(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}
