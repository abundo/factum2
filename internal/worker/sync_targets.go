package worker

import (
	"slices"

	"github.com/abundo/factum2/models"
)

// SyncTargets lists the systems that can be synced from the web UI. Each
// name doubles as the role/command name a factum2-worker instance activates
// to handle it - each one matches a corresponding "factum2-<name> sync" CLI
// command.
var SyncTargets = []string{"becs", "netbox", "lime", "librenms", "icinga", "oxidized", "prometheus", "dns", "device-sync"}

// sourceSyncTargets are the upstream systems synced *into* factum, as
// opposed to the downstream destinations synced *from* factum - see
// AGENTS.md's "Sync model" section. Mirrors SyncOverviewPage.vue's
// targetInfo section:'source' entries; "device-sync" isn't a source or a
// classic destination (it reconciles factum/netbox against live device
// state) but the frontend groups it with destinations by default, so
// SequencedSyncAllTargets does the same.
var sourceSyncTargets = map[string]bool{
	"becs":   true,
	"netbox": true,
	"lime":   true,
}

func IsValidSyncTarget(target string) bool {
	return slices.Contains(SyncTargets, target)
}

// SequencedSyncAllTargets orders targets (expected to already be filtered
// to enabled ones via EnabledSyncTargets) sources-first, destinations-
// second, preserving each group's relative order - the dispatch order
// StartJob's sequential batch path uses for "Sync all", so a destination
// sync never races a source sync that's still refreshing the data it
// reads.
func SequencedSyncAllTargets(targets []string) []string {
	ordered := make([]string, 0, len(targets))
	for _, t := range targets {
		if sourceSyncTargets[t] {
			ordered = append(ordered, t)
		}
	}
	for _, t := range targets {
		if !sourceSyncTargets[t] {
			ordered = append(ordered, t)
		}
	}
	return ordered
}

// EnabledSyncTargets filters SyncTargets down to the ones activated in
// Settings (the same *_enabled switches the admin Settings page edits). A
// nil pointer (the zero value until an admin explicitly flips the switch)
// means "not enabled".
func EnabledSyncTargets(s *models.Settings) []string {
	enabled := map[string]*bool{
		"becs":        s.BecsEnabled,
		"netbox":      s.NetboxEnabled,
		"lime":        s.LimeEnabled,
		"librenms":    s.LibrenmsEnabled,
		"icinga":      s.IcingaEnabled,
		"oxidized":    s.OxidizedEnabled,
		"prometheus":  s.PrometheusEnabled,
		"dns":         s.DnsEnabled,
		"device-sync": s.DeviceSyncEnabled,
	}
	out := make([]string, 0, len(SyncTargets))
	for _, t := range SyncTargets {
		if enabled[t] != nil && *enabled[t] {
			out = append(out, t)
		}
	}
	return out
}
