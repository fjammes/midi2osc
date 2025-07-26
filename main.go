package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/hypebeast/go-osc/osc"
	"gitlab.com/gomidi/midi/reader"
	"gitlab.com/gomidi/rtmididrv"
)

// sendOSC sends an OSC message to the specified address
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
		val, ok := value.(int32)
		if !ok {
			return fmt.Errorf("value must be int32 for type 'i'")
		}
		msg.Append(val)
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

// handleCC: reacts to specific CC messages
func handleCC(cc, val uint8, oscAddr, oscPath string) {
	if cc == 27 {
		var oscVal int32 = 0
		if val > 0 {
			oscVal = 1
		}
		err := sendOSC(oscAddr, oscPath, "i", oscVal)
		if err != nil {
			log.Printf("Failed to send OSC: %v", err)
		} else {
			log.Printf("Sent OSC: %s %s i %d", oscAddr, oscPath, oscVal)
		}
	}
}

func main() {
	oscAddress := "osc.tcp://clrinfopo18.home:22752"
	oscPath := "/Carla_Patchbay_4/0/set_active"

	// Init MIDI
	drv, err := rtmididrv.New()
	if err != nil {
		log.Fatalf("Could not open MIDI driver: %v", err)
	}
	defer drv.Close()

	ins, err := drv.Ins()
	if err != nil || len(ins) == 0 {
		log.Fatalf("No MIDI input devices found: %v", err)
	}

	in := ins[0]
	err = in.Open()
	if err != nil {
		log.Fatalf("Could not open MIDI input: %v", err)
	}
	defer in.Close()

	log.Printf("Listening on MIDI input: %s", in.String())

	rd := reader.New(
		reader.NoLogger(),
		reader.ControlChange(func(pos *reader.Position, channel, controller, value uint8) {
			log.Printf("Received CC%d val=%d", controller, value)
			handleCC(controller, value, oscAddress, oscPath)
		}),
	)

	// Run reader in goroutine
	go func() {
		if err := rd.ListenTo(in); err != nil {
			log.Fatalf("MIDI listen failed: %v", err)
		}
	}()

	// Wait for Ctrl+C to exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	log.Println("Shutting down gracefully.")
}
