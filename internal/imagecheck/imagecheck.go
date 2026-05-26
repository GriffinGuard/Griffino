// Package imagecheck verifies that Docker images used by plugins are either
// published by GriffinGuard or present in the community-approved whitelist.
package imagecheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

const (
	approvedImagesURL  = "https://raw.githubusercontent.com/GriffinGuard/Griffino-Plugins/main/approved-images.json"
	griffinguardPrefix = "ghcr.io/griffinguard/"
)

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
			// mainService 镜像由 CI/CD 保证，安装时不检查
			continue
		}
		// sub service 镜像一律进白名单检查
		// 包括 ghcr.io/griffinguard/ 前缀的镜像也不例外
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


// IsAllowedToPull 判断一个镜像是否允许被 Griffino 自动拉取。
//
// 规则：
//   - mainService 镜像：必须是 ghcr.io/griffinguard/ 前缀（由 CI/CD 审核保证）
//   - sub service 镜像：必须在社区白名单中
//
// Dev 插件的本地镜像应已通过 docker build 存在，调用方在本地存在时不会走到这里。
func IsAllowedToPull(imageName, mainServiceImage string) (bool, error) {
	if imageName == mainServiceImage {
		// mainService：只允许 griffinguard 官方前缀
		return strings.HasPrefix(imageName, griffinguardPrefix), nil
	}

	// sub service：查白名单，不接受 griffinguard 前缀的直接放行
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
	resp, err := c.Get(approvedImagesURL)
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