package driver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type hostMount struct {
	Source      string
	Destination string
}

type cpuSample struct {
	TotalUsage  uint64
	SystemUsage uint64
	Timestamp   time.Time
}

type containerStatsSnapshot struct {
	CPUPct      float64
	MemoryMB    float64
	MemoryPct   float64
	NetInputMB  float64
	NetOutputMB float64
	UpdatedAt   time.Time
}

type SocketDriver struct {
	client     *DockerClient
	mountsMu   sync.Mutex
	mounts     []hostMount
	mountsInit bool

	cpuMu     sync.Mutex
	prevCPUs  map[string]cpuSample
	lastStats map[string]containerStatsSnapshot
}

func NewSocketDriver(hostAddress string) (*SocketDriver, error) {
	c, err := NewDockerClient(hostAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &SocketDriver{
		client:    c,
		prevCPUs:  make(map[string]cpuSample),
		lastStats: make(map[string]containerStatsSnapshot),
	}, nil
}

func (d *SocketDriver) Ping(ctx context.Context) error {
	return d.client.Get(ctx, "/_ping", nil)
}

func (d *SocketDriver) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	var info struct {
		Name            string `json:"Name"`
		OperatingSystem string `json:"OperatingSystem"`
		KernelVersion   string `json:"KernelVersion"`
		ServerVersion   string `json:"ServerVersion"`
		NCPU            int    `json:"NCPU"`
		MemTotal        int64  `json:"MemTotal"`
	}
	if err := d.client.Get(ctx, "/info", &info); err != nil {
		return nil, err
	}

	return &SystemInfo{
		Hostname:      info.Name,
		OS:            info.OperatingSystem,
		KernelVersion: info.KernelVersion,
		DockerVersion: info.ServerVersion,
		TotalCPUs:     info.NCPU,
		TotalRAMBytes: uint64(info.MemTotal),
	}, nil
}

type rawContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort uint16 `json:"PrivatePort"`
		PublicPort  uint16 `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

func (d *SocketDriver) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	var raw []rawContainer
	if err := d.client.Get(ctx, "/containers/json?all=1", &raw); err != nil {
		return nil, err
	}

	// Pre-fetch images to map ImageID and tags to RepoDigests
	type rawImageSummary struct {
		ID          string   `json:"Id"`
		RepoTags    []string `json:"RepoTags"`
		RepoDigests []string `json:"RepoDigests"`
	}
	var rawImages []rawImageSummary
	_ = d.client.Get(ctx, "/images/json?all=1", &rawImages)

	imageDigests := make(map[string][]string)
	imageTags := make(map[string][]string)
	tagToImageID := make(map[string]string)
	for _, img := range rawImages {
		if len(img.RepoDigests) > 0 {
			imageDigests[img.ID] = img.RepoDigests
			imageDigests[cleanDigest(img.ID)] = img.RepoDigests
			for _, tag := range img.RepoTags {
				imageDigests[tag] = img.RepoDigests
			}
		}
		if len(img.RepoTags) > 0 {
			imageTags[img.ID] = img.RepoTags
			imageTags[cleanDigest(img.ID)] = img.RepoTags
			for _, tag := range img.RepoTags {
				if tag != "" && tag != "<none>:<none>" {
					tagToImageID[tag] = img.ID
					if strings.HasSuffix(tag, ":latest") {
						tagToImageID[strings.TrimSuffix(tag, ":latest")] = img.ID
					}
					trimmed := strings.TrimPrefix(tag, "docker.io/")
					trimmed = strings.TrimPrefix(trimmed, "library/")
					tagToImageID[trimmed] = img.ID
					if strings.HasSuffix(trimmed, ":latest") {
						tagToImageID[strings.TrimSuffix(trimmed, ":latest")] = img.ID
					}
				}
			}
		}
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		// Identify internal DockerPulse updater helper containers
		isInternalHelper := c.Labels["com.dockerpulse.helper"] == "true" || strings.Contains(c.Command, "sleep 2 && docker compose")
		if !isInternalHelper {
			for _, name := range c.Names {
				trimmed := strings.TrimPrefix(name, "/")
				if strings.HasPrefix(trimmed, "dockerpulse-updater") {
					isInternalHelper = true
					break
				}
			}
		}

		if isInternalHelper {
			// If it's done or stuck in created/exited/dead state, asynchronously clean it up
			if c.State == "created" || c.State == "exited" || c.State == "dead" {
				go func(cid string) {
					_ = d.client.Delete(context.Background(), fmt.Sprintf("/containers/%s?force=true", cid))
				}(c.ID)
			}
			continue
		}

		displayImage := c.Image
		if strings.HasPrefix(displayImage, "sha256:") || displayImage == "" {
			if composeImg := c.Labels["com.docker.compose.image"]; composeImg != "" && !strings.HasPrefix(composeImg, "sha256:") {
				displayImage = composeImg
			} else if tags := imageTags[c.ImageID]; len(tags) > 0 {
				for _, t := range tags {
					if t != "" && t != "<none>:<none>" && !strings.HasPrefix(t, "sha256:") {
						displayImage = t
						break
					}
				}
			} else if tags := imageTags[cleanDigest(c.ImageID)]; len(tags) > 0 {
				for _, t := range tags {
					if t != "" && t != "<none>:<none>" && !strings.HasPrefix(t, "sha256:") {
						displayImage = t
						break
					}
				}
			}

			// If still sha256 or empty, inspect container to read Config.Image
			if strings.HasPrefix(displayImage, "sha256:") || displayImage == "" {
				var detail struct {
					Config struct {
						Image string `json:"Image"`
					} `json:"Config"`
				}
				if err := d.client.Get(ctx, "/containers/"+c.ID+"/json", &detail); err == nil {
					if detail.Config.Image != "" && !strings.HasPrefix(detail.Config.Image, "sha256:") {
						displayImage = detail.Config.Image
					}
				}
			}
		}

		// Check if local image repository tag has already been updated to a newer ID than the container's running image
		hasLocalUpdate := false
		lookupTag := strings.TrimPrefix(displayImage, "docker.io/")
		lookupTag = strings.TrimPrefix(lookupTag, "library/")
		localTagID := tagToImageID[lookupTag]
		if localTagID == "" && !strings.Contains(lookupTag, ":") {
			localTagID = tagToImageID[lookupTag+":latest"]
		}
		if localTagID != "" && cleanDigest(localTagID) != cleanDigest(c.ImageID) {
			hasLocalUpdate = true
		}

		digests := imageDigests[c.ImageID]
		if len(digests) == 0 {
			digests = imageDigests[cleanDigest(c.ImageID)]
		}
		if len(digests) == 0 {
			digests = imageDigests[c.Image]
		}

		info := ContainerInfo{
			ID:          c.ID,
			Names:       c.Names,
			Image:       displayImage,
			ImageID:     c.ImageID,
			RepoDigests: digests,
			Command:     c.Command,
			Created:     c.Created,
			State:       c.State,
			Status:      c.Status,
			HasUpdate:   hasLocalUpdate,
			Stack:       c.Labels["com.docker.compose.project"],
			Service:     c.Labels["com.docker.compose.service"],
			WorkingDir:  c.Labels["com.docker.compose.project.working_dir"],
			ConfigFile:  c.Labels["com.docker.compose.project.config_files"],
		}

		for _, p := range c.Ports {
			info.Ports = append(info.Ports, ContainerPort{
				IP:          p.IP,
				PrivatePort: p.PrivatePort,
				PublicPort:  p.PublicPort,
				Type:        p.Type,
			})
		}

		result = append(result, info)
	}

	// Fetch quick stats in parallel for running containers with worker concurrency pool
	var wg sync.WaitGroup
	statsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	sem := make(chan struct{}, 8)

	for i := range result {
		if result[i].State == "running" {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-statsCtx.Done():
					d.cpuMu.Lock()
					if cached, ok := d.lastStats[result[idx].ID]; ok {
						result[idx].CPUPct = cached.CPUPct
						result[idx].MemoryMB = cached.MemoryMB
						result[idx].MemoryPct = cached.MemoryPct
						result[idx].NetInputMB = cached.NetInputMB
						result[idx].NetOutputMB = cached.NetOutputMB
					}
					d.cpuMu.Unlock()
					return
				}
				d.populateContainerStats(statsCtx, result[idx].ID, &result[idx])
			}(i)
		}
	}
	wg.Wait()

	return result, nil
}

func parseUint64(v interface{}) uint64 {
	switch val := v.(type) {
	case float64:
		return uint64(val)
	case int64:
		return uint64(val)
	case uint64:
		return val
	case int:
		return uint64(val)
	}
	return 0
}

func (d *SocketDriver) populateContainerStats(ctx context.Context, id string, info *ContainerInfo) {
	statsCtx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer cancel()

	var stats struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs  uint32 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64                 `json:"usage"`
			Limit uint64                 `json:"limit"`
			Stats map[string]interface{} `json:"stats"`
		} `json:"memory_stats"`
		Networks map[string]struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		} `json:"networks"`
		Network struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		} `json:"network"`
	}

	// First attempt with one-shot=true for instantaneous return without 1-second CPU delta blocking
	err := d.client.Get(statsCtx, fmt.Sprintf("/containers/%s/stats?stream=false&one-shot=true", id), &stats)
	if err != nil {
		// Fallback to standard stream=false if one-shot is rejected or fails
		err = d.client.Get(statsCtx, fmt.Sprintf("/containers/%s/stats?stream=false", id), &stats)
	}

	if err != nil {
		// If transient failure or timeout, fallback to last known good stats
		d.cpuMu.Lock()
		if cached, ok := d.lastStats[id]; ok {
			info.CPUPct = cached.CPUPct
			info.MemoryMB = cached.MemoryMB
			info.MemoryPct = cached.MemoryPct
			info.NetInputMB = cached.NetInputMB
			info.NetOutputMB = cached.NetOutputMB
		}
		d.cpuMu.Unlock()
		return
	}

	currentTotal := stats.CPUStats.CPUUsage.TotalUsage
	currentSystem := stats.CPUStats.SystemUsage
	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = 1
	}

	d.cpuMu.Lock()
	if d.prevCPUs == nil {
		d.prevCPUs = make(map[string]cpuSample)
	}
	prev, hasPrev := d.prevCPUs[id]
	d.prevCPUs[id] = cpuSample{
		TotalUsage:  currentTotal,
		SystemUsage: currentSystem,
		Timestamp:   time.Now(),
	}
	d.cpuMu.Unlock()

	var cpuDelta float64
	var systemDelta float64

	if hasPrev && prev.TotalUsage > 0 && currentTotal >= prev.TotalUsage {
		cpuDelta = float64(currentTotal - prev.TotalUsage)
		if currentSystem > prev.SystemUsage {
			systemDelta = float64(currentSystem - prev.SystemUsage)
		} else {
			elapsedNano := time.Since(prev.Timestamp).Nanoseconds()
			if elapsedNano > 0 {
				systemDelta = float64(elapsedNano)
			}
		}
	} else if stats.PreCPUStats.CPUUsage.TotalUsage > 0 && stats.PreCPUStats.SystemUsage > 0 {
		cpuDelta = float64(currentTotal) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
		systemDelta = float64(currentSystem) - float64(stats.PreCPUStats.SystemUsage)
	}

	if systemDelta > 0 && cpuDelta > 0 {
		cpuPct := (cpuDelta / systemDelta) * onlineCPUs * 100.0
		if cpuPct > onlineCPUs*100.0 {
			cpuPct = onlineCPUs * 100.0
		}
		info.CPUPct = math.Round(cpuPct*10) / 10
	} else {
		info.CPUPct = 0.0
	}

	// Memory usage: match Docker CLI by subtracting inactive_file / cache
	usedMem := stats.MemoryStats.Usage
	if statsMap := stats.MemoryStats.Stats; statsMap != nil {
		if usedMem == 0 {
			anon := parseUint64(statsMap["anon"])
			file := parseUint64(statsMap["file"])
			usedMem = anon + file
		}
		var cache uint64
		if v, ok := statsMap["inactive_file"]; ok {
			cache = parseUint64(v)
		} else if v, ok := statsMap["total_inactive_file"]; ok {
			cache = parseUint64(v)
		} else if v, ok := statsMap["cache"]; ok {
			cache = parseUint64(v)
		}
		if cache < usedMem {
			usedMem -= cache
		}
	}

	info.MemoryMB = math.Round((float64(usedMem)/(1024*1024))*10) / 10
	if stats.MemoryStats.Limit > 0 {
		pct := (float64(usedMem) / float64(stats.MemoryStats.Limit)) * 100.0
		info.MemoryPct = math.Round(pct*10) / 10
	}

	info.NetInputMB = 0
	info.NetOutputMB = 0
	for _, n := range stats.Networks {
		info.NetInputMB += float64(n.RxBytes) / (1024 * 1024)
		info.NetOutputMB += float64(n.TxBytes) / (1024 * 1024)
	}
	if len(stats.Networks) == 0 && (stats.Network.RxBytes > 0 || stats.Network.TxBytes > 0) {
		info.NetInputMB = float64(stats.Network.RxBytes) / (1024 * 1024)
		info.NetOutputMB = float64(stats.Network.TxBytes) / (1024 * 1024)
	}
	info.NetInputMB = math.Round(info.NetInputMB*10) / 10
	info.NetOutputMB = math.Round(info.NetOutputMB*10) / 10

	// Save to lastStats cache
	d.cpuMu.Lock()
	if d.lastStats == nil {
		d.lastStats = make(map[string]containerStatsSnapshot)
	}
	d.lastStats[id] = containerStatsSnapshot{
		CPUPct:      info.CPUPct,
		MemoryMB:    info.MemoryMB,
		MemoryPct:   info.MemoryPct,
		NetInputMB:  info.NetInputMB,
		NetOutputMB: info.NetOutputMB,
		UpdatedAt:   time.Now(),
	}
	d.cpuMu.Unlock()
}

func (d *SocketDriver) StartContainer(ctx context.Context, id string) error {
	return d.client.Post(ctx, fmt.Sprintf("/containers/%s/start", id), nil, nil)
}

func (d *SocketDriver) StopContainer(ctx context.Context, id string) error {
	return d.client.Post(ctx, fmt.Sprintf("/containers/%s/stop?t=15", id), nil, nil)
}

func (d *SocketDriver) RestartContainer(ctx context.Context, id string) error {
	return d.client.Post(ctx, fmt.Sprintf("/containers/%s/restart?t=15", id), nil, nil)
}

func (d *SocketDriver) RemoveContainer(ctx context.Context, id string, force bool) error {
	return d.client.Delete(ctx, fmt.Sprintf("/containers/%s?force=%t", id, force))
}

func (d *SocketDriver) StreamLogs(ctx context.Context, containerID string, follow bool, tail string, writer io.Writer) error {
	if tail == "" {
		tail = "200"
	}
	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&timestamps=1&follow=%t&tail=%s", containerID, follow, tail)
	return d.client.Stream(ctx, path, writer)
}

func (d *SocketDriver) ExecShell(ctx context.Context, containerID string, cmd []string, in io.Reader, out io.Writer, resizeChan <-chan TerminalSize) error {
	// Candidate shells to try if using default shell
	var shellCandidates [][]string
	if len(cmd) > 0 && !(len(cmd) == 1 && cmd[0] == "/bin/sh") {
		shellCandidates = [][]string{cmd}
	} else {
		shellCandidates = [][]string{
			{"/bin/sh"},
			{"/bin/bash"},
			{"sh"},
		}
	}

	var lastErr error
	for _, candidateCmd := range shellCandidates {
		err := d.runExec(ctx, containerID, candidateCmd, in, out, resizeChan)
		if err == nil {
			return nil
		}
		lastErr = err
		// If error is not executable not found, don't keep retrying other shells
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "executable file not found") && !strings.Contains(errLower, "no such file") {
			break
		}
	}

	if lastErr != nil {
		errLower := strings.ToLower(lastErr.Error())
		if strings.Contains(errLower, "executable file not found") || strings.Contains(errLower, "no such file") {
			_, _ = fmt.Fprintf(out, "\r\n\x1b[33m[DockerPulse] Notice: No shell (/bin/sh or /bin/bash) found in container.\x1b[0m\r\n\x1b[90mThis container is likely built 'FROM scratch' (such as Watchtower or static Go/Rust binaries) and does not have an interactive shell environment.\x1b[0m\r\n")
		}
	}
	return lastErr
}

func (d *SocketDriver) runExec(ctx context.Context, containerID string, cmd []string, in io.Reader, out io.Writer, resizeChan <-chan TerminalSize) error {
	// 1. Try native Docker Engine API exec first (handles PTY/TTY properly over socket/TCP)
	err := d.runDockerAPIExec(ctx, containerID, cmd, in, out, resizeChan)
	if err == nil {
		return nil
	}

	// If the shell binary does not exist in the container, do not attempt fallback
	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "executable file not found") || strings.Contains(errLower, "no such file") {
		return err
	}

	// 2. Subprocess fallback using 'docker exec -i' (WITHOUT -t to avoid "the input device is not a TTY")
	args := append([]string{"exec", "-i", containerID}, cmd...)
	execCmd := exec.CommandContext(ctx, "docker", args...)
	execCmd.Stdin = in
	execCmd.Stdout = out
	execCmd.Stderr = out
	return execCmd.Run()
}

func (d *SocketDriver) runDockerAPIExec(ctx context.Context, containerID string, cmd []string, in io.Reader, out io.Writer, resizeChan <-chan TerminalSize) error {
	type execConfig struct {
		AttachStdin  bool     `json:"AttachStdin"`
		AttachStdout bool     `json:"AttachStdout"`
		AttachStderr bool     `json:"AttachStderr"`
		Tty          bool     `json:"Tty"`
		Cmd          []string `json:"Cmd"`
	}

	var execResp struct {
		ID string `json:"Id"`
	}

	createPayload := execConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
	}

	if err := d.client.Post(ctx, fmt.Sprintf("/containers/%s/exec", containerID), createPayload, &execResp); err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	execID := execResp.ID

	// Dial Docker daemon directly for raw hijacked stream
	conn, err := d.client.Dial(ctx)
	if err != nil {
		return fmt.Errorf("failed to dial docker socket: %w", err)
	}
	defer conn.Close()

	reqBody := `{"Detach":false,"Tty":true}`
	reqStr := fmt.Sprintf("POST /exec/%s/start HTTP/1.1\r\n"+
		"Host: docker\r\n"+
		"User-Agent: Docker-Client\r\n"+
		"Content-Type: application/json\r\n"+
		"Connection: Upgrade\r\n"+
		"Upgrade: tcp\r\n"+
		"Content-Length: %d\r\n\r\n%s", execID, len(reqBody), reqBody)

	if _, err := conn.Write([]byte(reqStr)); err != nil {
		return fmt.Errorf("failed to send exec start request: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "POST"})
	if err != nil {
		return fmt.Errorf("failed to read exec start response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exec start returned status %d: %s", resp.StatusCode, string(b))
	}

	// Handle PTY resize events in background
	resizeCtx, cancelResize := context.WithCancel(ctx)
	defer cancelResize()
	go func() {
		for {
			select {
			case <-resizeCtx.Done():
				return
			case size, ok := <-resizeChan:
				if !ok {
					return
				}
				if size.Rows > 0 && size.Cols > 0 {
					_ = d.client.Post(resizeCtx, fmt.Sprintf("/exec/%s/resize?h=%d&w=%d", execID, size.Rows, size.Cols), nil, nil)
				}
			}
		}
	}()

	// Probe initial output with short read deadline to detect immediate exit / missing executable
	initialBuf := make([]byte, 512)
	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Millisecond))
	n, readErr := br.Read(initialBuf)
	_ = conn.SetReadDeadline(time.Time{})

	if n > 0 {
		initialOutput := string(initialBuf[:n])
		lower := strings.ToLower(initialOutput)
		if strings.Contains(lower, "executable file not found") || strings.Contains(lower, "no such file") {
			return errors.New(strings.TrimSpace(initialOutput))
		}
		// Write valid initial output to terminal
		if _, err := out.Write(initialBuf[:n]); err != nil {
			return err
		}
	} else if readErr != nil && !errors.Is(readErr, os.ErrDeadlineExceeded) && !strings.Contains(readErr.Error(), "timeout") {
		// Connection closed immediately; inspect exit code
		var inspectResp struct {
			ExitCode int  `json:"ExitCode"`
			Running  bool `json:"Running"`
		}
		if d.client.Get(ctx, fmt.Sprintf("/exec/%s/json", execID), &inspectResp) == nil {
			if inspectResp.ExitCode == 126 || inspectResp.ExitCode == 127 {
				return fmt.Errorf("executable file not found (exit code %d)", inspectResp.ExitCode)
			}
		}
	}

	// Bidirectional stream copying
	done := make(chan struct{})

	// Stream stdout/stderr from container PTY to WebSocket
	go func() {
		_, _ = io.Copy(out, br)
		close(done)
	}()

	// Stream stdin from WebSocket to container PTY
	go func() {
		_, _ = io.Copy(conn, in)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (d *SocketDriver) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	var raw []struct {
		ID       string `json:"Id"`
		Name     string `json:"Name"`
		Driver   string `json:"Driver"`
		Scope    string `json:"Scope"`
		Internal bool   `json:"Internal"`
		IPAM     struct {
			Config []struct {
				Subnet  string `json:"Subnet"`
				Gateway string `json:"Gateway"`
			} `json:"Config"`
		} `json:"IPAM"`
		Containers map[string]struct {
			IPv4Address string `json:"IPv4Address"`
		} `json:"Containers"`
	}

	if err := d.client.Get(ctx, "/networks", &raw); err != nil {
		return nil, err
	}

	result := make([]NetworkInfo, 0, len(raw))
	for _, n := range raw {
		netInfo := NetworkInfo{
			ID:         n.ID,
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Internal:   n.Internal,
			Containers: make(map[string]string),
		}
		if len(n.IPAM.Config) > 0 {
			netInfo.Subnet = n.IPAM.Config[0].Subnet
			netInfo.Gateway = n.IPAM.Config[0].Gateway
		}
		for cID, c := range n.Containers {
			netInfo.Containers[cID] = c.IPv4Address
		}
		result = append(result, netInfo)
	}
	return result, nil
}

func (d *SocketDriver) CreateNetwork(ctx context.Context, name, driver string) error {
	if driver == "" {
		driver = "bridge"
	}
	body := map[string]interface{}{
		"Name":   name,
		"Driver": driver,
	}
	return d.client.Post(ctx, "/networks/create", body, nil)
}

func (d *SocketDriver) RemoveNetwork(ctx context.Context, id string) error {
	return d.client.Delete(ctx, "/networks/"+id)
}

func (d *SocketDriver) ConnectNetwork(ctx context.Context, netID, containerID string) error {
	body := map[string]interface{}{
		"Container": containerID,
	}
	return d.client.Post(ctx, fmt.Sprintf("/networks/%s/connect", netID), body, nil)
}

func (d *SocketDriver) DisconnectNetwork(ctx context.Context, netID, containerID string) error {
	body := map[string]interface{}{
		"Container": containerID,
		"Force":     false,
	}
	return d.client.Post(ctx, fmt.Sprintf("/networks/%s/disconnect", netID), body, nil)
}

func (d *SocketDriver) GetDiskUsage(ctx context.Context) (*DiskUsageInfo, error) {
	var raw struct {
		LayersSize int64 `json:"LayersSize"`
		Images     []struct {
			Size      int64    `json:"Size"`
			RepoTags  []string `json:"RepoTags"`
		} `json:"Images"`
		Containers []struct {
			SizeRw int64 `json:"SizeRw"`
		} `json:"Containers"`
		Volumes []struct {
			UsageData struct {
				Size     int64 `json:"Size"`
				RefCount int   `json:"RefCount"`
			} `json:"UsageData"`
		} `json:"Volumes"`
		BuildCache []struct {
			Size int64 `json:"Size"`
		} `json:"BuildCache"`
	}

	if err := d.client.Get(ctx, "/system/df", &raw); err != nil {
		return &DiskUsageInfo{}, nil
	}

	info := &DiskUsageInfo{
		LayersSize: raw.LayersSize,
	}

	for _, img := range raw.Images {
		info.ImagesSize += img.Size
		if len(img.RepoTags) == 0 || img.RepoTags[0] == "<none>:<none>" {
			info.DanglingImages++
		}
	}

	for _, c := range raw.Containers {
		info.ContainersSize += c.SizeRw
	}

	for _, v := range raw.Volumes {
		info.VolumesSize += v.UsageData.Size
		if v.UsageData.RefCount == 0 {
			info.UnusedVolumes++
		}
	}

	for _, b := range raw.BuildCache {
		info.BuildCacheSize += b.Size
	}

	return info, nil
}

func (d *SocketDriver) PruneResources(ctx context.Context, pruneAll bool) (*PruneReport, error) {
	report := &PruneReport{}

	var imgReport struct {
		ImagesDeleted []struct {
			Untagged string `json:"Untagged"`
			Deleted  string `json:"Deleted"`
		} `json:"ImagesDeleted"`
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	imgURL := "/images/prune?filters=%7B%22dangling%22%3A%5B%22true%22%5D%7D"
	if pruneAll {
		imgURL = "/images/prune"
	}
	if err := d.client.Post(ctx, imgURL, nil, &imgReport); err == nil {
		report.ImagesDeleted = len(imgReport.ImagesDeleted)
		report.SpaceReclaimed = imgReport.SpaceReclaimed
	}

	_ = d.client.Post(ctx, "/containers/prune", nil, nil)
	_ = d.client.Post(ctx, "/volumes/prune", nil, nil)

	return report, nil
}

func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = filepath.Clean(p)
	return strings.ReplaceAll(p, "\\", "/")
}

func (d *SocketDriver) getMounts(ctx context.Context) []hostMount {
	d.mountsMu.Lock()
	defer d.mountsMu.Unlock()

	if d.mountsInit {
		return d.mounts
	}
	d.mountsInit = true

	// 1. Check environment variable override
	if envHostBase := os.Getenv("HOST_BASE_DIR"); envHostBase != "" {
		envContainerBase := os.Getenv("CONTAINER_BASE_DIR")
		if envContainerBase == "" {
			envContainerBase = "/root/docker"
		}
		d.mounts = append(d.mounts, hostMount{
			Source:      normalizePath(envHostBase),
			Destination: normalizePath(envContainerBase),
		})
	}

	// 2. Query Docker inspect for self-container
	hostname, _ := os.Hostname()
	candidates := []string{
		hostname,
		os.Getenv("HOSTNAME"),
		"dockerpulse-agent",
		"dockerpulse",
	}

	type inspectResp struct {
		Mounts []struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}

	for _, cid := range candidates {
		if cid == "" {
			continue
		}
		var resp inspectResp
		if err := d.client.Get(ctx, "/containers/"+cid+"/json", &resp); err == nil && len(resp.Mounts) > 0 {
			for _, m := range resp.Mounts {
				if m.Type == "bind" && !strings.Contains(m.Source, "docker.sock") && !strings.Contains(m.Destination, "docker.sock") {
					normSrc := normalizePath(m.Source)
					normDst := normalizePath(m.Destination)
					exists := false
					for _, existing := range d.mounts {
						if existing.Destination == normDst {
							exists = true
							break
						}
					}
					if !exists {
						d.mounts = append(d.mounts, hostMount{
							Source:      normSrc,
							Destination: normDst,
						})
					}
				}
			}
			if len(d.mounts) > 0 {
				break
			}
		}
	}

	// 3. Fallback: check /containers/json for dockerpulse containers
	if len(d.mounts) == 0 {
		var containers []struct {
			Names  []string `json:"Names"`
			Image  string   `json:"Image"`
			Mounts []struct {
				Type        string `json:"Type"`
				Source      string `json:"Source"`
				Destination string `json:"Destination"`
			} `json:"Mounts"`
		}
		if err := d.client.Get(ctx, "/containers/json?all=1", &containers); err == nil {
			for _, c := range containers {
				isSelf := strings.Contains(strings.ToLower(c.Image), "dockerpulse")
				if !isSelf {
					for _, n := range c.Names {
						if strings.Contains(strings.ToLower(n), "dockerpulse") {
							isSelf = true
							break
						}
					}
				}
				if isSelf && len(c.Mounts) > 0 {
					for _, m := range c.Mounts {
						if m.Type == "bind" && !strings.Contains(m.Source, "docker.sock") && !strings.Contains(m.Destination, "docker.sock") {
							normSrc := normalizePath(m.Source)
							normDst := normalizePath(m.Destination)
							exists := false
							for _, existing := range d.mounts {
								if existing.Destination == normDst {
									exists = true
									break
								}
							}
							if !exists {
								d.mounts = append(d.mounts, hostMount{
									Source:      normSrc,
									Destination: normDst,
								})
							}
						}
					}
					if len(d.mounts) > 0 {
						break
					}
				}
			}
		}
	}

	return d.mounts
}

func (d *SocketDriver) ToHostPath(ctx context.Context, p string) string {
	if p == "" {
		return ""
	}
	normP := normalizePath(p)
	mounts := d.getMounts(ctx)

	// Check if normP already matches a host mount Source
	for _, m := range mounts {
		if normP == m.Source || strings.HasPrefix(normP, m.Source+"/") {
			return normP
		}
	}

	// Check if normP matches a container mount Destination
	for _, m := range mounts {
		if normP == m.Destination {
			return m.Source
		}
		if strings.HasPrefix(normP, m.Destination+"/") {
			rel := strings.TrimPrefix(normP, m.Destination+"/")
			return m.Source + "/" + rel
		}
	}

	return normP
}

func (d *SocketDriver) ToContainerPath(ctx context.Context, p string) string {
	if p == "" {
		return ""
	}
	expanded := expandHomeDir(p)
	normP := normalizePath(expanded)
	mounts := d.getMounts(ctx)

	// If normP already matches a container mount Destination, return it
	for _, m := range mounts {
		if normP == m.Destination || strings.HasPrefix(normP, m.Destination+"/") {
			return normP
		}
	}

	// If normP matches a host mount Source, convert to container Destination
	for _, m := range mounts {
		if normP == m.Source {
			return m.Destination
		}
		if strings.HasPrefix(normP, m.Source+"/") {
			rel := strings.TrimPrefix(normP, m.Source+"/")
			return m.Destination + "/" + rel
		}
	}

	return normP
}

// Stacks filesystem discovery and compose execution
func (d *SocketDriver) DiscoverStacks(ctx context.Context, baseDir string) ([]DiscoveredStack, error) {
	containerBase := d.ToContainerPath(ctx, baseDir)
	entries, err := os.ReadDir(containerBase)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []DiscoveredStack{}, nil
		}
		return nil, err
	}

	var stacks []DiscoveredStack
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		containerDirPath := filepath.Join(containerBase, entry.Name())
		composeFile := findComposeFile(containerDirPath)
		if composeFile != "" {
			info, _ := entry.Info()
			hasEnv := false
			if _, err := os.Stat(filepath.Join(containerDirPath, ".env")); err == nil {
				hasEnv = true
			}

			hostDirPath := d.ToHostPath(ctx, containerDirPath)
			stackName := getComposeProjectName(composeFile, hostDirPath)

			stacks = append(stacks, DiscoveredStack{
				Name:        stackName,
				Path:        hostDirPath,
				ComposeFile: filepath.Base(composeFile),
				HasEnvFile:  hasEnv,
				UpdatedAt:   info.ModTime(),
			})
		}
	}
	return stacks, nil
}

func (d *SocketDriver) ReadStackFiles(ctx context.Context, stackPath string) (string, string, error) {
	containerPath := d.ToContainerPath(ctx, stackPath)
	composeFile := findComposeFile(containerPath)
	if composeFile == "" {
		return "", "", fmt.Errorf("no compose file found in %s", stackPath)
	}

	composeBytes, err := os.ReadFile(composeFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read compose file: %w", err)
	}

	envBytes, _ := os.ReadFile(filepath.Join(containerPath, ".env"))
	return string(composeBytes), string(envBytes), nil
}

func (d *SocketDriver) WriteStackFiles(ctx context.Context, stackPath string, composeContent string, envContent string) error {
	containerPath := d.ToContainerPath(ctx, stackPath)
	composeFile := findComposeFile(containerPath)
	if composeFile == "" {
		composeFile = filepath.Join(containerPath, "docker-compose.yml")
	}

	if err := os.MkdirAll(containerPath, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(composeFile, []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	if envContent != "" || fileExists(filepath.Join(containerPath, ".env")) {
		if err := os.WriteFile(filepath.Join(containerPath, ".env"), []byte(envContent), 0644); err != nil {
			return fmt.Errorf("failed to write .env file: %w", err)
		}
	}

	return nil
}

func isSelfUpdate(containerPath string) bool {
	composeFile := findComposeFile(containerPath)
	if composeFile != "" {
		content, err := os.ReadFile(composeFile)
		if err == nil && strings.Contains(strings.ToLower(string(content)), "dockerpulse") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(containerPath), "dockerpulse")
}

func (d *SocketDriver) ExecuteCompose(ctx context.Context, stackPath string, action string, writer io.Writer) error {
	containerPath := d.ToContainerPath(ctx, stackPath)
	hostPath := d.ToHostPath(ctx, stackPath)
	composeFile := findComposeFile(containerPath)
	if composeFile == "" {
		return fmt.Errorf("no compose file found in %s", stackPath)
	}

	envFile := filepath.Join(containerPath, ".env")

	// Detect top-level 'name:' issues and notify user, without automatically modifying user files
	if hasIssue, oldName, proposedName := detectComposeNameIssue(composeFile); hasIssue {
		fmt.Fprintf(writer, "[DockPulse] WARNING: Top-level 'name: %s' in %s violates Docker Compose v2 naming rules (pattern '^[a-z0-9][a-z0-9_-]*$').\n", oldName, filepath.Base(composeFile))
		fmt.Fprintf(writer, "[DockPulse] Suggested fix: 'name: %s'. You can apply this fix or edit the file in DockerPulse.\n", proposedName)
	}

	baseArgs := []string{"compose", "-f", composeFile}
	if fileExists(envFile) {
		baseArgs = append(baseArgs, "--env-file", envFile)
	}
	if hostPath != "" {
		baseArgs = append(baseArgs, "--project-directory", hostPath)
	}

	switch action {
	case "up", "restart":
		if isSelfUpdate(containerPath) {
			fmt.Fprintf(writer, "[DockPulse] Detected self-update/restart of DockerPulse at %s\n", hostPath)
			fmt.Fprintln(writer, "[DockPulse] Spawning detached helper runner to safely restart container...")

			selfRef, _ := os.Hostname()
			if selfRef == "" {
				selfRef = "dockerpulse-agent"
			}

			composeSubCmd := "up -d"
			if action == "restart" {
				composeSubCmd = "restart"
			}

			helperImage := "ghcr.io/farmers00/dockerpulse:latest"
			var detail struct {
				Config struct {
					Image string `json:"Image"`
				} `json:"Config"`
			}
			if err := d.client.Get(ctx, "/containers/"+selfRef+"/json", &detail); err == nil && detail.Config.Image != "" {
				helperImage = detail.Config.Image
			}

			userEnv := parseEnvFile(envFile)

			var helperCmdParts []string
			helperCmdParts = append(helperCmdParts, "docker", "compose", "-f", fmt.Sprintf("%q", filepath.ToSlash(composeFile)))
			if fileExists(envFile) {
				helperCmdParts = append(helperCmdParts, "--env-file", fmt.Sprintf("%q", filepath.ToSlash(envFile)))
			}
			if hostPath != "" {
				helperCmdParts = append(helperCmdParts, "--project-directory", fmt.Sprintf("%q", filepath.ToSlash(hostPath)))
			}
			helperCmdParts = append(helperCmdParts, composeSubCmd)

			composeExec := strings.Join(helperCmdParts, " ")

			var scriptParts []string
			scriptParts = append(scriptParts, "unset PORT DATA_DIR JWT_SECRET AGENT_SECRET HOST_BASE_DIR CONTAINER_BASE_DIR PROXY_AUTH_HEADER PROXY_EMAIL_HEADER UPDATE_INTERVAL_MINUTES")
			for k, v := range userEnv {
				scriptParts = append(scriptParts, fmt.Sprintf("export %s=%s", k, shellQuote(v)))
			}
			scriptParts = append(scriptParts, "sleep 2")
			scriptParts = append(scriptParts, composeExec)

			cmdScript := strings.Join(scriptParts, " && ")
			fmt.Fprintf(writer, "[DockPulse] Helper command: %s\n", composeExec)
			if len(userEnv) > 0 {
				var exportedKeys []string
				for k := range userEnv {
					exportedKeys = append(exportedKeys, k)
				}
				fmt.Fprintf(writer, "[DockPulse] Exported environment from .env: %s\n", strings.Join(exportedKeys, ", "))
			}

			// Ensure any previous helper container with this name is removed first
			_ = exec.Command("docker", "rm", "-f", "dockerpulse-updater-helper").Run()

			runArgs := []string{
				"run", "--rm", "-d",
				"--name", "dockerpulse-updater-helper",
				"--label", "com.dockerpulse.helper=true",
				"--entrypoint", "sh",
				"-v", "/var/run/docker.sock:/var/run/docker.sock",
				"--volumes-from", selfRef,
				"-w", containerPath,
			}
			if _, hasPort := userEnv["PORT"]; !hasPort {
				runArgs = append(runArgs, "-e", "PORT=")
			}
			for k, v := range userEnv {
				runArgs = append(runArgs, "-e", fmt.Sprintf("%s=%s", k, v))
			}
			runArgs = append(runArgs, helperImage, "-c", cmdScript)

			runnerCmd := exec.Command("docker", runArgs...)

			out, err := runnerCmd.CombinedOutput()
			if err != nil {
				_ = exec.Command("docker", "rm", "-f", "dockerpulse-updater-helper").Run()
				if selfRef != "dockerpulse-agent" {
					runArgs2 := make([]string, len(runArgs))
					copy(runArgs2, runArgs)
					for i, arg := range runArgs2 {
						if arg == "--volumes-from" && i+1 < len(runArgs2) {
							runArgs2[i+1] = "dockerpulse-agent"
							break
						}
					}
					runnerCmd2 := exec.Command("docker", runArgs2...)
					if out2, err2 := runnerCmd2.CombinedOutput(); err2 == nil {
						out = out2
						err = nil
					} else {
						_ = exec.Command("docker", "rm", "-f", "dockerpulse-updater-helper").Run()
					}
				}
			}

			if err == nil {
				cid := strings.TrimSpace(string(out))
				if len(cid) > 12 {
					cid = cid[:12]
				}
				fmt.Fprintf(writer, "[DockPulse] Detached runner container launched (ID: %s).\n", cid)
				fmt.Fprintf(writer, "[DockPulse] Container will %s in 2 seconds with updated image.\n", action)
				fmt.Fprintln(writer, "[DockPulse] Command completed successfully.")
				return nil
			}

			fmt.Fprintf(writer, "[DockPulse] Detached runner failed (%v: %s), falling back to direct compose\n", err, strings.TrimSpace(string(out)))
		}

		args := append([]string{}, baseArgs...)
		if action == "restart" {
			args = append(args, "restart")
		} else {
			args = append(args, "up", "-d")
		}
		return d.runComposeCmd(ctx, containerPath, args, writer)

	case "down":
		args := append(append([]string{}, baseArgs...), "down")
		return d.runComposeCmd(ctx, containerPath, args, writer)

	case "pull":
		args := append(append([]string{}, baseArgs...), "pull")
		return d.runComposeCmd(ctx, containerPath, args, writer)

	case "pull_up":
		if err := d.ExecuteCompose(ctx, stackPath, "pull", writer); err != nil {
			return err
		}
		return d.ExecuteCompose(ctx, stackPath, "up", writer)

	default:
		return fmt.Errorf("unsupported compose action: %s", action)
	}
}

var dockerPulseInternalVars = map[string]bool{
	"PORT":                    true,
	"DATA_DIR":                true,
	"JWT_SECRET":              true,
	"AGENT_SECRET":            true,
	"HOST_BASE_DIR":           true,
	"CONTAINER_BASE_DIR":      true,
	"PROXY_AUTH_HEADER":       true,
	"PROXY_EMAIL_HEADER":      true,
	"UPDATE_INTERVAL_MINUTES": true,
}

func parseEnvFile(p string) map[string]string {
	res := make(map[string]string)
	data, err := os.ReadFile(p)
	if err != nil {
		return res
	}

	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") && len(val) >= 2) {
			val = val[1 : len(val)-1]
		} else {
			if commentIdx := strings.Index(val, " #"); commentIdx != -1 {
				val = strings.TrimSpace(val[:commentIdx])
			}
		}

		if key != "" {
			res[key] = val
		}
	}
	return res
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func buildComposeEnv(workingDir string) []string {
	envFile := filepath.Join(workingDir, ".env")
	userEnv := parseEnvFile(envFile)

	var result []string
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			k := parts[0]
			if dockerPulseInternalVars[k] {
				if _, ok := userEnv[k]; !ok {
					continue
				}
			}
			if _, ok := userEnv[k]; ok {
				continue
			}
			result = append(result, env)
		}
	}

	for k, v := range userEnv {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}

	return result
}

func (d *SocketDriver) runComposeCmd(ctx context.Context, workingDir string, args []string, writer io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = workingDir
	cmd.Env = buildComposeEnv(workingDir)
	cmd.Stdout = writer
	cmd.Stderr = writer

	fmt.Fprintf(writer, "[DockPulse] Running: docker %s\n", strings.Join(args, " "))
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(writer, "[DockPulse] Command finished with error: %v\n", err)
	} else {
		fmt.Fprintln(writer, "[DockPulse] Command completed successfully.")
	}
	return err
}

func (d *SocketDriver) Close() error {
	return nil
}

func findComposeFile(dir string) string {
	candidates := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}
	for _, c := range candidates {
		p := filepath.Join(dir, c)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func expandHomeDir(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

func cleanDigest(d string) string {
	d = strings.TrimSpace(d)
	d = strings.Trim(d, "\"")
	if idx := strings.LastIndex(d, "sha256:"); idx != -1 {
		return d[idx+7:]
	}
	return strings.TrimPrefix(d, "sha256:")
}

var (
	validComposeNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	topLevelNameRegex     = regexp.MustCompile(`(?m)^name\s*:\s*(.+)$`)
)

func sanitizeComposeName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	raw = strings.ToLower(raw)

	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' {
			if b.Len() > 0 {
				last := b.String()[b.Len()-1]
				if last != '-' && last != '_' {
					b.WriteByte('-')
				}
			}
		}
	}
	res := strings.Trim(b.String(), "-_")
	if res == "" || !((res[0] >= 'a' && res[0] <= 'z') || (res[0] >= '0' && res[0] <= '9')) {
		return "stack"
	}
	return res
}

func getComposeProjectName(composeFile string, hostPath string) string {
	if composeFile != "" {
		if data, err := os.ReadFile(composeFile); err == nil {
			loc := topLevelNameRegex.FindStringSubmatchIndex(string(data))
			if len(loc) >= 4 {
				valWithComment := string(data)[loc[2]:loc[3]]
				val := strings.TrimSpace(strings.Split(valWithComment, "#")[0])
				val = strings.Trim(val, `"'`)
				if val != "" {
					if validComposeNameRegex.MatchString(val) {
						return val
					}
					sanitized := sanitizeComposeName(val)
					if sanitized != "" {
						return sanitized
					}
				}
			}
		}
	}
	return sanitizeComposeName(filepath.Base(hostPath))
}

func sanitizeComposeContent(content string) (newContent string, modified bool, oldName string, newName string) {
	loc := topLevelNameRegex.FindStringSubmatchIndex(content)
	if len(loc) < 4 {
		return content, false, "", ""
	}

	valWithComment := content[loc[2]:loc[3]]
	val := strings.TrimSpace(strings.Split(valWithComment, "#")[0])
	val = strings.Trim(val, `"'`)

	if validComposeNameRegex.MatchString(val) {
		return content, false, "", ""
	}

	sanitized := sanitizeComposeName(val)
	fullMatch := content[loc[0]:loc[1]]
	lineEnding := ""
	if strings.HasSuffix(fullMatch, "\r") {
		lineEnding = "\r"
	}
	comment := ""
	if hashIdx := strings.Index(valWithComment, "#"); hashIdx != -1 {
		comment = " " + strings.TrimSpace(valWithComment[hashIdx:])
	}
	newLine := "name: " + sanitized + comment + lineEnding
	newContent = content[:loc[0]] + newLine + content[loc[1]:]
	return newContent, true, val, sanitized
}

func detectComposeNameIssue(filePath string) (hasIssue bool, oldName string, proposedName string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, "", ""
	}
	_, hasIssue, oldName, proposedName = sanitizeComposeContent(string(data))
	return hasIssue, oldName, proposedName
}

