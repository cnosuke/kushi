package main

import (
	"context"
	"io/ioutil"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

func startSession(addr string, config *ssh.ClientConfig, timeout, keepAlive time.Duration, ctx context.Context, cancel context.CancelFunc) (err error) {
	zap.S().Infof("Starting sessions")

	cli, err := NewSSHClient(addr, config, timeout, keepAlive, ctx, cancel)
	if err != nil {
		zap.S().Error(err)
		zap.S().Warnf("Retrying after 5 seconds...")
		time.Sleep(5 * time.Second)
		return
	}

	defer cli.Close()

	bindingList, err := NewBindingList()
	if err != nil {
		zap.S().Error(err)
	}

	zap.S().Infow("Got binding list", "bindings", bindingList)

	var wg sync.WaitGroup

	for _, b := range bindingList {
		wg.Add(1)
		go b.handle(cli, ctx, cancel, &wg)
	}

	<-ctx.Done()
	wg.Wait()

	zap.S().Infof("Session finished")
	return
}

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	undo := zap.ReplaceGlobals(logger)
	defer undo()

	zap.S().Infof("Starting agent")

	key, err := ioutil.ReadFile("/Users/cnosuke/.ssh/id_ecdsa")
	if err != nil {
		zap.S().Fatal(err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		zap.S().Fatal(err)
	}

	auth := []ssh.AuthMethod{
		ssh.PublicKeys(signer),
	}

	hostKey := ssh.InsecureIgnoreHostKey()

	sshConfig := &ssh.ClientConfig{
		User:            "staff",
		Auth:            auth,
		HostKeyCallback: hostKey,
	}

	for {
		ctx, cancel := context.WithCancel(context.Background())
		startSession("", sshConfig, 5*time.Second, 2*time.Second, ctx, cancel)
	}
}
