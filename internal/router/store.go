package router

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RouteStore struct {
	rdb *redis.Client
}

func NewRouteStore(redisAddr, password string) *RouteStore {
	return &RouteStore{
		rdb: redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: password,
		}),
	}
}

func routeKey(userID string) string {
	return fmt.Sprintf("user:%s:capability-routes", userID)
}

func (s *RouteStore) GetRoutes(ctx context.Context, userID string) ([]Route, error) {
	val, err := s.rdb.Get(ctx, routeKey(userID)).Result()
	if err == redis.Nil {
		return []Route{}, nil
	}
	if err != nil {
		return nil, err
	}
	var routes []Route
	if err := json.Unmarshal([]byte(val), &routes); err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *RouteStore) SetRoutes(ctx context.Context, userID string, routes []Route) error {
	data, err := json.Marshal(routes)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, routeKey(userID), data, 0).Err()
}