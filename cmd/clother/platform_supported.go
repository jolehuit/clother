//go:build darwin || linux

package main

// platformSupported gates main on the two operating systems Clother actually
// supports. Everything else gets one clear refusal instead of a diffuse failure
// later on (symlinks, /dev/tty, stty).
const platformSupported = true
