package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/setting"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

const clientDisconnectRiskSettingsLockKey = "client_disconnect_risk_settings"

func (r *settingRepository) GetClientDisconnectRiskSettings(ctx context.Context) (bool, int64, error) {
	values, err := r.GetMultiple(ctx, []string{
		service.SettingKeyClientDisconnectConsecutiveBanEnabled,
		service.SettingKeyClientDisconnectConsecutiveBanGeneration,
	})
	if err != nil {
		return false, 0, err
	}
	enabled := !strings.EqualFold(strings.TrimSpace(values[service.SettingKeyClientDisconnectConsecutiveBanEnabled]), "false")
	generation := int64(1)
	if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(values[service.SettingKeyClientDisconnectConsecutiveBanGeneration]), 10, 64); parseErr == nil && parsed > 0 {
		generation = parsed
	}
	return enabled, generation, nil
}

func (r *settingRepository) SetMultipleWithClientDisconnectRiskGeneration(
	ctx context.Context,
	values map[string]string,
) (int64, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	txClient := tx.Client()
	if txClient.Driver().Dialect() == dialect.Postgres {
		var rows entsql.Rows
		if err = txClient.Driver().Query(
			ctx,
			"SELECT pg_advisory_xact_lock(hashtext($1))",
			[]any{clientDisconnectRiskSettingsLockKey},
			&rows,
		); err != nil {
			return 0, err
		}
		_ = rows.Close()
	}

	current, err := tx.Setting.Query().Where(setting.KeyIn(
		service.SettingKeyClientDisconnectConsecutiveBanEnabled,
		service.SettingKeyClientDisconnectConsecutiveBanGeneration,
	)).All(ctx)
	if err != nil {
		return 0, err
	}
	currentEnabled := true
	generation := int64(1)
	for _, item := range current {
		switch item.Key {
		case service.SettingKeyClientDisconnectConsecutiveBanEnabled:
			currentEnabled = !strings.EqualFold(strings.TrimSpace(item.Value), "false")
		case service.SettingKeyClientDisconnectConsecutiveBanGeneration:
			if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(item.Value), 10, 64); parseErr == nil && parsed > 0 {
				generation = parsed
			}
		}
	}
	requestedEnabled := !strings.EqualFold(
		strings.TrimSpace(values[service.SettingKeyClientDisconnectConsecutiveBanEnabled]),
		"false",
	)
	if requestedEnabled != currentEnabled {
		generation++
	}

	persisted := make(map[string]string, len(values)+1)
	for key, value := range values {
		persisted[key] = value
	}
	persisted[service.SettingKeyClientDisconnectConsecutiveBanGeneration] = strconv.FormatInt(generation, 10)
	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(persisted))
	for key, value := range persisted {
		builders = append(builders, tx.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	if err = tx.Setting.CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return generation, nil
}

type settingRepository struct {
	client *ent.Client
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	return &settingRepository{client: client}
}

func (r *settingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	m, err := r.client.Setting.Query().Where(setting.KeyEQ(key)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, service.ErrSettingNotFound
		}
		return nil, err
	}
	return &service.Setting{
		ID:        m.ID,
		Key:       m.Key,
		Value:     m.Value,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (r *settingRepository) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *settingRepository) Set(ctx context.Context, key, value string) error {
	now := time.Now()
	return r.client.Setting.
		Create().
		SetKey(key).
		SetValue(value).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := r.client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}

	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(settings))
	for key, value := range settings {
		builders = append(builders, r.client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	return r.client.Setting.
		CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) Delete(ctx context.Context, key string) error {
	_, err := r.client.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
	return err
}
