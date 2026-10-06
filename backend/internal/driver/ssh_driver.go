package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHDriver struct {
	client *ssh.Client
	host   string
	port   int
	user   string
	key    string
	mu     sync.Mutex
}

func NewSSHDriver(address string, port int, user string, privateKey string) (*SSHDriver, error) {
	if port == 0 {
		port = 22
	}
	if user == "" {
		user = "root"
	}

	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // User managed homelab hosts
		Timeout:         10 * time.Second,
	}

	target := fmt.Sprintf("%s:%d", address, port)
	conn, err := ssh.Dial("tcp", target, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial failed: %w", err)
	}

	return &SSHDriver{
		client: conn,
		host:   address,
		port:   port,
		user:   user,
		key:    privateKey,
	}, nil
}

func (s *SSHDriver) runCommand(cmdStr string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmdStr)
	if err != nil {
		return "", fmt.Errorf("exec error (%v): %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func (s *SSHDriver) Ping(ctx context.Context) error {
	_, err := s.runCommand("docker info --format '{{.ServerVersion}}'")
	return err
}

func (s *SSHDriver) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	out, err := s.runCommand(`docker info --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}

	var dInfo struct {
		Name            string `json:"Name"`
		OperatingSystem string `json:"OperatingSystem"`
		KernelVersion   string `json:"KernelVersion"`
		ServerVersion   string `json:"ServerVersion"`
		NCPU            int    `json:"NCPU"`
		MemTotal        int64  `json:"MemTotal"`
	}
	_ = json.Unmarshal([]byte(out), &dInfo)

	return &SystemInfo{
		Hostname:      dInfo.Name,
		OS:            dInfo.OperatingSystem,
		KernelVersion: dInfo.KernelVersion,
		DockerVersion: dInfo.ServerVersion,
		TotalCPUs:     dInfo.NCPU,
		TotalRAMBytes: uint64(dInfo.MemTotal),
	}, nil
}

func (s *SSHDriver) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	out, err := s.runCommand(`docker ps -a --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var result []ContainerInfo

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw struct {
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			Command string `json:"Command"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Ports   string `json:"Ports"`
			Labels  string `json:"Labels"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		info := ContainerInfo{
			ID:      raw.ID,
			Names:   []string{raw.Names},
			Image:   raw.Image,
			Command: raw.Command,
			State:   raw.State,
			Status:  raw.Status,
		}

		// Parse labels
		for _, l := range strings.Split(raw.Labels, ",") {
			parts := strings.SplitN(l, "=", 2)
			if len(parts) == 2 {
				switch parts[0] {
				case "com.docker.compose.project":
					info.Stack = parts[1]
				case "com.docker.compose.service":
					info.Service = parts[1]
				case "com.docker.compose.project.working_dir":
					info.WorkingDir = parts[1]
				}
			}
		}

		result = append(result, info)
	}

	// Fetch live resource usage for running containers via docker stats
	hasRunning := false
	for _, r := range result {
		if r.State == "running" {
			hasRunning = true
			break
		}
	}

	if hasRunning {
		statsOut, err := s.runCommand(`docker stats --no-stream --format '{{json .}}'`)
		if err == nil && strings.TrimSpace(statsOut) != "" {
			type rawStat struct {
				ID       string `json:"ID"`
				Name     string `json:"Name"`
				CPUPerc  string `json:"CPUPerc"`
				MemUsage string `json:"MemUsage"`
				MemPerc  string `json:"MemPerc"`
				NetIO    string `json:"NetIO"`
			}
			statsMap := make(map[string]rawStat)
			for _, sline := range strings.Split(strings.TrimSpace(statsOut), "\n") {
				var st rawStat
				if err := json.Unmarshal([]byte(sline), &st); err == nil {
					if st.ID != "" {
						statsMap[st.ID] = st
					}
					if st.Name != "" {
						statsMap[st.Name] = st
					}
				}
			}

			for i := range result {
				if result[i].State != "running" {
					continue
				}
				var matched *rawStat
				if st, ok := statsMap[result[i].ID]; ok {
					matched = &st
				} else if len(result[i].ID) >= 12 {
					if st, ok := statsMap[result[i].ID[:12]]; ok {
						matched = &st
					}
				}
				if matched == nil && len(result[i].Names) > 0 {
					cleanName := strings.TrimPrefix(result[i].Names[0], "/")
					if st, ok := statsMap[cleanName]; ok {
						matched = &st
					}
				}

				if matched != nil {
					// CPU %
					cpuStr := strings.TrimSuffix(strings.TrimSpace(matched.CPUPerc), "%")
					if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
						result[i].CPUPct = math.Round(cpu*10) / 10
					}

					// Mem %
					memPercStr := strings.TrimSuffix(strings.TrimSpace(matched.MemPerc), "%")
					if mp, err := strconv.ParseFloat(memPercStr, 64); err == nil {
						result[i].MemoryPct = math.Round(mp*10) / 10
					}

					// Mem Usage: "667MiB / 31.25GiB"
					if parts := strings.Split(matched.MemUsage, "/"); len(parts) >= 1 {
						result[i].MemoryMB = parseHumanMB(parts[0])
					}

					// Net I/O: "15.4MB / 120.3MB"
					if parts := strings.Split(matched.NetIO, "/"); len(parts) >= 2 {
						result[i].NetInputMB = parseHumanMB(parts[0])
						result[i].NetOutputMB = parseHumanMB(parts[1])
					}
				}
			}
		}
	}

	return result, nil
}

func parseHumanMB(val string) float64 {
	val = strings.TrimSpace(val)
	if val == "" || val == "--" || val == "0B" {
		return 0
	}
	idx := 0
	for idx < len(val) && ((val[idx] >= '0' && val[idx] <= '9') || val[idx] == '.') {
		idx++
	}
	if idx == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(val[:idx], 64)
	if err != nil {
		return 0
	}
	unit := strings.TrimSpace(strings.ToLower(val[idx:]))
	switch unit {
	case "gib", "gb":
		return math.Round(num*1024*10) / 10
	case "mib", "mb":
		return math.Round(num*10) / 10
	case "kib", "kb":
		return math.Round((num/1024)*10) / 10
	case "b":
		return math.Round((num/(1024*1024))*10) / 10
	default:
		return math.Round(num*10) / 10
	}
}

func (s *SSHDriver) StartContainer(ctx context.Context, id string) error {
	_, err := s.runCommand(fmt.Sprintf("docker start %s", id))
	return err
}

func (s *SSHDriver) StopContainer(ctx context.Context, id string) error {
	_, err := s.runCommand(fmt.Sprintf("docker stop %s", id))
	return err
}

func (s *SSHDriver) RestartContainer(ctx context.Context, id string) error {
	_, err := s.runCommand(fmt.Sprintf("docker restart %s", id))
	return err
}

func (s *SSHDriver) RemoveContainer(ctx context.Context, id string, force bool) error {
	fFlag := ""
	if force {
		fFlag = "-f"
	}
	_, err := s.runCommand(fmt.Sprintf("docker rm %s %s", fFlag, id))
	return err
}

func (s *SSHDriver) StreamLogs(ctx context.Context, containerID string, follow bool, tail string, writer io.Writer) error {
	session, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	if tail == "" {
		tail = "200"
	}
	followFlag := ""
	if follow {
		followFlag = "-f"
	}

	session.Stdout = writer
	session.Stderr = writer

	cmd := fmt.Sprintf("docker logs --tail %s -t %s %s", tail, followFlag, containerID)
	return session.Run(cmd)
}

func (s *SSHDriver) ExecShell(ctx context.Context, containerID string, cmd []string, in io.Reader, out io.Writer, resizeChan <-chan TerminalSize) error {
	session, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm", 30, 100, modes); err != nil {
		return err
	}

	session.Stdin = in
	session.Stdout = out
	session.Stderr = out

	shellCmd := "/bin/sh"
	if len(cmd) > 0 {
		shellCmd = cmd[0]
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case size, ok := <-resizeChan:
				if !ok {
					return
				}
				_ = session.WindowChange(int(size.Rows), int(size.Cols))
			}
		}
	}()

	return session.Run(fmt.Sprintf("docker exec -it %s %s", containerID, shellCmd))
}

func (s *SSHDriver) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	out, err := s.runCommand(`docker network ls --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}

	var list []NetworkInfo
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var raw struct {
			ID     string `json:"ID"`
			Name   string `json:"Name"`
			Driver string `json:"Driver"`
			Scope  string `json:"Scope"`
		}
		if err := json.Unmarshal([]byte(l), &raw); err == nil {
			list = append(list, NetworkInfo{
				ID:         raw.ID,
				Name:       raw.Name,
				Driver:     raw.Driver,
				Scope:      raw.Scope,
				Containers: make(map[string]string),
			})
		}
	}
	return list, nil
}

func (s *SSHDriver) CreateNetwork(ctx context.Context, name, driver string) error {
	if driver == "" {
		driver = "bridge"
	}
	_, err := s.runCommand(fmt.Sprintf("docker network create --driver %s %s", driver, name))
	return err
}

func (s *SSHDriver) RemoveNetwork(ctx context.Context, id string) error {
	_, err := s.runCommand(fmt.Sprintf("docker network rm %s", id))
	return err
}

func (s *SSHDriver) ConnectNetwork(ctx context.Context, netID, containerID string) error {
	_, err := s.runCommand(fmt.Sprintf("docker network connect %s %s", netID, containerID))
	return err
}

func (s *SSHDriver) DisconnectNetwork(ctx context.Context, netID, containerID string) error {
	_, err := s.runCommand(fmt.Sprintf("docker network disconnect %s %s", netID, containerID))
	return err
}

func (s *SSHDriver) GetDiskUsage(ctx context.Context) (*DiskUsageInfo, error) {
	out, err := s.runCommand("docker system df --format '{{json .}}'")
	if err != nil {
		return &DiskUsageInfo{}, nil
	}
	_ = out
	return &DiskUsageInfo{}, nil
}

func (s *SSHDriver) PruneResources(ctx context.Context, pruneAll bool) (*PruneReport, error) {
	allFlag := ""
	if pruneAll {
		allFlag = "-a"
	}
	_, err := s.runCommand(fmt.Sprintf("docker system prune -f %s", allFlag))
	return &PruneReport{}, err
}

func (s *SSHDriver) DiscoverStacks(ctx context.Context, baseDir string) ([]DiscoveredStack, error) {
	// Look for compose files under baseDir
	findCmd := fmt.Sprintf(`find %s -maxdepth 2 -type f \( -name "docker-compose.yml" -o -name "docker-compose.yaml" -o -name "compose.yml" -o -name "compose.yaml" \) 2>/dev/null`, baseDir)
	out, err := s.runCommand(findCmd)
	if err != nil {
		return []DiscoveredStack{}, nil
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var stacks []DiscoveredStack
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			dir := strings.Join(parts[:len(parts)-1], "/")
			stackName := parts[len(parts)-2]
			composeFile := parts[len(parts)-1]

			stacks = append(stacks, DiscoveredStack{
				Name:        stackName,
				Path:        dir,
				ComposeFile: composeFile,
			})
		}
	}
	return stacks, nil
}

func (s *SSHDriver) ReadStackFiles(ctx context.Context, stackPath string) (string, string, error) {
	composeContent, err := s.runCommand(fmt.Sprintf(`for f in "%s/docker-compose.yml" "%s/compose.yaml" "%s/docker-compose.yaml" "%s/compose.yml"; do if [ -f "$f" ]; then cat "$f"; break; fi; done`, stackPath, stackPath, stackPath, stackPath))
	if err != nil {
		return "", "", err
	}

	envContent, _ := s.runCommand(fmt.Sprintf(`if [ -f "%s/.env" ]; then cat "%s/.env"; fi`, stackPath, stackPath))
	return composeContent, envContent, nil
}

func (s *SSHDriver) WriteStackFiles(ctx context.Context, stackPath string, composeContent string, envContent string) error {
	mkdirCmd := fmt.Sprintf(`mkdir -p "%s"`, stackPath)
	if _, err := s.runCommand(mkdirCmd); err != nil {
		return err
	}

	// Safe write using base64 decoding on remote host to prevent escaping bugs
	writeScript := fmt.Sprintf(`cat <<'EOF' > "%s/docker-compose.yml"
%s
EOF`, stackPath, composeContent)
	if _, err := s.runCommand(writeScript); err != nil {
		return err
	}

	if envContent != "" {
		envScript := fmt.Sprintf(`cat <<'EOF' > "%s/.env"
%s
EOF`, stackPath, envContent)
		if _, err := s.runCommand(envScript); err != nil {
			return err
		}
	}
	return nil
}

func (s *SSHDriver) ExecuteCompose(ctx context.Context, stackPath string, action string, writer io.Writer) error {
	session, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdout = writer
	session.Stderr = writer

	var cmd string
	switch action {
	case "up":
		cmd = fmt.Sprintf(`cd "%s" && docker compose up -d`, stackPath)
	case "down":
		cmd = fmt.Sprintf(`cd "%s" && docker compose down`, stackPath)
	case "pull":
		cmd = fmt.Sprintf(`cd "%s" && docker compose pull`, stackPath)
	case "restart":
		cmd = fmt.Sprintf(`cd "%s" && docker compose restart`, stackPath)
	case "pull_up":
		cmd = fmt.Sprintf(`cd "%s" && docker compose pull && docker compose up -d`, stackPath)
	default:
		return fmt.Errorf("unknown compose action: %s", action)
	}

	fmt.Fprintf(writer, "[DockPulse SSH] Executing: %s\n", cmd)
	return session.Run(cmd)
}

func (s *SSHDriver) Close() error {
	return s.client.Close()
}
