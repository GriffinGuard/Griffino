// Copyright 2025 GriffinGuard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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