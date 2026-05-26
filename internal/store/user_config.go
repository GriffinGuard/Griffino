package store

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/redis/go-redis/v9"
)

type UserConfigStore struct {
    rdb  *redis.Client
}

func NewUserConfigStore(addr, password string) *UserConfigStore {
	return &UserConfigStore{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
		}),
	}
}

func userConfigKey(userID, pluginID string) string {
    return fmt.Sprintf("user:%s:plugin:%s:config", userID, pluginID)
}

func (s *UserConfigStore) Get(ctx context.Context, userID, pluginID string) (map[string]string, error) {
    val, err := s.rdb.Get(ctx, userConfigKey(userID, pluginID)).Result()
    if err == redis.Nil {
        return map[string]string{}, nil
    }
    if err != nil {
        return nil, err
    }
    var result map[string]string
    if err := json.Unmarshal([]byte(val), &result); err != nil {
        return nil, err
    }
    return result, nil
}

func (s *UserConfigStore) Set(ctx context.Context, userID, pluginID string, values map[string]string) error {
    data, err := json.Marshal(values)
    if err != nil {
        return err
    }
    return s.rdb.Set(ctx, userConfigKey(userID, pluginID), data, 0).Err()
}