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

package broker

import "errors"

var (
	ErrGeneratePassword    = errors.New("failed to generate rabbitmq password")
	ErrCreateUser          = errors.New("failed to create rabbitmq user")
	ErrSetPermissions      = errors.New("failed to set rabbitmq permissions")
	ErrDeclareQueue        = errors.New("failed to declare rabbitmq queue")
	ErrBindQueue           = errors.New("failed to bind rabbitmq queue")
	ErrDeleteUser          = errors.New("failed to delete rabbitmq user")
	ErrSetUserPassword     = errors.New("failed to set rabbitmq user password")
)