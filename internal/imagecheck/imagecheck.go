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

// Package imagecheck verifies that Docker images used by plugins are either
// published by GriffinGuard or present in the community-approved whitelist.
package imagecheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

const (
	defaultApprovedImagesURL = "https://raw.githubusercontent.com/GriffinGuard/Griffino-Plugins/main/approved-images.json"
	griffinguardPrefix       = "ghcr.io/griffinguard/"
)

// approvedImagesURL returns the URL of the community-approved image whitelist.
// Like the registry, it defaults to the official repo; when GRIFFINO_REGISTRY_BASE_URL
// is set it is derived from the same base URL (at your own risk, for local dev / private
// deployment only — locked to the official default when unset).
// 返回社区批准镜像白名单地址；默认官方源，设置 GRIFFINO_REGISTRY_BASE_URL 时从同一基址派生（自担风险，仅本地/私有部署）。
func approvedImagesURL() string {
	if v := strings.TrimRight(os.Getenv("GRIFFINO_REGISTRY_BASE_URL"), "/"); v != "" {
		return v + "/approved-images.json"
	}
	return defaultApprovedImagesURL
}

type approvedEntry struct {
	Image      string `json:"image"`
	ApprovedAt string `json:"approvedAt"`
	ApprovedBy string `json:"approvedBy"`
	Notes      string `json:"notes"`
}

type approvedList struct {
	ApprovedImages []approvedEntry `json:"approvedImages"`
}

// UnapprovedError is returned when one or more images are not whitelisted.
type UnapprovedError struct {
	Images []string
}

func (e *UnapprovedError) Error() string {
	return griffinoi18n.T(griffinoi18n.ErrImageCheckUnapproved,
		map[string]interface{}{"Images": strings.Join(e.Images, ", ")})
}

// CheckBootSpec checks all auxiliary service images in the BootSpec against
// the approved-images whitelist. The main service image (mainServiceID) is
// unconditionally allowed—it is verified by the CI/CD pipeline. Images with
// the ghcr.io/griffinguard/ prefix are also unconditionally allowed.
func CheckBootSpec(spec *manifest.BootSpec) error {
	toCheck := make([]string, 0, len(spec.Services))
	for svcID, svc := range spec.Services {
		if svc.Image == "" || svcID == spec.MainServiceID {
			// main service image is guaranteed by CI/CD; not checked at install time
			// 主服务镜像由 CI/CD 保证，安装时不检查
			continue
		}
		// every sub-service image goes through the whitelist check, including
		// ghcr.io/griffinguard/ -prefixed ones / 子服务镜像一律走白名单，含 ghcr.io/griffinguard/ 前缀
		toCheck = append(toCheck, svc.Image)
	}
	if len(toCheck) == 0 {
		return nil
	}

	approved, err := fetchApprovedImages()
	if err != nil {
		return fmt.Errorf("%s", griffinoi18n.T(griffinoi18n.ErrImageCheckFetchFailed,
			map[string]interface{}{"detail": err.Error()}))
	}

	approvedSet := make(map[string]struct{}, len(approved))
	for _, e := range approved {
		approvedSet[e.Image] = struct{}{}
	}

	var unapproved []string
	for _, img := range toCheck {
		if _, ok := approvedSet[img]; !ok {
			unapproved = append(unapproved, img)
		}
	}
	if len(unapproved) > 0 {
		return &UnapprovedError{Images: unapproved}
	}
	return nil
}

// IsAllowedToPull reports whether Griffino may auto-pull an image.
//
// Rules:
//   - main service image: must have the ghcr.io/griffinguard/ prefix (vetted by CI/CD)
//   - sub-service image: must be in the community whitelist
//
// A dev plugin's local image should already exist via `docker build`; callers skip
// this path when the image is present locally.
// 判断镜像是否允许被自动拉取：主服务镜像须为 ghcr.io/griffinguard/ 前缀，子服务镜像须在社区白名单中。
func IsAllowedToPull(imageName, mainServiceImage string) (bool, error) {
	if imageName == mainServiceImage {
		// main service: only the official griffinguard prefix is allowed / 主服务仅允许官方前缀
		return strings.HasPrefix(imageName, griffinguardPrefix), nil
	}

	// sub-service: check the whitelist; the griffinguard prefix is not auto-allowed here
	// 子服务：查白名单，griffinguard 前缀在此不直接放行
	approved, err := fetchApprovedImages()
	if err != nil {
		return false, fmt.Errorf("%s", griffinoi18n.T(griffinoi18n.ErrImageCheckFetchFailed,
			map[string]interface{}{"Error": err.Error()}))
	}
	for _, e := range approved {
		if e.Image == imageName {
			return true, nil
		}
	}
	return false, nil
}

func fetchApprovedImages() ([]approvedEntry, error) {
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(approvedImagesURL())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result approvedList
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%s", griffinoi18n.T(griffinoi18n.ErrImageCheckParseFailed,
			map[string]interface{}{"Error": err.Error()}))
	}
	return result.ApprovedImages, nil
}
