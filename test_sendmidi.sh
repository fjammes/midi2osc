#!/bin/bash

# Test script for sending MIDI messages using sendmidi


cc=$1
cc_value=$2

sendmidi dev "MIDI Through Port-0" cc "$cc" "$cc_value"
