#!/bin/bash

# Test script for sending MIDI messages using sendmidi

msg=$1

sendmidi dev "MIDI Through Port-0" cc 27 "$msg"
