//go:build integration

package integration

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

// What a configDir change does to the *container* — reload, restart or nothing —
// lives with the other service-transition scenarios in container_state_*_test.go.
// This file is only for what a configDir looks like on disk.
//
// Every test here runs with WithOSConfigStore: the versioned-directory logic
// uses symlinks, which afero.MemMapFs does not support. Only the config store is
// OS-backed; unit files still live on the in-memory filesystem.

// TestNewEmptyContainerConfigDir_CreatesDir pins that a configDir declaring no
// files still materializes as a real directory.
//
// The version directory is created even when empty because ..data points at it
// and podman needs a real directory on disk to bind-mount; an empty configDir
// that produced nothing would fail the container at start with no diagnostic
// pointing back at the spec.
func TestNewEmptyContainerConfigDir_CreatesDir(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath)))))

	env.Apply()

	env.AssertConfigDirExists("webapp", mountPath)
	env.AssertConfigDirFiles("webapp", mountPath)
}

// TestContainerConfigDirRemoved_RemovesOnlyThatDir pins that dropping one
// configDir reclaims that directory and leaves the container's other one
// untouched.
//
// Removal is addressed by the hash of the mount path, so the blast radius is
// only correct if that addressing is. A test carrying a single configDir cannot
// tell a precisely-scoped removal from one that took the whole unit directory
// with it — both leave nothing behind and both look right. The second
// directory is what makes the difference observable.
func TestContainerConfigDirRemoved_RemovesOnlyThatDir(t *testing.T) {
	removed := model.ContainerMountPath("/etc/app/")
	kept := model.ContainerMountPath("/etc/other/")

	removedFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "going away", 0),
	}
	keptFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("other.conf", "staying", 0),
	}

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.Dirs(
		model.NewContainerDirMount(string(removed), removedFiles...),
		model.NewContainerDirMount(string(kept), keptFiles...),
	)))
	env.SeedConfigDir("webapp", removed, 1, removedFiles...)
	env.SeedConfigDir("webapp", kept, 1, keptFiles...)
	env.Specs(systest.NewContainer("webapp", "nginx:latest", systest.Dirs(
		model.NewContainerDirMount(string(kept), keptFiles...),
	)))

	env.Apply()

	env.AssertConfigDirAbsent("webapp", removed)
	env.AssertConfigDirFiles("webapp", kept, keptFiles...)
	env.AssertConfigDirVersion("webapp", kept, 1)
}
