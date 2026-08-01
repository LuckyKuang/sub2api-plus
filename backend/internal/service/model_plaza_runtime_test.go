package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type modelPlazaRuntimeRepoStub struct {
	values map[string]string
	err    error
}

func (s *modelPlazaRuntimeRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (s *modelPlazaRuntimeRepoStub) GetValue(context.Context, string) (string, error) {
	return "", ErrSettingNotFound
}

func (s *modelPlazaRuntimeRepoStub) Set(context.Context, string, string) error { return nil }

func (s *modelPlazaRuntimeRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *modelPlazaRuntimeRepoStub) SetMultiple(context.Context, map[string]string) error { return nil }
func (s *modelPlazaRuntimeRepoStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}
func (s *modelPlazaRuntimeRepoStub) Delete(context.Context, string) error { return nil }

func TestGetModelPlazaRuntimeRequiresAuthenticationUnlessExplicitlyPublic(t *testing.T) {
	t.Run("missing require-auth setting defaults to sign-in", func(t *testing.T) {
		settings := NewSettingService(&modelPlazaRuntimeRepoStub{values: map[string]string{
			SettingKeyModelPlazaEnabled: "true",
		}}, nil)

		runtime := settings.GetModelPlazaRuntime(context.Background())
		require.True(t, runtime.Enabled)
		require.True(t, runtime.RequireAuth)
	})

	t.Run("administrator can explicitly enable anonymous showcase", func(t *testing.T) {
		settings := NewSettingService(&modelPlazaRuntimeRepoStub{values: map[string]string{
			SettingKeyModelPlazaEnabled:     "true",
			SettingKeyModelPlazaRequireAuth: "false",
		}}, nil)

		runtime := settings.GetModelPlazaRuntime(context.Background())
		require.True(t, runtime.Enabled)
		require.False(t, runtime.RequireAuth)
	})

	t.Run("setting store failure keeps the feature unavailable", func(t *testing.T) {
		settings := NewSettingService(&modelPlazaRuntimeRepoStub{err: errors.New("database unavailable")}, nil)

		runtime := settings.GetModelPlazaRuntime(context.Background())
		require.False(t, runtime.Enabled)
	})
}
