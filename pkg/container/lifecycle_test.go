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

package container

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestMain(m *testing.M) {
	// Lifecycle/network functions construct error and log text via i18n.T; initialize localizer first / 生命周期/网络函数会通过 i18n.T 构造错误与日志文本，需先初始化 localizer
	if err := griffinoi18n.Init("en"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// fakeDocker implements DockerAPI, recording calls and allowing per-method return values/errors to be injected / fakeDocker 实现 DockerAPI，记录调用并允许注入返回值/错误
type fakeDocker struct {
	calls []string

	listResult []container.Summary
	listErr    error

	stopErr   error
	removeErr error
	startErr  error
	createErr error
	createID  string

	networkListResult []network.Summary
	networkListErr    error
	networkCreateErr  error
	networkRemoveErr  error
}

// Compile-time assertion: fakeDocker satisfies DockerAPI / 编译期断言：fakeDocker 满足 DockerAPI
var _ DockerAPI = (*fakeDocker)(nil)

func (f *fakeDocker) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	f.calls = append(f.calls, "ContainerCreate:"+containerName)
	if f.createErr != nil {
		return container.CreateResponse{}, f.createErr
	}
	return container.CreateResponse{ID: f.createID}, nil
}

func (f *fakeDocker) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	f.calls = append(f.calls, "ContainerStart:"+containerID)
	return f.startErr
}

func (f *fakeDocker) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	f.calls = append(f.calls, "ContainerStop:"+containerID)
	return f.stopErr
}

func (f *fakeDocker) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	f.calls = append(f.calls, "ContainerRemove:"+containerID)
	return f.removeErr
}

func (f *fakeDocker) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	f.calls = append(f.calls, "ContainerList")
	return f.listResult, f.listErr
}

func (f *fakeDocker) ContainerInspectWithRaw(ctx context.Context, containerID string, getSize bool) (container.InspectResponse, []byte, error) {
	f.calls = append(f.calls, "ContainerInspectWithRaw:"+containerID)
	return container.InspectResponse{}, nil, nil
}

func (f *fakeDocker) ImageInspectWithRaw(ctx context.Context, imageID string) (image.InspectResponse, []byte, error) {
	f.calls = append(f.calls, "ImageInspectWithRaw:"+imageID)
	return image.InspectResponse{}, nil, nil
}

func (f *fakeDocker) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	f.calls = append(f.calls, "ImagePull:"+refStr)
	return io.NopCloser(nil), nil
}

func (f *fakeDocker) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error) {
	f.calls = append(f.calls, "NetworkList")
	return f.networkListResult, f.networkListErr
}

func (f *fakeDocker) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	f.calls = append(f.calls, "NetworkCreate:"+name)
	if f.networkCreateErr != nil {
		return network.CreateResponse{}, f.networkCreateErr
	}
	return network.CreateResponse{ID: "net-" + name}, nil
}

func (f *fakeDocker) NetworkRemove(ctx context.Context, networkID string) error {
	f.calls = append(f.calls, "NetworkRemove:"+networkID)
	return f.networkRemoveErr
}

func TestStopPluginStopsThenRemoves(t *testing.T) {
	fake := &fakeDocker{
		listResult: []container.Summary{
			{ID: "c1", Names: []string{"/griffino_acme_main"}, State: "running"},
		},
	}

	if err := StopPlugin(context.Background(), fake, "acme.thing"); err != nil {
		t.Fatalf("StopPlugin() error = %v", err)
	}

	want := []string{"ContainerList", "ContainerStop:c1", "ContainerRemove:c1"}
	assertCalls(t, fake.calls, want)
}

func TestStopPluginListError(t *testing.T) {
	fake := &fakeDocker{listErr: errors.New("daemon down")}
	if err := StopPlugin(context.Background(), fake, "acme.thing"); err == nil {
		t.Fatal("expected error when ContainerList fails")
	}
	assertCalls(t, fake.calls, []string{"ContainerList"})
}

func TestStopPluginStopError(t *testing.T) {
	fake := &fakeDocker{
		listResult: []container.Summary{
			{ID: "c1", Names: []string{"/griffino_acme_main"}, State: "running"},
		},
		stopErr: errors.New("stop failed"),
	}
	if err := StopPlugin(context.Background(), fake, "acme.thing"); err == nil {
		t.Fatal("expected error when ContainerStop fails")
	}
	// After a stop failure, remove should not continue / 停止失败后不应继续 remove
	assertCalls(t, fake.calls, []string{"ContainerList", "ContainerStop:c1"})
}

func TestStopPluginContainersSkipsNonRunning(t *testing.T) {
	fake := &fakeDocker{
		listResult: []container.Summary{
			{ID: "c1", Names: []string{"/griffino_acme_main"}, State: "running"},
			{ID: "c2", Names: []string{"/griffino_acme_side"}, State: "exited"},
		},
	}
	if err := StopPluginContainers(context.Background(), fake, "acme.thing"); err != nil {
		t.Fatalf("StopPluginContainers() error = %v", err)
	}
	// Only stop the running c1, skip the exited c2, and never remove / 只停止 running 的 c1，跳过 exited 的 c2，从不 remove
	assertCalls(t, fake.calls, []string{"ContainerList", "ContainerStop:c1"})
}

func TestCreateNetworkReusesExisting(t *testing.T) {
	name := NetworkName("acme.thing")
	fake := &fakeDocker{
		networkListResult: []network.Summary{{ID: "existing-id", Name: name}},
	}
	id, err := CreateNetwork(context.Background(), fake, "acme.thing")
	if err != nil {
		t.Fatalf("CreateNetwork() error = %v", err)
	}
	if id != "existing-id" {
		t.Errorf("id = %q, want existing-id", id)
	}
	// Reusing an existing network; NetworkCreate should not be called / 复用已存在的网络，不应调用 NetworkCreate
	assertCalls(t, fake.calls, []string{"NetworkList"})
}

func TestCreateNetworkCreatesWhenMissing(t *testing.T) {
	fake := &fakeDocker{}
	id, err := CreateNetwork(context.Background(), fake, "acme.thing")
	if err != nil {
		t.Fatalf("CreateNetwork() error = %v", err)
	}
	wantName := NetworkName("acme.thing")
	if id != "net-"+wantName {
		t.Errorf("id = %q, want net-%s", id, wantName)
	}
	assertCalls(t, fake.calls, []string{"NetworkList", "NetworkCreate:" + wantName})
}

func TestCreateNetworkCreateError(t *testing.T) {
	fake := &fakeDocker{networkCreateErr: errors.New("boom")}
	if _, err := CreateNetwork(context.Background(), fake, "acme.thing"); err == nil {
		t.Fatal("expected error when NetworkCreate fails")
	}
}

func TestRemoveNetworkPropagatesError(t *testing.T) {
	fake := &fakeDocker{networkRemoveErr: errors.New("in use")}
	if err := RemoveNetwork(context.Background(), fake, "acme.thing"); err == nil {
		t.Fatal("expected error when NetworkRemove fails")
	}
	assertCalls(t, fake.calls, []string{"NetworkRemove:" + NetworkName("acme.thing")})
}

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q (full got=%v)", i, got[i], want[i], got)
		}
	}
}
