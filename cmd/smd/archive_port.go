package main

import (
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// The archive activation's TX admission ports (ADR 0071, W-0021 slice 3):
// internal/archive imports neither subsystem, so the daemon adapts each seal
// here at the assembly boundary.

// ft8Seal seals FT8 admission: arm, manual send, session start and profile
// claim refuse until the restart (ft8.Service.SealTxAdmission).
type ft8Seal struct{ s *ft8.Service }

func (a ft8Seal) Seal() error { return a.s.SealTxAdmission() }
func (a ft8Seal) Release()    { a.s.ReleaseTxAdmissionSeal() }

// bridgeSeal seals the rig's keyed paths: tune and FT8 keying refuse until the
// restart (bridge.Service.SealTx).
type bridgeSeal struct{ s *bridge.Service }

func (a bridgeSeal) Seal() error { return a.s.SealTx() }
func (a bridgeSeal) Release()    { a.s.ReleaseTxSeal() }
