package main

import (
	"cmp"
	"retty/internal/remote"
	"slices"
)

func mergeIncomingDevices(first, second []remote.ConnectedDevice) []remote.ConnectedDevice {
	var records []remote.ConnectedDevice
	seen := map[string]bool{}
	all := append(slices.Clone(first), second...)
	slices.SortStableFunc(all, func(a, b remote.ConnectedDevice) int {
		return cmp.Compare(b.Connected.UnixNano(), a.Connected.UnixNano())
	})
	for _, d := range all {
		if d.ID == "" || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		records = append(records, d)
		if len(records) == 12 {
			break
		}
	}
	return records
}
