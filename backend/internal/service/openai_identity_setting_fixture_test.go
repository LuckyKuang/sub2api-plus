//go:build unit || !integration

package service

import "context"

// Shared by native transport and identity tests in both default and unit builds.
type openAIIdentitySettingRepoStub struct {
	values map[string]string
}

func (s *openAIIdentitySettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (s *openAIIdentitySettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *openAIIdentitySettingRepoStub) Set(context.Context, string, string) error { return nil }

func (s *openAIIdentitySettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (s *openAIIdentitySettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *openAIIdentitySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *openAIIdentitySettingRepoStub) Delete(context.Context, string) error { return nil }
