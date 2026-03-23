package cat

import (
	"github.com/Station-Manager/types"
)

func (s *Service) lineProcessor(shutdown <-chan struct{}) {
	for {
		select {
		case <-shutdown:
			return
		case state := <-s.processingChannel:
			if len(state.Markers) == 0 {
				s.LoggerService.ErrorWith().Str("line", state.Data).Msg("Bad catState configuration; no markers defined. Skipping line.")
				continue
			}

			status := types.CatStatus{}

			for _, marker := range state.Markers {
				start := marker.Index
				if start < 0 || start >= len(state.Data) {
					s.LoggerService.WarnWith().Int("index", start).Msg("marker index out of range; skipping marker")
					continue
				}

				end := marker.Index + marker.Length
				if end > len(state.Data) {
					s.LoggerService.WarnWith().Int("index", start).Int("length", marker.Length).Msg("marker end out of range; clamping to line end")
					end = len(state.Data)
				}
				if start >= end {
					s.LoggerService.DebugWith().Int("index", start).Int("length", marker.Length).Msg("empty slice for marker; skipping")
					continue
				}

				slice := state.Data[start:end]

				if len(marker.ValueMappings) == 0 {
					status[marker.Tag] = slice
				} else {
					mapped := ""
					for _, vm := range marker.ValueMappings {
						if slice == vm.Key {
							mapped = vm.Value
							break
						}
					}
					status[marker.Tag] = mapped // empty string if no mapping matched
				}
			}

			if !s.sendStatusWithEviction(status, shutdown) {
				return // Shutdown signaled
			}
		}
	}
}

// sendStatusWithEviction attempts to send a status update to the status channel.
// If the channel is full, it evicts the oldest status and retries.
// For unbuffered channels, it drops the status with a warning.
// Returns true if sent successfully, false if shutdown was signaled.
func (s *Service) sendStatusWithEviction(status types.CatStatus, shutdown <-chan struct{}) bool {
	for {
		select {
		case <-shutdown:
			return false
		case s.statusChannel <- status:
			return true
		default:
			if !s.tryEvictOldestStatus(shutdown) {
				return false
			}
			// Successfully evicted, loop will retry send
		}
	}
}

// tryEvictOldestStatus attempts to remove one item from the status channel to make room.
// Returns false if the channel is unbuffered or shutdown is signaled, true otherwise.
func (s *Service) tryEvictOldestStatus(shutdown <-chan struct{}) bool {
	if cap(s.statusChannel) == 0 {
		s.LoggerService.WarnWith().Msg("No consumer on unbuffered status channel, dropping status.")
		return false
	}

	select {
	case <-shutdown:
		return false
	case <-s.statusChannel:
		s.LoggerService.DebugWith().Msg("Evicted oldest status from full channel")
		return true
	default:
		// Channel became empty between checks (race condition)
		// Return true to retry send - the channel likely has space now
		return true
	}
}
