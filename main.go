package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/urfave/cli"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

func startSession(addr string, config *ssh.ClientConfig, timeout, keepAlive time.Duration, b *bindingsCache, ctx context.Context, cancel context.CancelFunc) (err error) {
	zap.S().Infof("Starting sessions")

	cli, err := NewSSHClient(addr, config, timeout, keepAlive, ctx, cancel)
	if err != nil {
		zap.S().Error(err)
		zap.S().Warnf("Retrying after 5 seconds...")
		time.Sleep(5 * time.Second)
		return
	}

	defer cli.Close()

	bindings := b.Read()
	zap.S().Infow("Got binding list", "bindings", bindings)

	var wg sync.WaitGroup

	for _, b := range bindings {
		wg.Add(1)
		go b.handle(cli, ctx, cancel, &wg)
	}

	<-ctx.Done()
	wg.Wait()

	zap.S().Infof("Session finished")
	return
}

var (
	// Version and Revision are replaced when building.
	// To set specific version, edit Makefile.
	Version  string
	Revision string
	Name     string
)

const (
	LogSTDOUTPath     = "stdout"
	LogTimeFormat     = "20060102150405"
	LogFilePermission = 0755
)

func buildZapLogger(logSTDOUT bool) *zap.Logger {
	zapConfig := zap.NewDevelopmentConfig()
	if logSTDOUT {
		zapConfig.OutputPaths = []string{LogSTDOUTPath}
	} else {
		t := time.Now().Local()
		logsDir := fmt.Sprintf("%s/logs", detectConfigDir())
		logPath := fmt.Sprintf("%s/%s.log", logsDir, t.Format(LogTimeFormat))
		if _, err := os.Stat(logsDir); err != nil {
			os.MkdirAll(logsDir, LogFilePermission)
		}
		zapConfig.OutputPaths = []string{logPath}
	}

	logger, err := zapConfig.Build()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}

	return logger
}

func main() {
	app := cli.NewApp()
	app.Version = fmt.Sprintf("%s (%s)", Version, Revision)
	app.Name = Name
	app.Usage = "SSH Agent to forwarding ports as configs."

	var (
		configPath     string
		passphraseFlag bool
		logSTDOUTFlag  bool
	)

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "config, c",
			Usage:       "Config path",
			Value:       "",
			Destination: &configPath,
		},
		&cli.BoolFlag{
			Name:        "pass, p",
			Usage:       "passphrase",
			Destination: &passphraseFlag,
		},
		&cli.BoolFlag{
			Name:        "stdout",
			Usage:       "Output logs to STDOUT",
			Destination: &logSTDOUTFlag,
		},
	}

	app.Action = func(c *cli.Context) error {
		logger := buildZapLogger(logSTDOUTFlag)
		defer logger.Sync()

		undo := zap.ReplaceGlobals(logger)
		defer undo()

		zap.S().Infof("Starting agent")

		config := LoadKushiConfigs(configPath)
		zap.S().Infow("Config loaded", "config", config)

		b := NewBindingsCache(config.BindingConfigsURL, config.CheckInterval)
		b.Watch()

		for {
			ctx, cancel := context.WithCancel(context.Background())
			b.cancel = cancel

			startSession(
				config.SSHConfig.getServerAddr(),
				config.SSHConfig.getClientConfig(passphraseFlag),
				time.Duration(config.SSHConfig.Timeout)*time.Second,
				time.Duration(config.SSHConfig.KeepaliveInterval)*time.Second,
				b,
				ctx,
				cancel,
			)
		}

		return nil
	}

	app.Run(os.Args)
}
