package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/gnolang/gno/contribs/tx-archive/backup"
	"github.com/gnolang/gno/contribs/tx-archive/backup/client/rpc"
	"github.com/gnolang/gno/contribs/tx-archive/backup/writer/standard"
)

const (
	setReadOnly   = `middlewares: ["ipwhitelist"]`
	unsetReadOnly = `middlewares: []`
)

type snapshotter struct {
	dockerClient *client.Client

	containerName      string
	backupFile         string
	instanceBackupFile string

	cfg config

	url string
}

type config struct {
	rpcAddr        string
	traefikGnoFile string

	snapshotsDir     string
	masterBackupFile string

	hostPWD string
}

func NewSnapshotter(dockerClient *client.Client, cfg config) (*snapshotter, error) {
	timenow := time.Now()
	now := fmt.Sprintf("%s_%v", timenow.Format("2006-01-02_"), timenow.UnixNano())

	backupFile, err := filepath.Abs(cfg.masterBackupFile)
	if err != nil {
		return nil, err
	}
	instanceBackupFile, err := filepath.Abs(fmt.Sprintf("%s/backup_%s.jsonl", cfg.snapshotsDir, now))
	if err != nil {
		return nil, err
	}
	return &snapshotter{
		dockerClient: dockerClient,

		cfg: cfg,

		containerName:      "gno-" + now,
		backupFile:         backupFile,
		instanceBackupFile: instanceBackupFile,
	}, nil
}

// pullLatestImage get latest version of the docker image
func (s snapshotter) pullLatestImage(ctx context.Context) (bool, error) {
	reader, err := s.dockerClient.ImagePull(ctx, "ghcr.io/gnolang/gno/gnoland:master", types.ImagePullOptions{})
	if err != nil {
		return false, err
	}
	var b bytes.Buffer
	defer reader.Close()

	_, err = io.Copy(&b, reader)
	if err != nil {
		return false, err
	}

	return !strings.Contains(b.String(), "Image is up to date"), nil
}

func (s snapshotter) switchTraefikMode(replaceStr string) error {
	input, err := ioutil.ReadFile(s.cfg.traefikGnoFile)
	if err != nil {
		return err
	}

	regex := regexp.MustCompile(`middlewares: \[.*\]`)
	output := regex.ReplaceAllLiteral(input, []byte(replaceStr))

	return os.WriteFile(s.cfg.traefikGnoFile, output, 0o655)
}

func (s snapshotter) switchTraefikPortalLoop(url string) error {
	input, err := ioutil.ReadFile(s.cfg.traefikGnoFile)
	if err != nil {
		return err
	}

	regex := regexp.MustCompile(`http://.*:[0-9]+`)
	output := regex.ReplaceAllLiteral(input, []byte(url))

	return os.WriteFile(s.cfg.traefikGnoFile, output, 0o655)
}

func (s snapshotter) getPortalLoopContainers(ctx context.Context) ([]types.Container, error) {
	// Check if a portal loop is running
	containers, err := s.dockerClient.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return []types.Container{}, err
	}

	portalLoopContainers := make([]types.Container, 0)

	for _, container := range containers {
		if _, exists := container.Labels["the-portal-loop"]; exists {
			portalLoopContainers = append(portalLoopContainers, container)
		}
	}

	return portalLoopContainers, nil
}

func (s snapshotter) startPortalLoopContainer(ctx context.Context) (*types.Container, error) {
	// Create Docker volume
	_, err := s.dockerClient.VolumeCreate(ctx, volume.CreateOptions{
		Name: s.containerName,
	})
	if err != nil {
		return nil, err
	}

	// Run Docker container
	dockerContainer, err := s.dockerClient.ContainerCreate(ctx, &container.Config{
		Image: "ghcr.io/gnolang/gno/gnoland:master",
		Labels: map[string]string{
			"the-portal-loop": s.containerName,
		},
		WorkingDir: "/gnoroot",
		Env: []string{
			"MONIKER=the-portal-loop",
			"GENESIS_BACKUP_FILE=/backups/backup.jsonl",
			"GENESIS_BALANCES_FILE=/backups/balances.jsonl",
		},
		Entrypoint: []string{"/scripts/start.sh"},
		ExposedPorts: nat.PortSet{
			"26656/tcp": struct{}{},
			"26657/tcp": struct{}{},
		},
	}, &container.HostConfig{
		PublishAllPorts: true,
		PortBindings: nat.PortMap{
			"26657/tcp": []nat.PortBinding{
				{HostIP: "127.0.0.1"},
			},
		},
		Binds: []string{
			fmt.Sprintf("%s/scripts:/scripts", s.cfg.hostPWD),
			fmt.Sprintf("%s/backups:/backups", s.cfg.hostPWD),
			fmt.Sprintf("%s:/gnoroot/gnoland-data", s.containerName),
		},
	}, nil, nil, s.containerName)
	if err != nil {
		return nil, err
	}

	err = s.dockerClient.NetworkConnect(ctx, "portal-loop", dockerContainer.ID, nil)
	if err != nil {
		return nil, err
	}

	if err := s.dockerClient.ContainerStart(ctx, dockerContainer.ID, types.ContainerStartOptions{}); err != nil {
		return nil, err
	}
	time.Sleep(time.Second * 5)

	containers, err := s.getPortalLoopContainers(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		if c.ID == dockerContainer.ID {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("container not found")
}

func (s snapshotter) backupTXs(ctx context.Context, rpcURL string) error {
	cfg := backup.DefaultConfig()
	cfg.FromBlock = 1
	cfg.Watch = false

	// We want to skip failed txs on the Portal Loop reset,
	// because they might (unexpectedly) succeed
	cfg.SkipFailedTx = true

	instanceBackupFile, err := os.Create(s.instanceBackupFile)
	if err != nil {
		return err
	}
	defer instanceBackupFile.Close()

	w := standard.NewWriter(instanceBackupFile)

	// Create the tx-archive backup service
	c, err := rpc.NewHTTPClient(rpcURL)
	if err != nil {
		return fmt.Errorf("could not create tx-archive client, %w", err)
	}

	backupService := backup.NewService(c, w)

	// Run the backup service
	if backupErr := backupService.ExecuteBackup(ctx, cfg); backupErr != nil {
		return fmt.Errorf("unable to execute backup, %w", backupErr)
	}

	if err := instanceBackupFile.Sync(); err != nil {
		return err
	}

	info, err := instanceBackupFile.Stat()
	if err != nil {
		return err
	} else if info.Size() == 0 {
		return os.Remove(instanceBackupFile.Name())
	}

	// Append to backup file
	backupFile, err := os.OpenFile(s.backupFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("unable to open file %s, %w", s.backupFile, err)
	}
	defer backupFile.Close()

	// NOTE(albttx): Impossible to use io.ReadAll(instanceBackupFile)
	output, err := ioutil.ReadFile(s.instanceBackupFile)
	if err != nil {
		return err
	}

	_, err = backupFile.Write(output)
	return err
}
