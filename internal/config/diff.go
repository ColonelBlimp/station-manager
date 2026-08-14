package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// FieldChange is one field that moved between two configs, rendered for the
// log. From and To are empty when the value is withheld (Secret or Redacted);
// in that case they carry presence words instead, never the value.
type FieldChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
	// Secret marks a known credential path. The value is never rendered — only
	// "(set)" / "(unset)", mirroring the API's credentials_set / password_set
	// masking on the read side.
	Secret bool `json:"secret,omitempty"`
	// Redacted marks a path that is not on the value allowlist. The change is
	// still reported; only the value is withheld. This is the default for
	// anything new, which is the whole point of an allowlist — a field added
	// later is silent about its contents until someone decides otherwise,
	// rather than leaking on the day it lands.
	Redacted bool `json:"redacted,omitempty"`
}

const (
	presenceSet   = "(set)"
	presenceUnset = "(unset)"
)

// Diff reports what moved between two configs as dotted JSON paths, ready to
// log. It compares the MARSHALLED shape rather than walking the struct by
// reflection, so the paths it emits are the ones the operator sees in
// config.json — logging_station.station_callsign, not LoggingStation.
//
// Values are governed by the operator's 2026-08-02 ruling: non-secret fields
// carry their value, secrets carry only presence, and the classification is an
// allowlist so an unrecognised path is redacted rather than logged. See
// valuePolicy.
//
// Returns nil when nothing moved — the caller logs nothing in that case, which
// is what keeps a whole-tab save from writing a line about a change it did not
// make.
func Diff(before, after Config) []FieldChange {
	b, err := toJSONMap(before)
	if err != nil {
		return nil
	}
	a, err := toJSONMap(after)
	if err != nil {
		return nil
	}

	var out []FieldChange
	diffValue("", b, a, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

func toJSONMap(cfg Config) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// diffValue walks two decoded JSON values in step, appending a FieldChange for
// every differing leaf. Objects recurse by key; lists are keyed by identity
// (see listKey) so a forwarder is reported as forwarders[qrz], not
// forwarders[2] — an index shifts when an unrelated entry is removed, and a
// path that moves is a path the operator cannot grep for.
func diffValue(path string, before, after any, out *[]FieldChange) {
	bObj, bIsObj := before.(map[string]any)
	aObj, aIsObj := after.(map[string]any)
	if bIsObj || aIsObj {
		for _, k := range unionKeys(bObj, aObj) {
			diffValue(join(path, k), bObj[k], aObj[k], out)
		}
		return
	}

	bList, bIsList := before.([]any)
	aList, aIsList := after.([]any)
	if bIsList || aIsList {
		bKeyed, bOrder, bPlain := keyList(bList)
		aKeyed, aOrder, aPlain := keyList(aList)
		if bPlain || aPlain {
			// A list of scalars (action_filter, rig_modes) has no stable
			// identity per element, so it is compared whole — reporting
			// "[a b] -> [a c]" is more use than three positional edits.
			if renderScalarList(bList) != renderScalarList(aList) {
				emit(path, renderScalarList(bList), renderScalarList(aList), out)
			}
			return
		}
		// Order can be data for keyed lists, so retain the established reorder
		// record generally. lookup.chain is the exception under ADR 0068:
		// explicit priority is authoritative and array order is only a
		// serialisation detail. Its meaningful reorder appears as changes to the
		// providers' priority leaves instead.
		//
		// Reported only when the MEMBERSHIP is unchanged: adding or removing an
		// entry necessarily shifts the sequence, and firing then would
		// double-report every list edit and drain the signal of meaning.
		if path != "lookup.chain" && sameMembers(bOrder, aOrder) && !sameSequence(bOrder, aOrder) {
			emit(path, renderKeyOrder(bOrder), renderKeyOrder(aOrder), out)
		}
		for _, k := range unionKeys(bKeyed, aKeyed) {
			diffValue(fmt.Sprintf("%s[%s]", path, k), bKeyed[k], aKeyed[k], out)
		}
		return
	}

	bs, as := renderScalar(before), renderScalar(after)
	if bs != as {
		emit(path, bs, as, out)
	}
}

// emit applies the value policy to one changed leaf.
func emit(path, from, to string, out *[]FieldChange) {
	switch policy := valuePolicy(path); policy {
	case policySecret:
		*out = append(*out, FieldChange{
			Field: path, From: presence(from), To: presence(to), Secret: true,
		})
	case policyRedact:
		*out = append(*out, FieldChange{
			Field: path, From: presence(from), To: presence(to), Redacted: true,
		})
	case policyURL:
		*out = append(*out, FieldChange{Field: path, From: originOf(from), To: originOf(to)})
	default:
		*out = append(*out, FieldChange{Field: path, From: originIfURL(from), To: originIfURL(to)})
	}
}

type policy int

const (
	policyValue policy = iota
	policySecret
	policyRedact
	policyURL
)

// secretLeaves are leaf names whose value never reaches the log regardless of
// where they appear. Checked BEFORE the allowlist so a credential that lands
// under an allowed prefix cannot be logged by inheriting it — belt and braces
// over the allowlist below, not a substitute for it.
var secretLeaves = map[string]bool{
	"password": true, "passwd": true, "secret": true, "token": true,
	"api_key": true, "apikey": true, "key": true, "credentials": true,
}

// urlLeaves log scheme + host only: a provider key can ride in the query
// string, so the tail is dropped. Same instinct as csrf.go, which parses the
// Origin rather than trusting the raw header.
//
// This list is a FLOOR, not the mechanism — originIfURL reduces any
// URL-shaped value wherever it appears. Naming the fields was the original
// design and it was wrong: forwarders[x].endpoints is a map keyed by ACTION,
// so its URLs sit at leaves called "insert" and "delete" and sailed straight
// past a check that looked for leaves called "url". Asking the question of the
// field name is a denylist; asking it of the value is not.
var urlLeaves = map[string]bool{"url": true, "view_url": true}

// valueAllowlist is the set of path prefixes whose leaves may carry their
// value into the log. ALLOWLIST, not denylist, on the operator's 2026-08-02
// ruling: a denylist fails open, so a config field added six months from now
// would leak by default instead of being redacted by default.
//
// Adding a prefix here is a decision to publish those values into a 0644 file
// that came from a 0600 one. Check what the block can hold before extending it.
// valueAllowlistExact matches a WHOLE path and nothing else. Every scalar
// belongs here rather than in the prefix list: "version" as a prefix also
// matches "versionsecret", and "smtp.username" also matches
// "smtp.username_token". The container paths (forwarders, rigs, operators) are
// here too, so a reorder — which is reported against the container — renders
// its order instead of a useless redacted "(set) -> (set)".
var valueAllowlistExact = []string{
	"version", "data_dir", "useragent", "socket_path", "setup_complete",
	"default_logbook_id", "default_operator", "default_rig_id",
	"bridge_enabled", "ft8_enabled", "restore_rig_on_mode_switch",
	"forwarders", "rigs", "operators",
	"smtp.enabled", "smtp.host", "smtp.port", "smtp.username", "smtp.from",
	"smtp.default_recipient", "smtp.starttls", "smtp.timeout_sec",
}

// valueAllowlistPrefix matches a path and everything beneath it. EVERY entry
// must end in "." or "[" — a bare word matches sibling top-level fields, which
// is how `forwarders_api_token` came to be publishable by an entry meant for
// the forwarders block (clean-room review of 479245e9). That constraint is
// enforced on the LIST itself, not on the entries someone happens to remember:
// TestValueAllowlist_PrefixesAreDelimiterBound.
var valueAllowlistPrefix = []string{
	"server.", "datastore.", "logging.", "logging_station.",
	"bridge.", "bridge_timeouts.", "bridge_tune.",
	"ft8.", "psk_reporter.", "map.", "qsl.", "mailer.", "lookup.",
	"forwarders[", "rigs[", "operators[",
}

func valuePolicy(path string) policy {
	leaf := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		leaf = path[i+1:]
	}
	// Credentials are a map, so its children are named by the operator's own
	// keys (credentials.api, credentials.whatever) — catch the container in the
	// path, not just the leaf.
	if secretLeaves[leaf] || strings.Contains(path, ".credentials.") {
		return policySecret
	}
	if urlLeaves[leaf] {
		return policyURL
	}
	for _, exact := range valueAllowlistExact {
		if path == exact {
			return policyValue
		}
	}
	for _, prefix := range valueAllowlistPrefix {
		if strings.HasPrefix(path, prefix) {
			return policyValue
		}
	}
	return policyRedact
}

// originOf reduces a URL to scheme://host, dropping path, query and fragment.
// An unparseable value is reported as present rather than echoed — the reason
// it failed to parse might be the interesting part, but not at the cost of
// printing it.
func originOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return presenceSet
	}
	return u.Scheme + "://" + u.Host
}

// originIfURL reduces a URL-SHAPED value to scheme://host and leaves anything
// else alone. Applied to every value-logged field, because a URL can live at a
// leaf with any name at all (see urlLeaves). The "://" pre-check keeps
// url.Parse — which accepts almost any string — from quietly mangling ordinary
// values such as a rig model or a grid square.
func originIfURL(v string) string {
	if !strings.Contains(v, "://") {
		return v
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		// Shaped like a URL but unparseable. Report presence rather than echo
		// it: whatever made it fail to parse is not worth printing into a 0644
		// file to find out.
		return presenceSet
	}
	return u.Scheme + "://" + u.Host
}

func presence(v string) string {
	if v == "" {
		return presenceUnset
	}
	return presenceSet
}

// listKey returns the field that identifies a list element across saves.
func listKey(m map[string]any) (string, bool) {
	for _, field := range []string{"name", "callsign", "id"} {
		if v, ok := m[field]; ok {
			if s := renderScalar(v); s != "" {
				return s, true
			}
		}
	}
	return "", false
}

// keyList indexes a list of objects by identity, returning both the map and
// the keys IN LIST ORDER. Some keyed config lists retain meaningful ordering;
// lookup.chain is explicitly excluded by diffValue because ADR 0068 makes its
// numeric priority authoritative.
// The final return reports that the list is NOT keyable (scalars, or objects
// with no identity field), in which case the caller compares it whole.
func keyList(list []any) (map[string]any, []string, bool) {
	if len(list) == 0 {
		return map[string]any{}, nil, false
	}
	out := make(map[string]any, len(list))
	order := make([]string, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, nil, true
		}
		k, ok := listKey(m)
		if !ok {
			return nil, nil, true
		}
		out[k] = m
		order = append(order, k)
	}
	return out, order, false
}

// sameMembers reports whether two key sequences contain the same identities,
// regardless of position.
func sameMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, k := range a {
		seen[k]++
	}
	for _, k := range b {
		seen[k]--
		if seen[k] < 0 {
			return false
		}
	}
	return true
}

func sameSequence(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func renderKeyOrder(keys []string) string {
	return "[" + strings.Join(keys, " ") + "]"
}

func renderScalarList(list []any) string {
	if len(list) == 0 {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, v := range list {
		parts = append(parts, renderScalar(v))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func renderScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	case float64:
		// JSON numbers decode as float64; render integers without a decimal
		// tail so a port reads as 587 rather than 587.000000.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func unionKeys(a, b map[string]any) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
