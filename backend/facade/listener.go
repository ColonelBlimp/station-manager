package facade

import (
	"sync"

	"github.com/Station-Manager/enums/events"
	"github.com/Station-Manager/errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	// eventEmitBufferSize limits the number of pending status events to prevent goroutine leaks.
	// If the buffer is full, older events are dropped to avoid unbounded growth.
	eventEmitBufferSize = 16
)

// catStatusChannelListener listens to the CAT status updates channel and logs received updates or handles shutdown signals.
// It uses a bounded worker pool to prevent goroutine leaks when status updates arrive faster than they can be emitted.
func (s *Service) catStatusChannelListener(shutdown <-chan struct{}) {
	const op errors.Op = "facade.Service.catStatusChannelListener"

	statusChannel, err := s.CatService.StatusChannel()
	if err != nil {
		err = errors.New(op).Err(err).Msg("Failed to get cat status channel.")
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to start CAT status channel listener.")
		return
	}

	if statusChannel == nil {
		err = errors.New(op).Msg("CAT status channel is nil")
		s.LoggerService.ErrorWith().Err(err).Msg("Cannot start listener with nil channel")
		return
	}

	// Create a buffered channel for event emission to bound goroutine creation
	eventQueue := make(chan map[string]string, eventEmitBufferSize)

	// Start a single worker goroutine to emit events sequentially
	var emitWg sync.WaitGroup
	emitWg.Add(1)
	go func() {
		defer emitWg.Done()
		for {
			select {
			case <-shutdown:
				s.LoggerService.DebugWith().Msg("Event emitter received shutdown signal")
				return
			case <-s.ctx.Done():
				s.LoggerService.DebugWith().Msg("Event emitter context cancelled")
				return
			case statusCopy, ok := <-eventQueue:
				if !ok {
					return
				}
				// Emit the event to the frontend
				runtime.EventsEmit(s.ctx, events.Status.String(), statusCopy)
			}
		}
	}()

	// Main listener loop
	for {
		select {
		case <-shutdown:
			s.LoggerService.DebugWith().Msg("CAT status listener received shutdown signal")
			close(eventQueue)
			emitWg.Wait()
			return
		case <-s.ctx.Done():
			s.LoggerService.DebugWith().Msg("CAT status listener context cancelled")
			close(eventQueue)
			emitWg.Wait()
			return
		case status, ok := <-statusChannel:
			if !ok {
				s.LoggerService.InfoWith().Msg("CAT status channel closed, listener exiting")
				close(eventQueue)
				emitWg.Wait()
				return
			}
			// Non-blocking send to the event queue; drop if full to prevent blocking
			select {
			case eventQueue <- status:
				// Successfully queued
			default:
				// Queue is full, drop this update to prevent blocking
				s.LoggerService.DebugWith().Msg("Event queue full, dropping status update")
			}
		}
	}
}
