package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type SSHConfig struct {
	HostName          string `yaml:"HostName"`
	User              string `yaml:"User"`
	IdentityFile      string `yaml:"IndentityFile"`
	Port              int    `yaml:"Port"`
	KeepaliveInterval int    `yaml:"KeepaliveInterval"`
	Timeout           int    `yaml:"Timeout"`
}

func (c *SSHConfig) getServerAddr() string {
	return fmt.Sprintf("%s:%d", c.HostName, c.Port)
}

func (c *SSHConfig) getKeyPath() string {
	if len(c.IdentityFile) != 0 {
		return c.IdentityFile
	}

	sshDir := fmt.Sprintf("%s/.ssh", os.Getenv("HOME"))

	ecdsaPath := fmt.Sprintf("%s/id_ecdsa", sshDir)
	if _, err := os.Stat(ecdsaPath); err == nil {
		return ecdsaPath
	}

	rsaPath := fmt.Sprintf("%s/id_rsa", sshDir)
	if _, err := os.Stat(rsaPath); err == nil {
		return rsaPath
	}

	zap.S().Fatalf("SSH key file not found.")

	return ""
}

type KushiConfig struct {
	BindingConfigsURL string    `yaml:"BindingConfigsURL"`
	CheckInterval     int       `yaml:"CheckInterval"`
	SSHConfig         SSHConfig `yaml:"SSHConfig"`
}

func detectConfigDir() string {
	path := fmt.Sprintf("%s/.config/kushi", os.Getenv("HOME"))

	if _, err := os.Stat(path); err != nil {
		err = os.MkdirAll(path, 0755)
		if err != nil {
			zap.S().Fatal(err)
		}
	}

	return path
}

func detectConfigPath(p string) string {
	if len(p) != 0 {
		return p
	}

	dirPath := detectConfigDir()
	return fmt.Sprintf("%s/config.yml", dirPath)
}

var (
	defaultCheckInterval     = 600
	defaultKeepaliveInterval = 3
	defaultTimeout           = 5
	defaultSSHPort           = 22
)

func LoadKushiConfigs(configPath string) (c *KushiConfig) {

	bytes, err := ioutil.ReadFile(detectConfigPath(configPath))
	if err != nil {
		zap.S().Fatal(err)
		return
	}

	err = yaml.Unmarshal(bytes, &c)
	if err != nil {
		zap.S().Fatal(err)
		return
	}

	if c.CheckInterval == 0 {
		c.CheckInterval = defaultCheckInterval
	}

	if c.SSHConfig.Port == 0 {
		c.SSHConfig.Port = defaultSSHPort
	}

	if c.SSHConfig.KeepaliveInterval == 0 {
		c.SSHConfig.KeepaliveInterval = defaultKeepaliveInterval
	}

	if c.SSHConfig.Timeout == 0 {
		c.SSHConfig.Timeout = defaultTimeout
	}

	return
}
