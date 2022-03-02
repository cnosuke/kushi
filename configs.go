package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	sshAgent "golang.org/x/crypto/ssh/agent"
	yaml "gopkg.in/yaml.v2"
)

type SSHConfig struct {
	HostName          string `yaml:"HostName"`
	User              string `yaml:"User"`
	IdentityFile      string `yaml:"IdentityFile"`
	IdentityAgent     string `yaml:"IdentityAgent"`
	Port              int    `yaml:"Port"`
	KeepaliveInterval int    `yaml:"KeepaliveInterval"`
	Timeout           int    `yaml:"Timeout"`
	passphrase        string
}

func (c *SSHConfig) getServerAddr() string {
	return fmt.Sprintf("%s:%d", c.HostName, c.Port)
}

func (c *SSHConfig) getImplicitKeyPaths() (candidates []string, err error) {
	// use os.UserHomeDir() instead of os.Getenv() for better portability
	homeDir, err := os.UserHomeDir()
	if err != nil {
		err = fmt.Errorf("could not retrieve the current user's home directory: %w", err)
		return
	}

	sshDir := filepath.Join(homeDir, ".ssh")

	for _, privKeyFileName := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(sshDir, privKeyFileName)
		if _, err := os.Stat(path); err == nil {
			candidates = append(candidates, path)
		}
	}

	return
}

func checkEligibilityAsAgentSocket(path string, specifier string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s, specified by %s is not accessible: %w", path, specifier, err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s, specified by %s is not an unix domain socket", specifier, path)
	}
	return nil
}

func (c *SSHConfig) getAgentSocketPath() (addr string, err error) {
	if c.IdentityAgent != "" {
		return c.IdentityAgent, checkEligibilityAsAgentSocket(c.IdentityAgent, `"IdentityAgent" setting`)
	}
	envAgentSock := os.Getenv("SSH_AUTH_SOCK")
	if envAgentSock != "" {
		return envAgentSock, checkEligibilityAsAgentSocket(envAgentSock, "SSH_AUTH_SOCK environment variable")
	}
	return "", nil
}

func (c *SSHConfig) dialToAgent() (*net.UnixConn, error) {
	addr, err := c.getAgentSocketPath()
	if err != nil {
		return nil, err
	}

	// no address is given
	if addr == "" {
		return nil, nil
	}

	raddr, err := net.ResolveUnixAddr("unix", addr)
	if err != nil {
		// should never happen; we aren't gonna need any verbose error message here
		return nil, err
	}

	conn, err := net.DialUnix("unix", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", addr, err)
	}

	return conn, nil
}

func (c *SSHConfig) connectToAgent() sshAgent.Agent {
	conn, err := c.dialToAgent()
	if err != nil {
		zap.S().Warn(err.Error())
		return nil
	}
	if conn == nil {
		return nil
	}
	return sshAgent.NewClient(conn)
}

func buildPubKeySigner(passphraseFlag bool, passphrase string, keyPath string) (ssh.Signer, error) {
	pemBytes, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key %s: %w", keyPath, err)
	}

	// 1. first try parsing it with no passphrase
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err == nil {
		return signer, nil
	}

	// proceed if the key is simply passphrase-protected
	if _, ok := err.(*ssh.PassphraseMissingError); !ok {
		return nil, fmt.Errorf("failed to parse private key %s: %w", keyPath, err)
	}

	// 2. ask a passphrase if we have to
	if !passphraseFlag {
		return nil, fmt.Errorf("private key %s requires a passphrase", keyPath)
	}

	if passphrase == "" {
		var ok bool
		passphrase, ok = InputBox("kushi", "Type passphrase")
		if !ok {
			return nil, fmt.Errorf("no value entered for private key %s", keyPath)
		}
	}
	return ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
}

func (c *SSHConfig) getSignerCallback(passphraseFlag bool) (func() ([]ssh.Signer, error), int, error) {
	// the private key explicitly specified by the setting has precedence over the agent
	explicitKeyPath := c.IdentityFile
	if explicitKeyPath != "" {
		if _, err := os.Stat(explicitKeyPath); err != nil {
			// this is almost the same warning message as OpenSSH's
			zap.S().Warn("identity file %s not accessible: %w", explicitKeyPath, err)
		}
	}

	// agent has precedence over the implicit keys
	agent := c.connectToAgent()

	// implicit keys
	keyPaths, err := c.getImplicitKeyPaths()
	if err != nil {
		zap.S().Warn(err.Error())
	}

	trialState := 0 // 0: explicity key and agent 1: implicity keys
	keyIndex := 0
	maxRetries := len(keyPaths) // this may be inaccurate (okay)
	if explicitKeyPath != "" || agent != nil {
		maxRetries += 1
	}

	return func() (signers []ssh.Signer, err error) {
		// on the first try
		switch trialState {
		case 0:
			if explicitKeyPath != "" {
				zap.S().Debugf("trying with public key %s", explicitKeyPath)
				signer, err := buildPubKeySigner(passphraseFlag, c.passphrase, explicitKeyPath)
				if err != nil {
					zap.S().Warn(err.Error())
				} else {
					signers = append(signers, signer)
				}
			}
			// append agent's signers after the explicity key
			if agent != nil {
				zap.S().Debugf("trying with agent")
				agentSigners, err := agent.Signers()
				if err != nil {
					zap.S().Warn(err.Error())
				} else {
					signers = append(signers, agentSigners...)
				}
			}
			trialState = 1

		case 1:
			if keyIndex >= len(keyPaths) {
				trialState = 2
				break
			}
			keyPath := keyPaths[keyIndex]
			keyIndex += 1
			zap.S().Debugf("trying with public key %s", keyPath)
			signer, err := buildPubKeySigner(passphraseFlag, c.passphrase, keyPath)
			if err != nil {
				zap.S().Warn(err.Error())
			} else {
				signers = append(signers, signer)
			}
		}

		if len(signers) == 0 && trialState > 1 {
			err = fmt.Errorf("no more public key signers available")
			return
		}

		return
	}, maxRetries, nil
}

func (c *SSHConfig) getClientConfig(passphraseFlag bool) *ssh.ClientConfig {
	signerCb, maxRetries, err := c.getSignerCallback(passphraseFlag)
	if err != nil {
		zap.S().Fatal(err)
	}

	pubKeyAuth := ssh.RetryableAuthMethod(ssh.PublicKeysCallback(signerCb), maxRetries)

	auth := []ssh.AuthMethod{pubKeyAuth}

	hostKey := ssh.InsecureIgnoreHostKey()

	return &ssh.ClientConfig{
		User:            c.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
	}
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
