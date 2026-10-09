package archive

import (
	"context"
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AdoptionSubject is what an adoption status describes (ADR 0091, "status
// belongs to its subject"): the active archive, the default logbook's binding,
// the name it uploads under and the station account's fingerprint. A status is
// shown only while every one of them is still current.
type AdoptionSubject struct {
	ArchiveID     string
	ForwarderName string
	LogbookID     int64
	Name          string
	Account       string
}

// AdoptionStatus is the adopter's latest status and the subject it describes.
type AdoptionStatus struct {
	Subject AdoptionSubject
	State   string
	Message string
}

// The adoption states the bindings view derives from the archive itself.
const (
	AdoptionStateAdopted             = "adopted"
	AdoptionStateAdoptedRestart      = "adopted_restart_required"
	AdoptionStateNeedsConfirmation   = "needs_confirmation"
	adoptionMessageAdopted           = "Adopted."
	adoptionMessageAdoptedRestart    = "Adopted; applies after a restart."
	adoptionMessageNeedsConfirmation = "Adoption needs confirmation for the current station account."
)

// SubjectOf is the subject an attempt on b acts for under cfg. An error means
// b has none: no adoption name, or no complete station account.
func SubjectOf(cfg config.Config, b types.LogbookDestination) (AdoptionSubject, error) {
	normalize, ok := forwarding.AdoptionNameFor(b.Destination)
	if !ok {
		return AdoptionSubject{}, fmt.Errorf("destination %q has no remote adoption", b.Destination)
	}
	name, err := normalize(b.Credentials)
	if err != nil {
		return AdoptionSubject{}, err
	}
	account, err := CurrentAccount(cfg, b.Destination, cfg.ActiveQsoArchiveID)
	if err != nil {
		return AdoptionSubject{}, err
	}
	return AdoptionSubject{ArchiveID: cfg.ActiveQsoArchiveID, ForwarderName: b.ForwarderName, LogbookID: b.LogbookID, Name: name, Account: account}, nil
}

// The held-uploads states (ruling C2; disabled from the operator's review of
// 5a: the start discards a disabled binding's queued uploads, ADR 0039).
const (
	UploadsHeldWaiting         = "waiting_for_confirmation"
	UploadsHeldRestart         = "restart_required"
	UploadsHeldDisabled        = "disabled"
	uploadsHeldWaitingMessage  = "Uploads are held until adoption is confirmed for the current station account."
	uploadsHeldRestartMessage  = "Uploads resume after a restart."
	uploadsHeldDisabledMessage = "Uploads are held; this binding is off, so its queued uploads are discarded at the next restart."
)

// SetHeldUploads names the bindings this generation started held (cmd/smd,
// from the same start snapshot that chose their wire).
func (m *Manager) SetHeldUploads(names []string) {
	held := make(map[string]struct{}, len(names))
	for _, n := range names {
		held[n] = struct{}{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heldUploads = held
}

// SetAdoptionStatus wires the adopter's status (cmd/smd); nil until then.
func (m *Manager) SetAdoptionStatus(status func() AdoptionStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adoptionStatus = status
}

// annotateAdoption sets each held binding's uploads_held line (ruling C2), and
// the adoption status on Home's default SM Cloud row
// (ADR 0090 T4, ADR 0091). A binding confirmed under the current account is
// adopted, pending a restart while the running generation started without
// that confirmation. Otherwise, on an enabled binding, the adopter's status is
// shown only when its subject is this binding's current one; a binding adopted
// under another account with no such status needs confirmation.
func (m *Manager) annotateAdoption(ctx context.Context, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, view *types.ArchiveBindingsView) error {
	if entry == nil || !adoptedArchive(entry) {
		return nil
	}
	m.mu.Lock()
	status, atStart, held := m.adoptionStatus, m.startBindings, m.heldUploads
	m.mu.Unlock()
	bindings, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if _, ok := held[b.ForwarderName]; ok {
			setRowUploadsHeld(view, b, uploadsHeldOf(cfg, b))
		}
		if b.LogbookID != cfg.DefaultLogbookID {
			continue
		}
		sub, err := SubjectOf(cfg, b)
		if err != nil {
			continue
		}
		if a := adoptionOf(b, sub, status, atStart[b.ForwarderName]); a != nil {
			setRowAdoption(view, b, a)
		}
	}
	return nil
}

func adoptionOf(b types.LogbookDestination, sub AdoptionSubject, status func() AdoptionStatus, started types.LogbookDestination) *types.BindingAdoptionView {
	switch {
	case AdoptionConfirmed(b, sub.Account) && AdoptionConfirmed(started, sub.Account):
		return &types.BindingAdoptionView{State: AdoptionStateAdopted, Message: adoptionMessageAdopted}
	case AdoptionConfirmed(b, sub.Account):
		return &types.BindingAdoptionView{State: AdoptionStateAdoptedRestart, Message: adoptionMessageAdoptedRestart}
	case !b.Enabled:
		return nil
	}
	if status != nil {
		if s := status(); s.Subject == sub && s.State != "" {
			return &types.BindingAdoptionView{State: s.State, Message: s.Message}
		}
	}
	if b.RemoteAdoptedAt != nil {
		return &types.BindingAdoptionView{State: AdoptionStateNeedsConfirmation, Message: adoptionMessageNeedsConfirmation}
	}
	return nil
}

// uploadsHeldOf is a held binding's line: waiting until a confirmation for the
// current account is recorded, then restart required; once disabled and
// saved, its queued uploads are discarded at the next start instead.
func uploadsHeldOf(cfg config.Config, b types.LogbookDestination) *types.BindingAdoptionView {
	if !b.Enabled {
		return &types.BindingAdoptionView{State: UploadsHeldDisabled, Message: uploadsHeldDisabledMessage}
	}
	account, err := CurrentAccount(cfg, b.Destination, cfg.ActiveQsoArchiveID)
	if err == nil && AdoptionConfirmed(b, account) {
		return &types.BindingAdoptionView{State: UploadsHeldRestart, Message: uploadsHeldRestartMessage}
	}
	return &types.BindingAdoptionView{State: UploadsHeldWaiting, Message: uploadsHeldWaitingMessage}
}

func setRowUploadsHeld(view *types.ArchiveBindingsView, b types.LogbookDestination, h *types.BindingAdoptionView) {
	for i := range view.Destinations {
		if view.Destinations[i].Type != b.Destination {
			continue
		}
		for j := range view.Destinations[i].Logbooks {
			if row := &view.Destinations[i].Logbooks[j]; row.ForwarderName == b.ForwarderName {
				row.UploadsHeld = h
			}
		}
	}
}

func setRowAdoption(view *types.ArchiveBindingsView, b types.LogbookDestination, a *types.BindingAdoptionView) {
	for i := range view.Destinations {
		if view.Destinations[i].Type != b.Destination {
			continue
		}
		for j := range view.Destinations[i].Logbooks {
			if row := &view.Destinations[i].Logbooks[j]; row.ForwarderName == b.ForwarderName {
				row.Adoption = a
			}
		}
	}
}
