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

package system

import "fmt"

// ContainerConflictError indicates a container name conflicts with a non-Griffino container / 表示容器名与非 Griffino 容器冲突
type ContainerConflictError struct {
	Name string
	ID   string
}

func (e *ContainerConflictError) Error() string {
	return fmt.Sprintf("container name %s is already in use by a non-Griffino container (ID: %s)", e.Name, e.ID)
}
