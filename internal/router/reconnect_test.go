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
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestMinDuration(t *testing.T) {
	if got := minDuration(time.Second, 30*time.Second); got != time.Second {
		t.Errorf("minDuration(1s,30s) = %v, want 1s", got)
	}
	if got := minDuration(60*time.Second, 30*time.Second); got != 30*time.Second {
		t.Errorf("minDuration(60s,30s) = %v, want 30s", got)
	}
}

// TestPublishPathsNilChannelSafe verifies the publish paths do not panic and fail
// gracefully before Start (or during a reconnect) when no channel is available.
func TestPublishPathsNilChannelSafe(t *testing.T) {
	r := New("localhost:6379", "", nil)

	if r.invokeCh() != nil {
		t.Fatal("invokeCh() should be nil before Start")
	}
	if err := r.PublishAction("action.p.u.a.v1", []byte("{}")); err == nil {
		t.Error("PublishAction should error when no amqp channel is available")
	}
	// replyError must be a no-op (no panic) when there is no channel.
	r.replyError(amqp.Delivery{ReplyTo: "reply-q"}, "boom")
}
