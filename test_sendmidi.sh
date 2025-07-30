#!/bin/bash

# Test script for sending MIDI messages using sendmidi


cc=$1
cc_value=$2

if [ -z "$cc" ] || [ -z "$cc_value" ]; then
  echo "Usage: $0 <cc_number> <cc_value>"
  exit 1
fi

sendmidi dev "MIDI Through Port-0" ch 1 cc "$cc" "$cc_value"