package cat

import "context"

func (s *Service) serialPortSender(shutdown <-chan struct{}) {
	for {
		select {
		case <-shutdown:
			return
		case cmd, ok := <-s.sendChannel:
			if !ok {
				return
			}
			if err := s.serialPort.WriteCommand(context.Background(), cmd.Cmd); err != nil {
				s.LoggerService.ErrorWith().Err(err).Msg("serial write failed")
			}
		}
	}
}
