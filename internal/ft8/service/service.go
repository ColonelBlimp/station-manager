package service

import (
	"context"
)

type Service struct {
}

func (s *Service) Initialize() error {
	return nil
}

func (s *Service) Start(ctx context.Context) error {
	return nil
}

func (s *Service) Stop() error {
	return nil
}
