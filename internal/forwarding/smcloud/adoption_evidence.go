package smcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AdoptionEvidence is what Home's adoption is judged on (ADR 0090, T3). The
// caller gathers it; a read that failed or came back incomplete is not
// evidence, so the caller retries rather than judging.
type AdoptionEvidence struct {
	// DefaultLogbookID is Home's default logbook, the one adoption maps.
	DefaultLogbookID int64
	// Bindings is every binding of Home's live logbooks, enabled or not; only
	// SM Cloud's are read.
	Bindings []types.LogbookDestination
	// Running is the routing snapshot the daemon built its workers from at
	// start. A worker uploads under the cloud name its binding had then, and a
	// logbook deleted since keeps its worker, which drains its queue, until
	// the restart: these routes are judged beside the saved bindings.
	Running []types.LogbookDestination
	// Queued is the upload queue by forwarder name.
	Queued map[string]sqlite.ForwarderQueueCounts
	// LocalUUIDs is every QSO of the default logbook, soft-deleted included.
	LocalUUIDs []string
	// CloudUUIDs is every QSO the cloud holds under the default binding's
	// cloud name, tombstones included; empty when the name was never pushed.
	CloudUUIDs []string
}

// AdoptionVerdict is the judgement: Safe, or the first reason it is not, for
// the log. CloudName is the default binding's normalized cloud name.
type AdoptionVerdict struct {
	Safe      bool
	CloudName string
	Reason    string
}

// JudgeAdoption decides whether Home's default SM Cloud binding may adopt its
// legacy cloud logbook (ADR 0090, T3). Every other SM Cloud binding is judged
// twice: as the running worker uploads (Running, its name at start) and as
// the next start will (Bindings, its saved name). All of these must hold:
//   - (a) no other binding, enabled or disabled, running or saved, has the
//     same normalized cloud name, and every such name can be read;
//   - (b) no other binding with that name has uploads queued (waiting, in
//     flight or failed — a failed upload can be retried); reported in
//     preference to (a) as the more specific reason;
//   - (c) every cloud UUID under the name, tombstones included, is a local QSO
//     of the default logbook, soft-deleted included.
//
// Cloud-only UUIDs refuse too: they prove nothing either way. Queue rows of a
// forwarder in neither list are inert: no worker sends them now or after the
// restart. An error means there is nothing to judge: no saved default SM
// Cloud binding, or one whose name cannot be read.
func JudgeAdoption(e AdoptionEvidence) (AdoptionVerdict, error) {
	const op errors.Op = "smcloud.JudgeAdoption"
	var def *types.LogbookDestination
	for i := range e.Bindings {
		if e.Bindings[i].Destination == Type && e.Bindings[i].LogbookID == e.DefaultLogbookID {
			def = &e.Bindings[i]
		}
	}
	if def == nil {
		return AdoptionVerdict{}, errors.New(op).WithMsgf("logbook %d has no SM Cloud binding", e.DefaultLogbookID)
	}
	name, err := CloudName(def.Credentials)
	if err != nil {
		return AdoptionVerdict{}, errors.New(op).WithErr(err).WithMsgf("the default binding %s", def.ForwarderName)
	}
	v := AdoptionVerdict{CloudName: name}
	if v.Reason = sharedName(e.Running, "as started", def.ForwarderName, name, e.Queued); v.Reason != "" {
		return v, nil
	}
	if v.Reason = sharedName(e.Bindings, "as saved", def.ForwarderName, name, e.Queued); v.Reason != "" {
		return v, nil
	}
	local := make(map[string]struct{}, len(e.LocalUUIDs))
	for _, u := range e.LocalUUIDs {
		local[strings.ToLower(strings.TrimSpace(u))] = struct{}{}
	}
	var foreign []string
	for _, u := range e.CloudUUIDs {
		if _, ok := local[strings.ToLower(strings.TrimSpace(u))]; !ok {
			foreign = append(foreign, u)
		}
	}
	if n := int64(len(foreign)); n > 0 {
		verb, noun := "is", "a QSO"
		if n > 1 {
			verb, noun = "are", "QSOs"
		}
		v.Reason = fmt.Sprintf("%d QSO%s in the cloud logbook %q %s not %s of the default logbook (first: %s)",
			n, plural(n), name, verb, noun, foreign[0])
		return v, nil
	}
	v.Safe = true
	return v, nil
}

// sharedName is the first reason, in one list of bindings, that another SM
// Cloud binding may upload under name: (b) its queued uploads, (a) the name
// itself, or a name that cannot be read. "" when none.
func sharedName(bindings []types.LogbookDestination, as, defaultForwarder, name string, queued map[string]sqlite.ForwarderQueueCounts) string {
	for _, b := range bindings {
		if b.Destination != Type || b.ForwarderName == defaultForwarder {
			continue
		}
		other, err := CloudName(b.Credentials)
		if err != nil {
			return fmt.Sprintf("binding %s: its cloud name cannot be read (%s), so it may share %q", b.ForwarderName, as, name)
		}
		if other != name {
			continue
		}
		c := queued[b.ForwarderName]
		if n := c.Waiting + c.InFlight + c.Failed; n > 0 {
			return fmt.Sprintf("binding %s has %d queued upload%s under the cloud name %q (%s)", b.ForwarderName, n, plural(n), name, as)
		}
		return fmt.Sprintf("binding %s shares the cloud name %q (%s)", b.ForwarderName, name, as)
	}
	return ""
}

// CloudName is the cloud logbook name a binding's credentials upload under,
// normalized exactly as New does: TrimSpace, blank meaning DefaultLogbook.
// Absent or null credentials are the default; a blob that is not an object
// with a string "logbook" is an error.
func CloudName(credentials json.RawMessage) (string, error) {
	var c struct {
		Logbook *string `json:"logbook"`
	}
	if raw := bytes.TrimSpace(credentials); len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return "", fmt.Errorf("the cloud name cannot be read: %w", err)
		}
	}
	if c.Logbook == nil || strings.TrimSpace(*c.Logbook) == "" {
		return DefaultLogbook, nil
	}
	return strings.TrimSpace(*c.Logbook), nil
}

func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// LocalAdoptionEvidence reads the local half of the evidence from the open
// archive: the saved bindings, the upload queue, and every QSO UUID of the
// default logbook, soft-deleted included. running is the routing snapshot the
// daemon started its workers from. The caller adds CloudUUIDs. Any failed read
// is an error, never partial evidence.
func LocalAdoptionEvidence(ctx context.Context, db *sqlite.Service, defaultLogbookID int64, running []types.LogbookDestination) (AdoptionEvidence, error) {
	const op errors.Op = "smcloud.LocalAdoptionEvidence"
	bindings, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return AdoptionEvidence{}, errors.New(op).WithErr(err).WithMsg("list bindings")
	}
	queued, err := db.ForwarderQueueCountsWithContext(ctx)
	if err != nil {
		return AdoptionEvidence{}, errors.New(op).WithErr(err).WithMsg("count the upload queue")
	}
	manifest, err := db.FetchQsoManifestWithContext(ctx, defaultLogbookID)
	if err != nil {
		return AdoptionEvidence{}, errors.New(op).WithErr(err).WithMsg("list the default logbook's QSOs")
	}
	local := make([]string, 0, len(manifest))
	for _, m := range manifest {
		local = append(local, m.UUID)
	}
	return AdoptionEvidence{
		DefaultLogbookID: defaultLogbookID,
		Bindings:         bindings,
		Running:          running,
		Queued:           queued,
		LocalUUIDs:       local,
	}, nil
}
