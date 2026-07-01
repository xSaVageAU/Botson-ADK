package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OCIConfig represents the structure of an OCI compliant config.json file
type OCIConfig struct {
	OCIVersion string    `json:"ociVersion"`
	Process    Process   `json:"process"`
	Root       Root      `json:"root"`
	Hostname   string    `json:"hostname"`
	Mounts     []Mount   `json:"mounts"`
	Linux      LinuxSpec `json:"linux"`
}

type Process struct {
	Terminal        bool            `json:"terminal"`
	User            User            `json:"user"`
	Args            []string        `json:"args"`
	Env             []string        `json:"env"`
	Cwd             string          `json:"cwd"`
	Capabilities    Capabilities    `json:"capabilities,omitempty"`
	Rlimits         []Rlimit        `json:"rlimits,omitempty"`
	NoNewPrivileges bool            `json:"noNewPrivileges"`
}

type User struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}

type Capabilities struct {
	Bounding    []string `json:"bounding,omitempty"`
	Effective   []string `json:"effective,omitempty"`
	Inheritable []string `json:"inheritable,omitempty"`
	Permitted   []string `json:"permitted,omitempty"`
}

type Rlimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

type Root struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly"`
}

type Mount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Source      string   `json:"source"`
	Options     []string `json:"options,omitempty"`
}

type LinuxSpec struct {
	Namespaces []Namespace `json:"namespaces,omitempty"`
	Resources  *Resources  `json:"resources,omitempty"`
}

type Namespace struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

type Resources struct {
	Devices []DeviceRule `json:"devices,omitempty"`
}

type DeviceRule struct {
	Allow  bool   `json:"allow"`
	Access string `json:"access,omitempty"`
}

// DefaultOCIConfig generates a safe, standard OCI specification for gVisor
func DefaultOCIConfig(args []string, isTerminal bool) OCIConfig {
	return OCIConfig{
		OCIVersion: "1.0.2",
		Hostname:   "gvisor-sandbox",
		Root: Root{
			Path:     "rootfs",
			Readonly: false, // Allows writing in tmp and user directories inside the sandbox
		},
		Process: Process{
			Terminal: isTerminal,
			User: User{
				UID: 0,
				GID: 0,
			},
			Args: args,
			Env: []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"TERM=xterm-256color",
				"HOME=/root",
				"LANG=en_US.UTF-8",
				"ENV=/etc/profile",
				"PS1=\033[1;36m🛡️  gvis-sandbox:\033[1;34m\\w\033[0m# ",
				"FORCE_COLOR=1",
				"CLICOLOR=1",
			},
			Cwd: "/",
			Capabilities: Capabilities{
				Bounding: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
				Effective: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
				Inheritable: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
				Permitted: []string{
					"CAP_AUDIT_WRITE",
					"CAP_KILL",
					"CAP_NET_BIND_SERVICE",
				},
			},
			Rlimits: []Rlimit{
				{
					Type: "RLIMIT_NOFILE",
					Hard: 1024,
					Soft: 1024,
				},
			},
			NoNewPrivileges: true,
		},
		Mounts: []Mount{
			{
				Destination: "/proc",
				Type:        "proc",
				Source:      "proc",
			},
			{
				Destination: "/dev",
				Type:        "tmpfs",
				Source:      "tmpfs",
				Options: []string{
					"nosuid",
					"strictatime",
					"mode=755",
					"size=65536k",
				},
			},
			{
				Destination: "/sys",
				Type:        "sysfs",
				Source:      "sysfs",
				Options: []string{
					"nosuid",
					"noexec",
					"nodev",
					"ro",
				},
			},
			{
				Destination: "/tmp",
				Type:        "tmpfs",
				Source:      "tmpfs",
				Options: []string{
					"nosuid",
					"nodev",
					"mode=1777",
					"size=131072k",
				},
			},
		},
		Linux: LinuxSpec{
			Namespaces: []Namespace{
				{Type: "pid"},
				{Type: "network"}, // isolated network unless host network configured
				{Type: "ipc"},
				{Type: "uts"},
				{Type: "mount"},
			},
			Resources: &Resources{
				Devices: []DeviceRule{
					{
						Allow:  false,
						Access: "rwm",
					},
				},
			},
		},
	}
}

// WriteConfig writes the OCI configuration to the specified bundle path
func WriteConfig(bundlePath string, config OCIConfig) error {
	configPath := filepath.Join(bundlePath, "config.json")
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(config)
}
