package main

import (
	"testing"
	"time"
)

func TestDesktopPresenceUsesLockSleepAndSystemInput(t *testing.T) {
	for _, tc := range []struct {
		name             string
		locked, sleeping bool
		idle             time.Duration
		want             bool
	}{
		{"using another app", false, false, time.Second, true},
		{"locked", true, false, 0, false},
		{"sleeping", false, true, 0, false},
		{"idle at threshold", false, false, 5 * time.Minute, false},
		{"reading briefly", false, false, 4 * time.Minute, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopUserPresent(tc.locked, tc.sleeping, tc.idle); got != tc.want {
				t.Fatalf("present=%v, want %v", got, tc.want)
			}
		})
	}
}
