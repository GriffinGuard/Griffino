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

package imagecheck

import "testing"

func TestApprovedImagesURL(t *testing.T) {
	t.Setenv("GRIFFINO_REGISTRY_BASE_URL", "")
	if got := approvedImagesURL(); got != defaultApprovedImagesURL {
		t.Fatalf("default = %q, want %q", got, defaultApprovedImagesURL)
	}
	t.Setenv("GRIFFINO_REGISTRY_BASE_URL", "http://localhost:9999/reg/")
	if got := approvedImagesURL(); got != "http://localhost:9999/reg/approved-images.json" {
		t.Fatalf("override = %q", got)
	}
}
