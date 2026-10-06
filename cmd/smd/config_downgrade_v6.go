package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// rebuildV5Forwarders is the data-aware half of `smd config-downgrade` from
// config v6 (ADR 0082 part 4, W-0021 5C). Once Home's binding seed has
// committed, the bindings — not config.json — hold each destination's name,
// on/off state and per-logbook credentials, so the v5 entries are rebuilt from
// Home's closed file, confirmed by its identity, whether or not the file was
// stripped yet. Before the seed the file still holds them and passes through
// for the plain stamp. Runs before `smd db-downgrade`: below log schema 14
// there are no bindings to read.
func rebuildV5Forwarders(path string, data []byte) ([]byte, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	stripped, err := documentStripped(data)
	if err != nil {
		return nil, err
	}
	peek, err := archive.ReadHomeBindings(cfg)
	if err != nil {
		return nil, fmt.Errorf("%w — the v5 entries are rebuilt from Home's destination bindings, so Home's file must be readable "+
			"(run this before smd db-downgrade)", err)
	}
	if !peek.Seeded {
		if !stripped {
			return data, nil
		}
		why := "Home's destination bindings were never seeded"
		if !peek.HasBindingsTable {
			why = "Home's file holds no destination bindings (its log schema is below 14 — was smd db-downgrade run first?)"
		}
		return nil, fmt.Errorf("config.json holds v6 station accounts, but %s, so nothing can rebuild the v5 entries; "+
			"restore config.v5.json, the copy written beside config.json before the strip", why)
	}
	defaultID := cfg.DefaultLogbookID
	if peek.HasIdentity && peek.Identity.DefaultLogbookID > 0 {
		defaultID = peek.Identity.DefaultLogbookID
	}
	rebuilt, err := recombineV5Forwarders(cfg.Forwarders, peek, defaultID)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	raw, err := json.Marshal(rebuilt)
	if err != nil {
		return nil, err
	}
	var entries []any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	doc["forwarders"] = entries
	return json.MarshalIndent(doc, "", "  ")
}

// documentStripped reports whether any forwarder entry has lost the v5 name.
func documentStripped(data []byte) (bool, error) {
	var shape struct {
		Forwarders []struct {
			Name string `json:"name"`
		} `json:"forwarders"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return false, fmt.Errorf("parse config: %w", err)
	}
	for _, f := range shape.Forwarders {
		if f.Name == "" {
			return true, nil
		}
	}
	return false, nil
}

// recombineV5Forwarders folds each station account and Home's bindings of its
// type into ONE v5 entry — v5 has one entry per destination serving every
// logbook — or refuses, naming the destination and logbooks, when that would
// change an on/off state or a credential: every live logbook must be in one
// state (an UNBOUND logbook counts as off; acceptance case 9), and every
// binding must hold the same per-logbook credentials. The name is migration
// 0015 down's collapse target: the first legacy name, else the default
// logbook's binding, else the first binding name; an account with no binding
// becomes a disabled entry named after its type.
func recombineV5Forwarders(accounts []types.ForwarderConfig, peek sqlite.PeekedBindings, defaultID int64) ([]types.ForwarderConfig, error) {
	names := make(map[int64]string, len(peek.Logbooks))
	for _, lb := range peek.Logbooks {
		names[lb.ID] = lb.Name
	}
	taken := map[string]bool{}
	out := make([]types.ForwarderConfig, 0, len(accounts))
	var unbound []int
	for _, account := range accounts {
		var bindings []types.LogbookDestination
		for _, b := range peek.Bindings {
			if b.Destination == account.Type {
				bindings = append(bindings, b)
			}
		}
		fc := account
		fc.Credentials = nil
		if len(bindings) == 0 {
			fc.Enabled = false
			fc.Credentials = account.Credentials
			out = append(out, fc)
			unbound = append(unbound, len(out)-1)
			continue
		}
		enabled, err := commonState(account.Type, bindings, peek.Logbooks, names)
		if err != nil {
			return nil, err
		}
		logbookKeys, err := commonLogbookCredentials(account.Type, bindings, names)
		if err != nil {
			return nil, err
		}
		fc.Enabled = enabled
		fc.Name = collapseName(bindings, defaultID)
		if fc.Credentials, err = v5Credentials(account, logbookKeys); err != nil {
			return nil, err
		}
		taken[fc.Name] = true
		out = append(out, fc)
	}
	for _, i := range unbound {
		name := out[i].Type
		for n := 2; taken[name]; n++ {
			name = fmt.Sprintf("%s-%d", out[i].Type, n)
		}
		out[i].Name = name
		taken[name] = true
	}
	return out, nil
}

func commonState(typ string, bindings []types.LogbookDestination, logbooks []types.Logbook, names map[int64]string) (bool, error) {
	on := map[int64]bool{}
	for _, b := range bindings {
		on[b.LogbookID] = b.Enabled
	}
	var onFor, offFor []string
	for _, lb := range logbooks {
		if on[lb.ID] {
			onFor = append(onFor, lb.Name)
		} else {
			offFor = append(offFor, lb.Name)
		}
	}
	if len(onFor) > 0 && len(offFor) > 0 {
		return false, fmt.Errorf("%s is on for %s and off for %s (a logbook with no binding counts as off); v5 holds one on/off state "+
			"for every logbook — make them agree in Settings → Forwarding, or restore config.v5.json", typ, strings.Join(onFor, ", "), strings.Join(offFor, ", "))
	}
	return len(onFor) > 0, nil
}

// commonLogbookCredentials returns the one per-logbook credential set every
// binding of the type holds, or refuses naming the logbooks — never a value.
func commonLogbookCredentials(typ string, bindings []types.LogbookDestination, names map[int64]string) (map[string]json.RawMessage, error) {
	var first map[string]json.RawMessage
	var firstCanon string
	for i, b := range bindings {
		keys, canon, err := logbookScoped(typ, b.Credentials)
		if err != nil {
			return nil, fmt.Errorf("%s binding for %s: %w", typ, names[b.LogbookID], err)
		}
		if i == 0 {
			first, firstCanon = keys, canon
			continue
		}
		if canon != firstCanon {
			return nil, fmt.Errorf("%s: the credentials differ between %s and %s; v5 holds one set for every logbook — "+
				"make them agree in Settings → Forwarding, or restore config.v5.json", typ, names[bindings[0].LogbookID], names[b.LogbookID])
		}
	}
	return first, nil
}

func logbookScoped(typ string, raw json.RawMessage) (map[string]json.RawMessage, string, error) {
	all, ok := credentialMap(raw)
	if !ok {
		return nil, "", fmt.Errorf("credentials are not a JSON object")
	}
	keys := map[string]json.RawMessage{}
	canon := map[string]any{}
	for _, k := range forwarding.LogbookScopedKeys(typ) {
		v, present := all[k]
		if !present {
			continue
		}
		keys[k] = v
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			return nil, "", fmt.Errorf("credential %q does not parse", k)
		}
		canon[k] = decoded
	}
	b, err := json.Marshal(canon) // map keys marshal sorted
	if err != nil {
		return nil, "", err
	}
	return keys, string(b), nil
}

// collapseName mirrors migration 0015 down's target, so the queue rows the log
// downgrade renames and the v5 entry agree.
func collapseName(bindings []types.LogbookDestination, defaultID int64) string {
	byID := append([]types.LogbookDestination(nil), bindings...)
	sort.Slice(byID, func(i, j int) bool { return byID[i].ID < byID[j].ID })
	for _, b := range byID {
		if b.LegacyName != "" {
			return b.LegacyName
		}
	}
	for _, b := range byID {
		if b.LogbookID == defaultID {
			return b.ForwarderName
		}
	}
	min := byID[0].ForwarderName
	for _, b := range byID[1:] {
		if b.ForwarderName < min {
			min = b.ForwarderName
		}
	}
	return min
}

// v5Credentials lays the bindings' per-logbook keys over the account's own.
func v5Credentials(account types.ForwarderConfig, logbookKeys map[string]json.RawMessage) (json.RawMessage, error) {
	merged, ok := credentialMap(account.Credentials)
	if !ok {
		return nil, fmt.Errorf("the %s station account's credentials are not a JSON object", account.Type)
	}
	for k, v := range logbookKeys {
		merged[k] = v
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return json.Marshal(merged)
}
