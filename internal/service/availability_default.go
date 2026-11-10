package service

import (
	"context"
	"github.com/astalum/vacaro-back/internal/domain"
)

func (s *Service) SetAvailabilityDefault(ctx context.Context, userID, value string) (domain.User, error) {
	if value != "available" && value != "unavailable" {
		return domain.User{}, &ValidationError{Fields: map[string]string{"availability_default": "空き時間の初期状態を選択してください"}}
	}
	if _, err := s.GetUser(ctx, userID); err != nil {
		return domain.User{}, err
	}
	return s.store.SetAvailabilityDefault(ctx, userID, value)
}
