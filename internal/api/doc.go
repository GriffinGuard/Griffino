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

// Package api hosts the Griffino HTTP API and its generated OpenAPI metadata.
//
// The general swagger annotations below describe the whole API surface; the
// per-route annotations live next to each handler. Regenerate the spec with
// `swag init -g internal/api/doc.go -o ./docs/api` from the repo root.
//
//	@title						Griffino API
//	@version					1.0
//	@description				Local-only control-plane API for the Griffino daemon. The API binds to 127.0.0.1:7070 and is consumed by the embedded web console. All non-public endpoints require a bearer session token obtained from POST /auth/login.
//	@BasePath					/api/v1
//	@host						127.0.0.1:7070
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Session token issued by POST /auth/login, sent as "Bearer <token>".
package api
