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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GriffinGuard/Griffino/internal/imagecheck"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// defaultRegistryBaseURL is the official public plugin registry base URL / 官方公共插件仓库基地址.
const defaultRegistryBaseURL = "https://raw.githubusercontent.com/GriffinGuard/Griffino-Plugins/main"

// registryBaseURL returns the plugin registry base URL.
//
// Defaults to the official public registry; advanced users can override via
// GRIFFINO_REGISTRY_BASE_URL. This is an intentionally low-visibility,
// undocumented "at your own risk" switch: custom sources bypass the official
// trust model (image vetting & audits), intended for local dev or private
// deployments only. When unset, the hardcoded official default is used / 返回 registry 基地址，默认官方源；GRIFFINO_REGISTRY_BASE_URL 可覆盖（自担风险），未设置时锁死官方默认值.
func registryBaseURL() string {
	if v := strings.TrimRight(os.Getenv("GRIFFINO_REGISTRY_BASE_URL"), "/"); v != "" {
		return v
	}
	return defaultRegistryBaseURL
}

// registryIndexURL returns the full URL of registry.json / 返回 registry.json 的完整地址.
func registryIndexURL() string { return registryBaseURL() + "/registry.json" }

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

// pluginWithStatus aggregates a registry entry with local install status, used by list and detail views / 把 registry 条目与本地安装状态聚合，供列表与详情共用.
type pluginWithStatus struct {
	RegistryPlugin
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	UpdateAvailable  bool   `json:"updateAvailable"`
}

// withInstallStatus reads the local store and fills installed / installedVersion / updateAvailable / 读取本地 store，填充 installed / installedVersion / updateAvailable.
func (s *Server) withInstallStatus(p RegistryPlugin) pluginWithStatus {
	item := pluginWithStatus{RegistryPlugin: p}
	if inst, _ := s.st.GetPlugin(p.ID); inst != nil {
		item.Installed = true
		item.InstalledVersion = filepath.Base(inst.PluginDir)
		item.UpdateAvailable = item.InstalledVersion != p.LatestVersion
	}
	return item
}

// GET /api/v1/registry/plugins
//
//	@Summary	List registry plugins
//	@Tags		Registry
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	502	{object}	api.AppError
//	@Router		/registry/plugins [get]
func (s *Server) handleListRegistryPlugins(w http.ResponseWriter, r *http.Request) {
	registry, err := fetchRegistry()
	if err != nil {
		writeAppError(w, http.StatusBadGateway, ErrRegistryFetchFailed, "Failed to fetch plugin registry",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	result := make([]pluginWithStatus, 0, len(registry.Plugins))
	for _, p := range registry.Plugins {
		result = append(result, s.withInstallStatus(p))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updatedAt": registry.UpdatedAt,
		"plugins":   result,
	})
}

// GET /api/v1/registry/plugins/{id}
// Returns full registry info for a single plugin (all versions + changelog) with local install status / 返回单个插件的完整 registry 信息与本地安装状态.
//
//	@Summary	Get a registry plugin
//	@Tags		Registry
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Plugin ID"
//	@Success	200	{object}	map[string]interface{}
//	@Failure	404	{object}	api.AppError
//	@Failure	502	{object}	api.AppError
//	@Router		/registry/plugins/{id} [get]
func (s *Server) handleGetRegistryPlugin(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	registry, err := fetchRegistry()
	if err != nil {
		writeAppError(w, http.StatusBadGateway, ErrRegistryFetchFailed, "Failed to fetch plugin registry",
			map[string]interface{}{"detail": err.Error()})
		return
	}
	target := findRegistryPlugin(registry, pluginID)
	if target == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found in registry",
			map[string]interface{}{"id": pluginID})
		return
	}
	writeJSON(w, http.StatusOK, s.withInstallStatus(*target))
}

// versionRequest is the optional request body for install / upgrade; defaults to LatestVersion / install / upgrade 的可选请求体，缺省取 LatestVersion.
type versionRequest struct {
	Version string `json:"version"`
}

// decodeVersion parses the optional request body; an empty body means no version specified / 解析可选请求体，空 body 视为未指定版本.
func decodeVersion(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	var req versionRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // tolerate empty/invalid body as unspecified / 空/非法 body 容忍为未指定
	return req.Version
}

// POST /api/v1/registry/plugins/{id}/install
//
//	@Summary	Install a registry plugin
//	@Tags		Registry
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		string	true	"Plugin ID"
//	@Param		body	body		object	false	"optional version (defaults to latest)"
//	@Success	201		{object}	map[string]interface{}
//	@Failure	403		{object}	api.AppError
//	@Failure	404		{object}	api.AppError
//	@Failure	409		{object}	api.AppError
//	@Failure	502		{object}	api.AppError
//	@Router		/registry/plugins/{id}/install [post]
func (s *Server) handleInstallRegistryPlugin(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	registry, err := fetchRegistry()
	if err != nil {
		writeAppError(w, http.StatusBadGateway, ErrRegistryFetchFailed, "Failed to fetch plugin registry",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	target := findRegistryPlugin(registry, pluginID)
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

	version := decodeVersion(r)
	if version == "" {
		version = target.LatestVersion
	}
	if !hasVersion(target, version) {
		writeAppError(w, http.StatusNotFound, ErrRegistryVersionNotFound, "Plugin version not found in registry",
			map[string]interface{}{"id": pluginID, "version": version})
		return
	}

	localDir, _, err := s.fetchAndVerifyVersion(pluginID, version)
	if err != nil {
		status, code := registryFetchErrStatus(err)
		writeAppError(w, status, code, "Failed to download plugin",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	// Register in store (IsDevPlugin=false; registry plugins are never dev plugins) / 注册到 store，IsDevPlugin=false，永远不是 Dev 插件
	s.installFromRegistry(w, localDir)
}

// POST /api/v1/registry/plugins/{id}/upgrade
// Upgrades an installed plugin to the specified version (defaults to LatestVersion).
// A running plugin is automatically stopped, switched, and restarted / 升级到指定版本（缺省 LatestVersion），运行中会自动停-切-重启.
//
//	@Summary	Upgrade a registry plugin
//	@Tags		Registry
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		string	true	"Plugin ID"
//	@Param		body	body		object	false	"optional version (defaults to latest)"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	404		{object}	api.AppError
//	@Failure	409		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Failure	502		{object}	api.AppError
//	@Router		/registry/plugins/{id}/upgrade [post]
func (s *Server) handleUpgradeRegistryPlugin(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	inst, _ := s.st.GetPlugin(pluginID)
	if inst == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not installed",
			map[string]interface{}{"id": pluginID})
		return
	}
	if inst.IsDevPlugin {
		writeAppError(w, http.StatusConflict, ErrPluginInvalidState, "Dev plugins cannot be upgraded from the registry",
			map[string]interface{}{"id": pluginID})
		return
	}

	registry, err := fetchRegistry()
	if err != nil {
		writeAppError(w, http.StatusBadGateway, ErrRegistryFetchFailed, "Failed to fetch plugin registry",
			map[string]interface{}{"detail": err.Error()})
		return
	}
	target := findRegistryPlugin(registry, pluginID)
	if target == nil {
		writeAppError(w, http.StatusNotFound, ErrPluginNotFound, "Plugin not found in registry",
			map[string]interface{}{"id": pluginID})
		return
	}

	version := decodeVersion(r)
	if version == "" {
		version = target.LatestVersion
	}
	if !hasVersion(target, version) {
		writeAppError(w, http.StatusNotFound, ErrRegistryVersionNotFound, "Plugin version not found in registry",
			map[string]interface{}{"id": pluginID, "version": version})
		return
	}
	if filepath.Base(inst.PluginDir) == version {
		writeAppError(w, http.StatusConflict, ErrPluginAlreadyUpToDate, "Plugin already at this version",
			map[string]interface{}{"id": pluginID, "version": version})
		return
	}

	localDir, pkg, err := s.fetchAndVerifyVersion(pluginID, version)
	if err != nil {
		status, code := registryFetchErrStatus(err)
		writeAppError(w, status, code, "Failed to download plugin",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	if err := s.pluginSvc.UpgradePlugin(r.Context(), pluginID, localDir, pkg); err != nil {
		// If upgrade fails before the directory switch, the new download dir is unused — clean up to avoid leftovers / 若升级在切目录前失败，新下载目录未被采用，清理以免残留.
		if cur, _ := s.st.GetPlugin(pluginID); cur == nil || cur.PluginDir != localDir {
			os.RemoveAll(localDir)
		}
		writeAppError(w, http.StatusInternalServerError, ErrPluginUpgradeFailed, "Failed to upgrade plugin",
			map[string]interface{}{"detail": err.Error()})
		return
	}

	updated, _ := s.st.GetPlugin(pluginID)
	resp := map[string]any{"id": pluginID, "version": version, "name": pkg.Manifest.Name}
	if updated != nil {
		resp["status"] = updated.Status
		resp["configDirty"] = updated.ConfigDirty
	}
	writeJSON(w, http.StatusOK, resp)
}

// findRegistryPlugin finds a plugin by ID in the registry; returns nil if not found / 在 registry 中按 ID 查找插件，未找到返回 nil.
func findRegistryPlugin(reg *Registry, id string) *RegistryPlugin {
	for i := range reg.Plugins {
		if reg.Plugins[i].ID == id {
			return &reg.Plugins[i]
		}
	}
	return nil
}

// hasVersion reports whether the plugin's versions[] contains the given version / 判断插件的 versions[] 中是否存在指定版本.
func hasVersion(p *RegistryPlugin, version string) bool {
	if version == p.LatestVersion {
		return true
	}
	for _, v := range p.Versions {
		if v.Version == version {
			return true
		}
	}
	return false
}

// registryFetchErrStatus maps fetchAndVerifyVersion errors to HTTP status and error code / 把 fetchAndVerifyVersion 的错误映射到 HTTP 状态与错误码.
func registryFetchErrStatus(err error) (int, ErrorCode) {
	var ue *imagecheck.UnapprovedError
	if errors.As(err, &ue) {
		return http.StatusForbidden, ErrPluginImageNotAllowed
	}
	return http.StatusBadGateway, ErrRegistryDownloadFailed
}

// fetchAndVerifyVersion downloads all files for the specified version to
// ~/.griffino/plugins/{id}/{version}, parses the manifest and checks the image whitelist.
// On any failure the created directory is cleaned up. Shared by install and upgrade / 下载指定版本文件到 plugins/{id}/{version}，解析 manifest 并做白名单检查，失败时清理目录，install/upgrade 共用.
func (s *Server) fetchAndVerifyVersion(pluginID, version string) (string, *manifest.PluginPackage, error) {
	remoteDir := fmt.Sprintf("plugins/%s/%s", pluginID, version)

	homeDir, _ := os.UserHomeDir()
	localDir := filepath.Join(homeDir, ".griffino", "plugins", pluginID, version)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return "", nil, fmt.Errorf("create plugin directory: %w", err)
	}

	// Must-download core files / 必须下载的核心文件
	for _, f := range []string{"plugin.manifest.json", "plugin.boot.yml", "config.boot.json"} {
		url := fmt.Sprintf("%s/%s/%s", registryBaseURL(), remoteDir, f)
		if err := downloadFile(url, filepath.Join(localDir, f)); err != nil {
			os.RemoveAll(localDir)
			return "", nil, fmt.Errorf("download %s: %w", f, err)
		}
	}
	// Optional files / 可选文件
	for _, f := range []string{"config.user.json", "i18n/zh_CN.json", "i18n/en_US.json"} {
		dest := filepath.Join(localDir, f)
		_ = os.MkdirAll(filepath.Dir(dest), 0755)
		_ = downloadFile(fmt.Sprintf("%s/%s/%s", registryBaseURL(), remoteDir, f), dest)
	}

	pkg, err := manifest.Load(localDir)
	if err != nil {
		os.RemoveAll(localDir)
		return "", nil, fmt.Errorf("parse plugin manifest: %w", err)
	}
	if err := imagecheck.CheckBootSpec(pkg.BootSpec); err != nil {
		os.RemoveAll(localDir)
		return "", nil, err
	}
	return localDir, pkg, nil
}

func fetchRegistry() (*Registry, error) {
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(registryIndexURL())
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
