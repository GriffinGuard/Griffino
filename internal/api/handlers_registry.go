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

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/GriffinGuard/Griffino/internal/imagecheck"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

const (
	registryURL = "https://raw.githubusercontent.com/GriffinGuard/Griffino-Plugins/main/registry.json"
	rawBaseURL  = "https://raw.githubusercontent.com/GriffinGuard/Griffino-Plugins/main"
)

type RegistryVersion struct {
	Version      string `json:"version"`
	ManifestPath string `json:"manifestPath"`
	PublishedAt  string `json:"publishedAt"`
	Changelog    string `json:"changelog"`
}

type RegistryPlugin struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	LatestVersion string            `json:"latestVersion"`
	APIVersion    string            `json:"apiVersion"`
	Author        string            `json:"author"`
	License       string            `json:"license"`
	Tags          []string          `json:"tags"`
	Verified      bool              `json:"verified"`
	Versions      []RegistryVersion `json:"versions"`
}

type Registry struct {
	RegistryVersion string           `json:"registryVersion"`
	UpdatedAt       string           `json:"updatedAt"`
	Plugins         []RegistryPlugin `json:"plugins"`
}

// GET /api/v1/registry/plugins
func (s *Server) handleListRegistryPlugins(w http.ResponseWriter, r *http.Request) {
	registry, err := fetchRegistry()
	if err != nil {
		writeAppError(w, http.StatusBadGateway, ErrRegistryFetchFailed, "Failed to fetch plugin registry",
    		map[string]interface{}{"detail": err.Error()})
		return
	}

	type PluginWithStatus struct {
		RegistryPlugin
		Installed        bool   `json:"installed"`
		InstalledVersion string `json:"installedVersion,omitempty"`
	}

	result := make([]PluginWithStatus, 0, len(registry.Plugins))
	for _, p := range registry.Plugins {
		item := PluginWithStatus{RegistryPlugin: p}
		if inst, _ := s.st.GetPlugin(p.ID); inst != nil {
			item.Installed = true
			item.InstalledVersion = filepath.Base(inst.PluginDir)
		}
		result = append(result, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updatedAt": registry.UpdatedAt,
		"plugins":   result,
	})
}

// POST /api/v1/registry/plugins/{id}/install
func (s *Server) handleInstallRegistryPlugin(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	registry, err := fetchRegistry()
	if err != nil {
		writeAppError(w, http.StatusBadGateway, ErrRegistryFetchFailed, "Failed to fetch plugin registry",
    		map[string]interface{}{"detail": err.Error()})
		return
	}

	var target *RegistryPlugin
	for i := range registry.Plugins {
		if registry.Plugins[i].ID == pluginID {
			target = &registry.Plugins[i]
			break
		}
	}
	if target == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found in registry",
    		map[string]interface{}{"id": pluginID})
		return
	}

	if inst, _ := s.st.GetPlugin(pluginID); inst != nil {
		writeAppError(w, http.StatusConflict, ErrPluginAlreadyInstalled, "Plugin already installed",
    		map[string]interface{}{"id": pluginID})
		return
	}

	version := target.LatestVersion
	remoteDir := fmt.Sprintf("plugins/%s/%s", pluginID, version)

	homeDir, _ := os.UserHomeDir()
	localDir := filepath.Join(homeDir, ".griffino", "plugins", pluginID, version)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to create plugin directory",
    		map[string]interface{}{"detail": err.Error()})
		return
	}

	// 必须下载的核心文件
	for _, f := range []string{"plugin.manifest.json", "plugin.boot.yml", "config.boot.json"} {
		url := fmt.Sprintf("%s/%s/%s", rawBaseURL, remoteDir, f)
		if err := downloadFile(url, filepath.Join(localDir, f)); err != nil {
			os.RemoveAll(localDir)
			writeAppError(w, http.StatusBadGateway, ErrRegistryDownloadFailed, "Failed to download plugin file",
    			map[string]interface{}{"file": f, "detail": err.Error()})
			return
		}
	}
	// 可选文件
	for _, f := range []string{"config.user.json", "i18n/zh_CN.json", "i18n/en_US.json"} {
		dest := filepath.Join(localDir, f)
		_ = os.MkdirAll(filepath.Dir(dest), 0755)
		_ = downloadFile(fmt.Sprintf("%s/%s/%s", rawBaseURL, remoteDir, f), dest)
	}

	// 镜像白名单检查（Registry 安装路径必须通过）
	pkg, err := manifest.Load(localDir)
	if err != nil {
		os.RemoveAll(localDir)
		writeAppError(w, http.StatusInternalServerError, ErrPluginLoadFailed, "Failed to parse plugin manifest",
    		map[string]interface{}{"detail": err.Error()})
		return
	}
	if err := imagecheck.CheckBootSpec(pkg.BootSpec); err != nil {
		os.RemoveAll(localDir)
		writeAppError(w, http.StatusForbidden, ErrPluginImageNotAllowed, "Plugin image is not allowed",
    		map[string]interface{}{"detail": err.Error()})
		return
	}

	// 注册到 store（IsDevPlugin=false，永远不是 Dev 插件）
	s.installFromRegistry(w, localDir)
}

func fetchRegistry() (*Registry, error) {
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(registryURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var reg Registry
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry.json: %w", err)
	}
	return &reg, nil
}

func downloadFile(url, dest string) error {
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}