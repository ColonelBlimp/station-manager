package main

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/clublog"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// v5RecoveryCopyName is the once-only, owner-only copy of the station's config
// as the v5 shape held it, written before the first strip (ruling R3,
// 2026-10-06). It is historical recovery material for a rollback that went
// wrong — not a guaranteed lossless rollback after later edits — and nothing
// ever reads it back automatically.
const v5RecoveryCopyName = "config.v5.json"

// stripLegacyForwarderFieldsAfterSeed removes the deprecated v5 fields from
// config.json once the adopted Home archive's binding seed has committed (ADR
// 0082 part 4, W-0021 5C): each forwarder entry keeps only its station-account
// fields. Never with another archive active — Home's seed is what made the
// fields redundant, and only Home's open file proves it. The recovery copy is
// written first; when it cannot be, the strip waits for a later start. A failed
// strip write leaves the file as it was and is retried at the next start. No
// failure here fails the start: the bindings, not these fields, route uploads.
func (d *daemon) stripLegacyForwarderFieldsAfterSeed(ctx context.Context) {
	if d.paths.Entry != nil && d.paths.Entry.Ownership != types.QsoArchiveOwnershipLegacy {
		return
	}
	seededAt, err := d.db.DestinationBindingsSeededAtWithContext(ctx)
	if err != nil || seededAt == nil {
		return
	}
	if !hasLegacyForwarderFields(d.cfgSvc.Snapshot().Forwarders) {
		return
	}
	recovered, err := ensureV5RecoveryCopy(d.cfgSvc.Path, d.configAtStart)
	if err != nil {
		d.logger.WarnWith().Err(err).Str("copy", v5RecoveryCopyName).
			Msg("config v6: the v5 recovery copy could not be written; config.json keeps its deprecated forwarder fields until a later start can copy them")
		return
	}
	if recovered != "" {
		d.logger.InfoWith().Str("copy", v5RecoveryCopyName).Str("source", recovered).
			Msg("config v6: wrote the one-time v5 recovery copy before stripping config.json")
	}
	if _, err := d.cfgSvc.Update(func(c *config.Config) error {
		stripLegacyForwarderFields(c.Forwarders)
		return nil
	}); err != nil {
		d.logger.WarnWith().Err(err).
			Msg("config v6: stripping the deprecated forwarder fields from config.json failed; retried at the next start")
		return
	}
	d.logger.InfoWith().Msg("config v6: config.json now holds station accounts only; names, on/off states and per-logbook credentials live in Home's destination bindings")
}

// hasLegacyForwarderFields reports whether any entry still carries a deprecated
// v5 field: a name, an enabled flag, or a logbook-scoped credential key.
func hasLegacyForwarderFields(fwds []types.ForwarderConfig) bool {
	for _, fc := range fwds {
		if fc.Name != "" || fc.Enabled {
			return true
		}
		creds, ok := credentialMap(fc.Credentials)
		if !ok {
			continue
		}
		for _, k := range forwarding.LogbookScopedKeys(fc.Type) {
			if _, present := creds[k]; present {
				return true
			}
		}
	}
	return false
}

// stripLegacyForwarderFields leaves each entry its station-account fields. A
// credential blob that is not a JSON object is left as it is: what it holds
// cannot be classified, and keeping it is the recoverable choice.
func stripLegacyForwarderFields(fwds []types.ForwarderConfig) {
	for i := range fwds {
		fc := &fwds[i]
		fc.Name = ""
		fc.Enabled = false
		creds, ok := credentialMap(fc.Credentials)
		if !ok {
			continue
		}
		for _, k := range forwarding.LogbookScopedKeys(fc.Type) {
			delete(creds, k)
		}
		fc.Credentials = nil
		if len(creds) > 0 {
			if b, err := json.Marshal(creds); err == nil {
				fc.Credentials = b
			}
		}
	}
}

func credentialMap(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, true
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}

// ensureV5RecoveryCopy writes config.v5.json beside config.json once and never
// overwrites it. Its source is, in order: the v5 bytes this start read before
// persisting the migrated file (the original document); else the file on disk,
// when it is v6 and still unstripped, re-stamped as v5 — "recovered at start"
// — which only stands if the result validates as a complete v5 document. It
// takes persistResolvedConfig's guarded ClubLog scrub. Returns the source used
// ("" when a copy already existed).
func ensureV5RecoveryCopy(cfgPath string, configAtStart []byte) (string, error) {
	const op errors.Op = "smd.ensureV5RecoveryCopy"
	dst := filepath.Join(filepath.Dir(cfgPath), v5RecoveryCopyName)
	if info, err := os.Lstat(dst); err == nil {
		// Only a regular file is a copy; anything else under the name means no
		// copy can be confirmed, and the strip must wait.
		if !info.Mode().IsRegular() {
			return "", errors.New(op).WithMsgf("%s exists but is not a regular file", v5RecoveryCopyName)
		}
		removeStaleStaging(dst)
		return "", nil
	} else if !os.IsNotExist(err) {
		return "", errors.New(op).WithErr(err).WithMsg("check for an existing recovery copy")
	}
	doc, source, err := v5RecoverySource(cfgPath, configAtStart)
	if err != nil {
		return "", errors.New(op).WithErr(err)
	}
	if doc, err = scrubClubLogAppKey(doc); err != nil {
		return "", errors.New(op).WithErr(err)
	}
	if err := writeOnce(dst, doc); err != nil {
		return "", errors.New(op).WithErr(err)
	}
	removeStaleStaging(dst)
	return source, nil
}

// removeStaleStaging deletes staging files an interrupted copy left beside
// the published one (they hold credentials); best-effort, regular files only.
func removeStaleStaging(dst string) {
	stale, _ := filepath.Glob(dst + "*.tmp")
	for _, p := range stale {
		if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() {
			_ = os.Remove(p)
		}
	}
}

// v5RecoverySource picks the copy's content and checks that v5 could load it:
// the rules v6 relaxed hold (checkV5Compatible), and the document otherwise
// validates as this build's config. A document that fails either is not a
// faithful v5 copy.
func v5RecoverySource(cfgPath string, configAtStart []byte) ([]byte, string, error) {
	var doc []byte
	var source string
	if v, err := config.DocumentSchemaVersion(configAtStart); err == nil && v == 5 {
		doc, source = configAtStart, "the v5 file read at this start"
	} else {
		current, err := os.ReadFile(cfgPath)
		if err != nil {
			return nil, "", fmt.Errorf("read config.json: %w", err)
		}
		if v, err := config.DocumentSchemaVersion(current); err != nil || v != 6 {
			return nil, "", fmt.Errorf("config.json is not a v6 document this build can re-stamp (version %d)", v)
		}
		if doc, err = config.DowngradeDocument(current, 5); err != nil {
			return nil, "", fmt.Errorf("config.json cannot be represented as v5: %w", err)
		}
		source = "config.json re-stamped as v5 at this start (recovered, not the original pre-upgrade bytes)"
	}
	if err := validateV5Document(doc); err != nil {
		return nil, "", err
	}
	return doc, source, nil
}

func validateV5Document(doc []byte) error {
	if err := checkV5Compatible(doc); err != nil {
		return err
	}
	// Validated in memory: the candidate holds credentials and is never staged
	// anywhere but its own owner-only destination.
	if err := config.CheckDocument(v5RecoveryCopyName, doc); err != nil {
		return fmt.Errorf("the v5 candidate does not load: %w", err)
	}
	return nil
}

// scrubClubLogAppKey removes a ClubLog entry's legacy `credentials.api` only
// when this build carries a nonblank injected key — persistResolvedConfig's own
// guard: a keyless build must not delete the only usable key. Every other
// credential stays.
func scrubClubLogAppKey(doc []byte) ([]byte, error) {
	if strings.TrimSpace(clublog.InjectedAPIKey) == "" {
		return doc, nil
	}
	var m map[string]any
	if err := json.Unmarshal(doc, &m); err != nil {
		return nil, fmt.Errorf("parse the v5 candidate: %w", err)
	}
	entries, _ := m["forwarders"].([]any)
	changed := false
	for _, e := range entries {
		f, _ := e.(map[string]any)
		if f["type"] != clublog.Type {
			continue
		}
		if creds, ok := f["credentials"].(map[string]any); ok {
			if _, has := creds["api"]; has {
				delete(creds, "api")
				changed = true
			}
		}
	}
	if !changed {
		return doc, nil
	}
	return json.MarshalIndent(m, "", "  ")
}

// writeOnce creates dst with the given content, owner-only, and never replaces
// an existing file: the content goes to a uniquely named staging file (so one
// an interrupted attempt left behind never blocks a retry), is synced, and is
// linked into place — a link fails when dst exists.
func writeOnce(dst string, content []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp") // created 0600
	if err != nil {
		return fmt.Errorf("stage the recovery copy: %w", err)
	}
	tmp := f.Name()
	_, werr := f.Write(content)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Link(tmp, dst)
	}
	_ = os.Remove(tmp)
	if werr != nil {
		if stderr.Is(werr, os.ErrExist) {
			return nil // another start won the race; its copy stands
		}
		return fmt.Errorf("write the recovery copy: %w", werr)
	}
	if dir, err := os.Open(filepath.Dir(dst)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// checkV5Compatible enforces the rules config v6 relaxed that a v5 (or older)
// loader still applies, so a document written for it — the recovery copy or a
// downgrade's result — is one it accepts: every forwarder entry carries a
// non-empty, unique name; and while evidence.sync is on, an smcloud entry is
// ENABLED with url and token (v5 tied sync to the enabled forwarder; v6 to the
// complete account, ruling R4). Nothing is changed to make it pass: consent
// and binding states are the operator's.
func checkV5Compatible(doc []byte) error {
	var shape struct {
		Evidence struct {
			Sync bool `json:"sync"`
		} `json:"evidence"`
		Forwarders []struct {
			Name        string          `json:"name"`
			Type        string          `json:"type"`
			Enabled     bool            `json:"enabled"`
			Credentials json.RawMessage `json:"credentials"`
		} `json:"forwarders"`
	}
	if err := json.Unmarshal(doc, &shape); err != nil {
		return fmt.Errorf("the v5 candidate does not parse: %w", err)
	}
	seen := map[string]bool{}
	smcloudOn := false
	for i, f := range shape.Forwarders {
		if f.Name == "" {
			return fmt.Errorf("forwarder[%d] (%s) has no name; v5 requires one", i, f.Type)
		}
		if seen[f.Name] {
			return fmt.Errorf("forwarder[%d]: duplicate name %q; v5 requires unique names", i, f.Name)
		}
		seen[f.Name] = true
		if f.Type == "smcloud" && f.Enabled && smcloudCredentialsComplete(f.Credentials) {
			smcloudOn = true
		}
	}
	if shape.Evidence.Sync && !smcloudOn {
		return fmt.Errorf("evidence.sync is on, but v5 accepts it only with the smcloud forwarder enabled (url and token set); " +
			"turn Home's SM Cloud destination on in Settings → Forwarding, or evidence sync off, and try again")
	}
	return nil
}

// smcloudCredentialsComplete reads SM Cloud's url and token exactly as v5's
// EvidenceSyncCredentials did — a tagged struct, so encoding/json's
// case-insensitive key matching applies (codex P2 on 25692f7a).
func smcloudCredentialsComplete(raw json.RawMessage) bool {
	var creds struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &creds) != nil {
		return false
	}
	return creds.URL != "" && creds.Token != ""
}
