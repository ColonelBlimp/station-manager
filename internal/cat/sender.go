package cat

import "context"

func (s *Service) serialPortSender(shutdown <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		select {
		case <-shutdown:
			return
		case cmd, ok := <-s.sendChannel:
			if !ok {
				return
			}
			if err := s.serialPort.WriteCommand(ctx, cmd.Cmd); err != nil {
				s.LoggerService.ErrorWith().Err(err).Msg("serial write failed")
			}
		}
	}
}
