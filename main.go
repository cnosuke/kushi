package main

import (
	"context"
	"io/ioutil"

	"time"

	"sync"

	"github.com/k0kubun/pp"
	"golang.org/x/crypto/ssh"
)

func startSession(addr string, config *ssh.ClientConfig, timeout, keepAlive time.Duration, ctx context.Context, cancel context.CancelFunc) (err error) {
	cli, err := NewSSHClient(addr, config, timeout, keepAlive, ctx, cancel)
	if err != nil {
		pp.Println(err.Error())
		pp.Println("retry after 10 seconds")
		time.Sleep(10 * time.Second)
		return
	}

	defer cli.Close()

	if err != nil {
		pp.Fatalln(err.Error())
	}

	bindingList, err := NewBindingList()
	if err != nil {
		pp.Fatalln(err.Error())
	}

	pp.Println(bindingList)

	var wg sync.WaitGroup

	for _, b := range bindingList {
		wg.Add(1)
		go b.handle(cli, ctx, cancel, &wg)
	}

	<-ctx.Done()
	wg.Wait()

	pp.Println("Session done")

	return
}

func main() {
	key, err := ioutil.ReadFile("/Users/cnosuke/.ssh/id_ecdsa")
	if err != nil {
		pp.Fatalln(err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		pp.Fatalln(signer)
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
		pp.Println("Starting sessions")
		ctx, cancel := context.WithCancel(context.Background())
		startSession("", sshConfig, 5*time.Second, 2*time.Second, ctx, cancel)
	}
}
