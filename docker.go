package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Docker is a struct that provides methods to interact with Docker running on the host.
type Docker struct {
	client *client.Client
}

// NewDocker creates a new Docker client instance, returning an error if the client could not be created.
func NewDocker() (*Docker, error) {
	c, err := client.NewClientWithOpts()
	if err != nil {
		return nil, err
	}
	return &Docker{client: c}, nil
}

// BuildImage attempts to build a new docker image for the PR, using the current directory as the build context.
// It assumes that the Dockerfile is present, as well as the necessary files for the specific PR.
func (d *Docker) BuildImage(pr string) error {
	name := "pr-" + pr
	err := exec.Command("docker", "build", "--build-arg", "PR="+pr, "-t", name, ".").Run()
	if err != nil {
		return err
	}
	_ = exec.Command("docker", "kill", "--signal=SIGINT", name, "&&", "docker", "wait", name).Run()
	return nil
}

// ServerPort retrieves the public port of the server running for the given PR. If the server is not running,
// it returns false. If an error occurs while listing the containers, it returns the error.
func (d *Docker) ServerPort(pr string) (uint16, bool, error) {
	opts := container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "pr="+pr)),
	}
	containers, err := d.client.ContainerList(context.Background(), opts)
	if err != nil {
		return 0, false, fmt.Errorf("list containers: %w", err)
	} else if len(containers) == 0 {
		return 0, false, nil
	}
	port := containers[0].Ports[0].PublicPort
	return port, true, nil
}

// mountDiskImage creates a fixed-size ext4 disk image for the PR (if one doesn't already exist) and mounts
// it at the PR directory. This limits the writable space available to the container.
func mountDiskImage(pr string) error {
	name := "pr-" + pr
	imgPath := name + ".img"

	if _, err := os.Stat(imgPath); err != nil {
		if err := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s", imgPath), "bs=1M", "count=256").Run(); err != nil {
			return fmt.Errorf("create disk image: %w", err)
		}
		if err := exec.Command("mkfs.ext4", "-F", imgPath).Run(); err != nil {
			_ = os.Remove(imgPath)
			return fmt.Errorf("format disk image: %w", err)
		}
	}

	_ = os.MkdirAll(name, 0755)
	// Unmount first in case it's already mounted from a previous run.
	_ = exec.Command("umount", name).Run()
	if err := exec.Command("mount", "-o", "loop", imgPath, name).Run(); err != nil {
		return fmt.Errorf("mount disk image: %w", err)
	}
	return nil
}

// unmountDiskImage unmounts the disk image for the given PR.
func unmountDiskImage(pr string) {
	_ = exec.Command("umount", "pr-"+pr).Run()
}

// removeDiskImage unmounts and deletes the disk image file for the given PR.
func removeDiskImage(pr string) {
	unmountDiskImage(pr)
	_ = os.Remove("pr-" + pr + ".img")
}

// unmountAllDiskImages finds and unmounts all PR disk images.
func unmountAllDiskImages() {
	matches, _ := filepath.Glob("pr-*.img")
	for _, img := range matches {
		name := strings.TrimSuffix(img, ".img")
		_ = exec.Command("umount", name).Run()
	}
}

// StartServer attempts to start a server for the given PR. It runs a Docker container with the specified name
// and random port mapping. If the server starts successfully, it retrieves the public port and returns it.
// If the server fails to start, it returns an error.
func (d *Docker) StartServer(pr string) (uint16, bool, error) {
	name := "pr-" + pr
	if err := mountDiskImage(pr); err != nil {
		return 0, false, fmt.Errorf("mount disk image: %w", err)
	}
	cmd := exec.Command("docker", "run", "-d", "--rm", "--name", name, "--label", "pr="+pr, "-v", "./"+name+":/"+name, "-p", "0:19132/udp", name)
	err := cmd.Run()
	if err != nil {
		unmountDiskImage(pr)
		return 0, false, fmt.Errorf("run command '%s': %w", cmd.String(), err)
	}
	port, found, err := d.ServerPort(pr)
	if err != nil {
		return 0, false, fmt.Errorf("get server port: %w", err)
	} else if !found {
		return 0, false, nil
	}
	return port, true, nil
}

// DeleteServer stops and removes the Docker container for the given PR, as well as removing the associated image.
func (d *Docker) DeleteServer(pr string) {
	name := "pr-" + pr
	_ = exec.Command("docker", "kill", "--signal=SIGINT", name).Run()
	_ = exec.Command("docker", "wait", name).Run()
	_ = exec.Command("docker", "image", "rm", name).Run()
	removeDiskImage(pr)
}

// StopServer stops the server for the given PR gracefully by sending a SIGINT signal to the Docker container.
func (d *Docker) StopServer(pr string) {
	name := "pr-" + pr
	_ = exec.Command("docker", "kill", "--signal=SIGINT", name, "&&", "docker", "wait", name).Run()
}

// ClearContainers removes all Docker containers that are associated with pull requests.
func (d *Docker) ClearContainers() error {
	containers, err := d.client.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range containers {
		if strings.HasPrefix(c.Image, "pr-") {
			if err := d.client.ContainerKill(context.Background(), c.ID, "SIGINT"); err != nil {
				return fmt.Errorf("remove container %s: %w", c.ID, err)
			}
		}
	}
	unmountAllDiskImages()
	return nil
}

// Close closes the Docker client connection, releasing any resources it holds.
func (d *Docker) Close() {
	if d.client != nil {
		_ = d.client.Close()
		d.client = nil
	}
}
